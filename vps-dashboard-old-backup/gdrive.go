package main

import (
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
)

type GDriveFolder struct {
	Name string `json:"name"`
	Time string `json:"time"`
}

// listGDriveBackups returns the timestamped folders inside VPS-Backups.
func listGDriveBackups(cm *ConfigManager) ([]GDriveFolder, error) {
	cfg := cm.Get()
	out, err := exec.Command("rclone", "lsf",
		fmt.Sprintf("%s:VPS-Backups", cfg.GDriveRemote),
		"--dirs-only", "--format", "tp").Output()
	if err != nil {
		return nil, err
	}

	var folders []GDriveFolder
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ";", 2)
		if len(parts) != 2 {
			continue
		}
		folders = append(folders, GDriveFolder{Time: parts[0], Name: parts[1]})
	}
	// Sort newest first
	sort.Slice(folders, func(i, j int) bool {
		return folders[i].Name > folders[j].Name
	})
	return folders, nil
}

// deleteGDriveBackup removes a specific folder from Drive.
func deleteGDriveBackup(cm *ConfigManager, name string) error {
	cfg := cm.Get()
	path := fmt.Sprintf("%s:VPS-Backups/%s", cfg.GDriveRemote, name)
	out, err := exec.Command("rclone", "purge", path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, out)
	}
	log.Printf("Deleted Drive folder: %s", name)
	return nil
}

// pruneOldBackups keeps the N most recent folders and deletes the rest.
func pruneOldBackups(cm *ConfigManager, keep int) (int, error) {
	folders, err := listGDriveBackups(cm)
	if err != nil {
		return 0, err
	}
	if len(folders) <= keep {
		return 0, nil
	}
	deleted := 0
	for _, f := range folders[keep:] {
		if err := deleteGDriveBackup(cm, f.Name); err != nil {
			log.Printf("Prune failed for %s: %v", f.Name, err)
			continue
		}
		deleted++
	}
	return deleted, nil
}
