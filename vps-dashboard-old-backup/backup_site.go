package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func runSiteBackup(cm *ConfigManager, serverName, domain string) {
	cfg := cm.Get()

	for _, srv := range cfg.Servers {
		if srv.Name != serverName {
			continue
		}
		for _, site := range srv.Sites {
			if site.Domain != domain {
				continue
			}

			timestamp := time.Now().Format("2006-01-02_15-04-05")
			srvDir := filepath.Join(cfg.BackupDir, timestamp, srv.Name, site.Domain)
			os.MkdirAll(srvDir, 0755)
			log.Printf("[%s/%s] backup started", srv.Name, site.Domain)

			sshBase := []string{
				"-i", "/root/.ssh/backup_key",
				"-o", "StrictHostKeyChecking=no",
				"-p", srv.SSHPort,
				fmt.Sprintf("%s@%s", srv.User, srv.Host),
			}

			// 1. Database (only this site's DB)
			if site.DBName != "" {
				dbPath := filepath.Join(srvDir, site.DBName+".sql")
				dbFile, err := os.Create(dbPath)
				if err != nil {
					log.Printf("[%s/%s] db file error: %v", srv.Name, site.Domain, err)
					continue
				}
				cmd := exec.Command("ssh", append(sshBase,
					fmt.Sprintf("MYSQL_PWD='%s' mysqldump -u %s %s",
						srv.DBPass, srv.DBUser, site.DBName))...)
				cmd.Stdout = dbFile
				if err := cmd.Run(); err != nil {
					log.Printf("[%s/%s] db backup failed: %v", srv.Name, site.Domain, err)
				} else {
					log.Printf("[%s/%s] db backup OK", srv.Name, site.Domain)
				}
				dbFile.Close()
			}

			// 2. Files (only this site's docroot)
			if site.DocRoot != "" {
				tarPath := filepath.Join(srvDir, "files.tar.gz")
				tarFile, err := os.Create(tarPath)
				if err != nil {
					log.Printf("[%s/%s] tar file error: %v", srv.Name, site.Domain, err)
					continue
				}
				cmd := exec.Command("ssh", append(sshBase,
					fmt.Sprintf("tar czf - '%s' 2>/dev/null", site.DocRoot))...)
				cmd.Stdout = tarFile
				if err := cmd.Run(); err != nil {
					log.Printf("[%s/%s] file backup failed: %v", srv.Name, site.Domain, err)
				} else {
					log.Printf("[%s/%s] file backup OK", srv.Name, site.Domain)
				}
				tarFile.Close()
			}

			// 3. Upload
			dest := fmt.Sprintf("%s:VPS-Backups/%s/%s/%s",
				cfg.GDriveRemote, timestamp, srv.Name, site.Domain)
			cmd := exec.Command("rclone", "copy", srvDir, dest)
			if out, err := cmd.CombinedOutput(); err != nil {
				log.Printf("[%s/%s] upload failed: %v\n%s", srv.Name, site.Domain, err, out)
				return // keep local copy on failure
			}
			log.Printf("[%s/%s] uploaded to %s", srv.Name, site.Domain, dest)
			cm.UpdateLastBackup(srv.Name, time.Now())
			// 4. Cleanup on success
			os.RemoveAll(filepath.Join(cfg.BackupDir, timestamp))
		}
	}
}
func runAllSitesBackup(cm *ConfigManager, serverName string) {
	cfg := cm.Get()
	for _, srv := range cfg.Servers {
		if srv.Name != serverName {
			continue
		}
		for _, site := range srv.Sites {
			log.Printf("[%s] starting site backup for %s", srv.Name, site.Domain)
			runSiteBackup(cm, srv.Name, site.Domain)
		}
	}
}
