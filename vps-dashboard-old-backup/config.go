package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

type GlobalSettings struct {
	AutoBackupEnabled bool   `json:"auto_backup_enabled"`
	AutoBackupMode    string `json:"auto_backup_mode"` // "daily" | "weekly" | "monthly" | "semimonthly" | "custom"
	AutoBackupDay     int    `json:"auto_backup_day"`  // weekly: 0-6, monthly: 1-31
	AutoBackupHour    int    `json:"auto_backup_hour"`
	AutoBackupMinute  int    `json:"auto_backup_minute"`
	KeepBackups       int    `json:"keep_backups"`

	// NEW: custom one-off
	CustomBackupTime  string `json:"custom_backup_time"`  // RFC3339, e.g. "2026-10-01T03:00:00Z"
	CustomBackupFired bool   `json:"custom_backup_fired"` // set true after running once
}

type Config struct {
	GDriveRemote string         `json:"gdrive_remote"`
	BackupDir    string         `json:"backup_dir"`
	Settings     GlobalSettings `json:"settings"`
	Servers      []ServerConfig `json:"servers"`
}

type SiteConfig struct {
	Domain     string `json:"domain"`
	DocRoot    string `json:"doc_root"`
	DBName     string `json:"db_name"`
	SSLMethod  string `json:"ssl_method"` // "certbot" | "cloudpanel" | "none"
	FullBackup bool   `json:"full_backup"`
}

type ServerConfig struct {
	Name       string       `json:"name"`
	Host       string       `json:"host"`
	SSHPort    string       `json:"ssh_port"`
	User       string       `json:"user"`
	SSHPass    string       `json:"ssh_password"`
	DBUser     string       `json:"db_user"`
	DBPass     string       `json:"db_pass"`
	StatusURLs []string     `json:"status_urls"`
	Sites      []SiteConfig `json:"sites"`

	LastBackup time.Time `json:"last_backup,omitempty"`
	LastSSL    time.Time `json:"last_ssl,omitempty"`
}
type ConfigManager struct {
	mu       sync.RWMutex
	config   Config
	filePath string
}

func NewConfigManager(path string) (*ConfigManager, error) {
	cm := &ConfigManager{filePath: path}
	if err := cm.load(); err != nil {
		return nil, err
	}
	return cm, nil
}

func (cm *ConfigManager) load() error {
	data, err := os.ReadFile(cm.filePath)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &cm.config)
}

func (cm *ConfigManager) save() error {
	data, err := json.MarshalIndent(cm.config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cm.filePath, data, 0600)
}
func (cm *ConfigManager) GetSettings() GlobalSettings {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.config.Settings
}

func (cm *ConfigManager) UpdateSettings(s GlobalSettings) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.config.Settings = s
	return cm.save()
}
func (cm *ConfigManager) Get() Config {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.config
}
func (cm *ConfigManager) AddSite(serverName string, site SiteConfig) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	for i, srv := range cm.config.Servers {
		if srv.Name == serverName {
			cm.config.Servers[i].Sites = append(cm.config.Servers[i].Sites, site)
			return cm.save()
		}
	}
	return fmt.Errorf("server %q not found", serverName)
}
func (cm *ConfigManager) RemoveSite(serverName, domain string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	for i, srv := range cm.config.Servers {
		if srv.Name != serverName {
			continue
		}
		var filtered []SiteConfig
		for _, site := range srv.Sites {
			if site.Domain != domain {
				filtered = append(filtered, site)
			}
		}
		cm.config.Servers[i].Sites = filtered
		return cm.save()
	}
	return fmt.Errorf("server %q not found", serverName)
}
func (cm *ConfigManager) AddServer(s ServerConfig) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.config.Servers = append(cm.config.Servers, s)
	return cm.save()
}
func (cm *ConfigManager) UpdateServer(name string, updated ServerConfig) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	for i, srv := range cm.config.Servers {
		if srv.Name == name {
			// Preserve sites unless the update provides new ones
			if updated.Sites == nil {
				updated.Sites = srv.Sites
			}
			cm.config.Servers[i] = updated
			return cm.save()
		}
	}
	return fmt.Errorf("server %q not found", name)
}

func (cm *ConfigManager) UpdateSite(serverName, domain string, updated SiteConfig) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	for i, srv := range cm.config.Servers {
		if srv.Name != serverName {
			continue
		}
		for j, site := range srv.Sites {
			if site.Domain == domain {
				cm.config.Servers[i].Sites[j] = updated
				return cm.save()
			}
		}
	}
	return fmt.Errorf("site %q not found on %q", domain, serverName)
}
func (cm *ConfigManager) RemoveServer(name string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	var filtered []ServerConfig
	for _, s := range cm.config.Servers {
		if s.Name != name {
			filtered = append(filtered, s)
		}
	}
	cm.config.Servers = filtered
	return cm.save()
}
func (cm *ConfigManager) UpdateLastBackup(serverName string, t time.Time) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	for i, srv := range cm.config.Servers {
		if srv.Name == serverName {
			cm.config.Servers[i].LastBackup = t
			return cm.save()
		}
	}
	return fmt.Errorf("server %q not found", serverName)
}

func (cm *ConfigManager) UpdateLastSSL(serverName string, t time.Time) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	for i, srv := range cm.config.Servers {
		if srv.Name == serverName {
			cm.config.Servers[i].LastSSL = t
			return cm.save()
		}
	}
	return fmt.Errorf("server %q not found", serverName)
}
