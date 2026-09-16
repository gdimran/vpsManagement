package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

var backupRunning sync.Mutex

// runBackup runs a full server-level backup: every site's docroot and every
// site's DB for the given server (or all servers if filterName is empty).
func runBackup(cm *ConfigManager, filterName string) {
	if !backupRunning.TryLock() {
		log.Printf("backup already running, skipping")
		return
	}
	defer backupRunning.Unlock()

	cfg := cm.Get()
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	backupRoot := filepath.Join(cfg.BackupDir, timestamp)

	if err := os.MkdirAll(backupRoot, 0755); err != nil {
		log.Printf("mkdir backup root: %v", err)
		return
	}

	log.Printf("=== Full backup started (filter=%q) ===", filterName)

	for _, srv := range cfg.Servers {
		if filterName != "" && srv.Name != filterName {
			continue
		}
		srvDir := filepath.Join(backupRoot, srv.Name)
		os.MkdirAll(srvDir, 0755)

		sshBase := []string{
			"-i", "/root/.ssh/backup_key",
			"-o", "StrictHostKeyChecking=no",
			"-p", srv.SSHPort,
			fmt.Sprintf("%s@%s", srv.User, srv.Host),
		}

		// 1. Database dump — all DBs referenced by this server's sites
		for _, site := range srv.Sites {
			if site.DBName == "" {
				continue
			}
			dbPath := filepath.Join(srvDir, site.DBName+".sql")
			dbFile, err := os.Create(dbPath)
			if err != nil {
				log.Printf("[%s] cannot create db file: %v", srv.Name, err)
				continue
			}
			remoteDump := fmt.Sprintf(
				"MYSQL_PWD='%s' mysqldump -u %s %s",
				srv.DBPass, srv.DBUser, site.DBName,
			)
			cmd := exec.Command("ssh", append(sshBase, remoteDump)...)
			cmd.Stdout = dbFile
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				log.Printf("[%s] db backup failed (%s): %v", srv.Name, site.DBName, err)
			} else {
				log.Printf("[%s] db backup OK: %s", srv.Name, site.DBName)
			}
			dbFile.Close()
		}

		// 2. Web files — one tarball per docroot
		for _, site := range srv.Sites {
			if site.DocRoot == "" {
				continue
			}
			safeName := site.Domain
			if safeName == "" {
				safeName = "site"
			}
			tarPath := filepath.Join(srvDir, safeName+".tar.gz")
			tarFile, err := os.Create(tarPath)
			if err != nil {
				log.Printf("[%s] cannot create tar file: %v", srv.Name, err)
				continue
			}
			remoteTar := fmt.Sprintf("tar czf - '%s' 2>/dev/null", site.DocRoot)
			cmd := exec.Command("ssh", append(sshBase, remoteTar)...)
			cmd.Stdout = tarFile
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				log.Printf("[%s] web backup failed (%s): %v", srv.Name, site.Domain, err)
			} else {
				log.Printf("[%s] web backup OK: %s", srv.Name, site.Domain)
			}
			tarFile.Close()
		}
	}

	// 3. Upload to Google Drive
	dest := fmt.Sprintf("%s:VPS-Backups/%s", cfg.GDriveRemote, timestamp)
	cmd := exec.Command("rclone", "copy", backupRoot, dest)
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("rclone upload failed: %v\n%s", err, out)
		log.Printf("KEPT local backup at %s — retry with: rclone copy %s %s", backupRoot, backupRoot, dest)
		return
	}
	log.Printf("backup uploaded to %s", dest)

	// 4. Cleanup local temp
	os.RemoveAll(backupRoot)
	log.Printf("=== Full backup finished ===")

	// Record last backup time per server
	for _, srv := range cfg.Servers {
		if filterName != "" && srv.Name != filterName {
			continue
		}
		cm.UpdateLastBackup(srv.Name, time.Now())
	}
}
