package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// =====================================================================
// DATA MODELS
// =====================================================================

type SiteConfig struct {
	Domain    string `json:"domain"`
	DocRoot   string `json:"doc_root"`
	DBName    string `json:"db_name"`
	SSLMethod string `json:"ssl_method"`
}

type GlobalSettings struct {
	AutoBackupEnabled bool   `json:"auto_backup_enabled"`
	AutoBackupMode    string `json:"auto_backup_mode"`
	AutoBackupDay     int    `json:"auto_backup_day"`
	AutoBackupHour    int    `json:"auto_backup_hour"`
	AutoBackupMinute  int    `json:"auto_backup_minute"`
	KeepBackups       int    `json:"keep_backups"`
	CustomBackupTime  string `json:"custom_backup_time"`
	CustomBackupFired bool   `json:"custom_backup_fired"`
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
	LastBackup time.Time    `json:"last_backup,omitempty"`
	LastSSL    time.Time    `json:"last_ssl,omitempty"`
}

type Config struct {
	GDriveRemote string         `json:"gdrive_remote"`
	BackupDir    string         `json:"backup_dir"`
	Settings     GlobalSettings `json:"settings"`
	Servers      []ServerConfig `json:"servers"`
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

func (cm *ConfigManager) Get() Config {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.config
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

func (cm *ConfigManager) AddServer(s ServerConfig) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.config.Servers = append(cm.config.Servers, s)
	return cm.save()
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

func (cm *ConfigManager) UpdateServer(name string, updated ServerConfig) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	for i, srv := range cm.config.Servers {
		if srv.Name == name {
			if updated.Sites == nil {
				updated.Sites = srv.Sites
			}
			updated.LastBackup = srv.LastBackup
			updated.LastSSL = srv.LastSSL
			cm.config.Servers[i] = updated
			return cm.save()
		}
	}
	return fmt.Errorf("server %q not found", name)
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

func (cm *ConfigManager) UpdateLastBackup(serverName string, t time.Time) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	for i, srv := range cm.config.Servers {
		if srv.Name == serverName {
			cm.config.Servers[i].LastBackup = t
			cm.save()
			return
		}
	}
}

func (cm *ConfigManager) UpdateLastSSL(serverName string, t time.Time) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	for i, srv := range cm.config.Servers {
		if srv.Name == serverName {
			cm.config.Servers[i].LastSSL = t
			cm.save()
			return
		}
	}
}

// =====================================================================
// MONITORING
// =====================================================================

type SiteStatus struct {
	URL          string        `json:"url"`
	StatusCode   int           `json:"status_code"`
	ResponseTime time.Duration `json:"response_time"`
	Up           bool          `json:"up"`
	Error        string        `json:"error"`
	CheckedAt    time.Time     `json:"checked_at"`
	SSLValid     bool          `json:"ssl_valid"`
	SSLExpiry    time.Time     `json:"ssl_expiry"`
	SSLDaysLeft  int           `json:"ssl_days_left"`
	SSLError     string        `json:"ssl_error"`
}

var (
	statusMutex  sync.RWMutex
	siteStatuses = make(map[string]SiteStatus)
)

func checkWebsite(url string) SiteStatus {
	status := SiteStatus{URL: url, CheckedAt: time.Now()}
	client := &http.Client{Timeout: 15 * time.Second}

	start := time.Now()
	resp, err := client.Get(url)
	status.ResponseTime = time.Since(start)

	if err != nil {
		status.Error = err.Error()
		return status
	}
	defer resp.Body.Close()

	status.StatusCode = resp.StatusCode
	status.Up = resp.StatusCode >= 200 && resp.StatusCode < 400

	if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		cert := resp.TLS.PeerCertificates[0]
		status.SSLValid = time.Now().Before(cert.NotAfter)
		status.SSLExpiry = cert.NotAfter
		status.SSLDaysLeft = int(time.Until(cert.NotAfter).Hours() / 24)
	} else if strings.HasPrefix(url, "https://") {
		status.SSLError = "no TLS certificate"
	}
	return status
}

func runWebsiteChecks(cm *ConfigManager) {
	cfg := cm.Get()
	var wg sync.WaitGroup
	for _, srv := range cfg.Servers {
		for _, url := range srv.StatusURLs {
			wg.Add(1)
			go func(u string) {
				defer wg.Done()
				result := checkWebsite(u)
				statusMutex.Lock()
				siteStatuses[u] = result
				statusMutex.Unlock()
			}(url)
		}
	}
	wg.Wait()
}

// =====================================================================
// BACKUP
// =====================================================================

var backupRunning sync.Mutex

func runBackup(cm *ConfigManager, filterName string) {
	if !backupRunning.TryLock() {
		log.Printf("backup already running, skipping")
		return
	}
	defer backupRunning.Unlock()

	cfg := cm.Get()
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	backupRoot := fmt.Sprintf("%s/%s", cfg.BackupDir, timestamp)

	if err := os.MkdirAll(backupRoot, 0755); err != nil {
		log.Printf("mkdir backup root: %v", err)
		return
	}

	log.Printf("=== Backup started (filter=%q) ===", filterName)

	for _, srv := range cfg.Servers {
		if filterName != "" && srv.Name != filterName {
			continue
		}
		srvDir := fmt.Sprintf("%s/%s", backupRoot, srv.Name)
		os.MkdirAll(srvDir, 0755)

		sshBase := []string{
			"-i", "/root/.ssh/backup_key",
			"-o", "StrictHostKeyChecking=no",
			"-p", srv.SSHPort,
			fmt.Sprintf("%s@%s", srv.User, srv.Host),
		}

		for _, site := range srv.Sites {
			siteDir := fmt.Sprintf("%s/%s", srvDir, site.Domain)
			os.MkdirAll(siteDir, 0755)

			if site.DBName != "" {
				dbPath := fmt.Sprintf("%s/%s.sql", siteDir, site.DBName)
				f, err := os.Create(dbPath)
				if err == nil {
					cmd := exec.Command("ssh", append(sshBase,
						fmt.Sprintf("MYSQL_PWD='%s' mysqldump -u %s %s", srv.DBPass, srv.DBUser, site.DBName))...)
					cmd.Stdout = f
					if err := cmd.Run(); err != nil {
						log.Printf("[%s/%s] db backup failed: %v", srv.Name, site.Domain, err)
					} else {
						log.Printf("[%s/%s] db backup OK", srv.Name, site.Domain)
					}
					f.Close()
				}
			}

			if site.DocRoot != "" {
				tarPath := fmt.Sprintf("%s/files.tar.gz", siteDir)
				f, err := os.Create(tarPath)
				if err == nil {
					cmd := exec.Command("ssh", append(sshBase,
						fmt.Sprintf("tar czf - '%s' 2>/dev/null", site.DocRoot))...)
					cmd.Stdout = f
					if err := cmd.Run(); err != nil {
						log.Printf("[%s/%s] file backup failed: %v", srv.Name, site.Domain, err)
					} else {
						log.Printf("[%s/%s] file backup OK", srv.Name, site.Domain)
					}
					f.Close()
				}
			}
		}
	}

	dest := fmt.Sprintf("%s:VPS-Backups/%s", cfg.GDriveRemote, timestamp)
	cmd := exec.Command("rclone", "copy", backupRoot, dest)
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("rclone upload failed: %v\n%s", err, out)
		log.Printf("KEPT local backup at %s", backupRoot)
		return
	}
	log.Printf("backup uploaded to %s", dest)

	for _, srv := range cfg.Servers {
		if filterName != "" && srv.Name != filterName {
			continue
		}
		cm.UpdateLastBackup(srv.Name, time.Now())
	}

	os.RemoveAll(backupRoot)
	log.Printf("=== Backup finished ===")
}

// =====================================================================
// SSL
// =====================================================================

func checkSSLCert(domain string) (time.Time, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://" + domain)
	if err != nil {
		return time.Time{}, err
	}
	defer resp.Body.Close()
	if resp.TLS == nil || len(resp.TLS.PeerCertificates) == 0 {
		return time.Time{}, fmt.Errorf("no certificate for %s", domain)
	}
	return resp.TLS.PeerCertificates[0].NotAfter, nil
}

func runSSLChecks(cm *ConfigManager, filterName string) {
	cfg := cm.Get()
	for _, srv := range cfg.Servers {
		if filterName != "" && srv.Name != filterName {
			continue
		}
		for _, site := range srv.Sites {
			if site.Domain == "" {
				continue
			}
			expiry, err := checkSSLCert(site.Domain)
			if err != nil {
				log.Printf("[SSL] %s check failed: %v", site.Domain, err)
				continue
			}
			daysLeft := int(time.Until(expiry).Hours() / 24)
			log.Printf("[SSL] %s expires on %s (%d days left)",
				site.Domain, expiry.Format("2006-01-02"), daysLeft)
		}
	}
}

func renewSSLCerts(cm *ConfigManager, filterName string) {
	cfg := cm.Get()
	for _, srv := range cfg.Servers {
		if filterName != "" && srv.Name != filterName {
			continue
		}
		for _, site := range srv.Sites {
			renewOneSite(srv, site)
		}
		cm.UpdateLastSSL(srv.Name, time.Now())
	}
}

func renewSiteSSL(cm *ConfigManager, serverName, domain string) {
	cfg := cm.Get()
	for _, srv := range cfg.Servers {
		if srv.Name != serverName {
			continue
		}
		for _, site := range srv.Sites {
			if site.Domain == domain {
				renewOneSite(srv, site)
				cm.UpdateLastSSL(srv.Name, time.Now())
				return
			}
		}
	}
}

func renewOneSite(srv ServerConfig, site SiteConfig) {
	sshBase := []string{
		"-i", "/root/.ssh/backup_key",
		"-o", "StrictHostKeyChecking=no",
		"-p", srv.SSHPort,
		fmt.Sprintf("%s@%s", srv.User, srv.Host),
	}
	var cmd string
	switch site.SSLMethod {
	case "cloudpanel":
		cmd = fmt.Sprintf(
			"clpctl lets-encrypt:install:certificate --domainName=%s --subjectAlternativeName=www.%s",
			site.Domain, site.Domain)
	case "none":
		log.Printf("[%s/%s] ssl_method=none, skipping", srv.Name, site.Domain)
		return
	default:
		cmd = fmt.Sprintf(
			"certbot certonly --nginx -d %s -d www.%s --non-interactive --agree-tos --renew-by-default --deploy-hook 'systemctl reload nginx'",
			site.Domain, site.Domain)
	}
	out, err := exec.Command("ssh", append(sshBase, cmd)...).CombinedOutput()
	if err != nil {
		log.Printf("[%s/%s] ssl renew failed: %v\n%s", srv.Name, site.Domain, err, out)
	} else {
		log.Printf("[%s/%s] ssl renew OK", srv.Name, site.Domain)
	}
}

// =====================================================================
// GDRIVE
// =====================================================================

type GDriveFolder struct {
	Name string `json:"name"`
	Time string `json:"time"`
}

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
	sort.Slice(folders, func(i, j int) bool { return folders[i].Name > folders[j].Name })
	return folders, nil
}

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

// =====================================================================
// AUTH
// =====================================================================

func basicAuth(next http.HandlerFunc, user, pass string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != user || p != pass {
			w.Header().Set("WWW-Authenticate", `Basic realm="VPS Dashboard"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// =====================================================================
// HTTP HANDLERS
// =====================================================================

func dashboardHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, dashboardHTML)
}

func statusHandler(w http.ResponseWriter, r *http.Request) {
	statusMutex.RLock()
	defer statusMutex.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(siteStatuses)
}

func serversHandler(cm *ConfigManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(cm.Get().Servers)
	}
}

func settingsHandler(cm *ConfigManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(cm.GetSettings())
			return
		}
		if r.Method == http.MethodPost {
			hour, _ := strconv.Atoi(r.FormValue("auto_backup_hour"))
			minute, _ := strconv.Atoi(r.FormValue("auto_backup_minute"))
			day, _ := strconv.Atoi(r.FormValue("auto_backup_day"))
			keep, _ := strconv.Atoi(r.FormValue("keep_backups"))

			existing := cm.GetSettings()
			updated := GlobalSettings{
				AutoBackupEnabled: r.FormValue("auto_backup_enabled") == "true",
				AutoBackupMode:    r.FormValue("auto_backup_mode"),
				AutoBackupDay:     day,
				AutoBackupHour:    hour,
				AutoBackupMinute:  minute,
				KeepBackups:       keep,
				CustomBackupTime:  r.FormValue("custom_backup_time"),
			}
			if updated.CustomBackupTime != existing.CustomBackupTime {
				updated.CustomBackupFired = false
			} else {
				updated.CustomBackupFired = existing.CustomBackupFired
			}
			if err := cm.UpdateSettings(updated); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Write([]byte("ok"))
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func addServerHandler(cm *ConfigManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		srv := ServerConfig{
			Name:    r.FormValue("name"),
			Host:    r.FormValue("host"),
			SSHPort: r.FormValue("ssh_port"),
			User:    r.FormValue("user"),
			SSHPass: r.FormValue("ssh_password"),
			DBUser:  r.FormValue("db_user"),
			DBPass:  r.FormValue("db_pass"),
		}
		if urls := r.FormValue("status_urls"); urls != "" {
			srv.StatusURLs = strings.Split(urls, ",")
		}
		if sitesText := r.FormValue("sites"); sitesText != "" {
			for _, line := range strings.Split(sitesText, "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				parts := strings.Split(line, "|")
				if len(parts) < 2 {
					continue
				}
				site := SiteConfig{
					Domain:  strings.TrimSpace(parts[0]),
					DocRoot: strings.TrimSpace(parts[1]),
				}
				if len(parts) > 2 {
					site.DBName = strings.TrimSpace(parts[2])
				}
				if len(parts) > 3 {
					site.SSLMethod = strings.TrimSpace(parts[3])
				}
				if site.SSLMethod == "" {
					site.SSLMethod = "certbot"
				}
				srv.Sites = append(srv.Sites, site)
			}
		}
		if err := cm.AddServer(srv); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

func removeServerHandler(cm *ConfigManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		if name == "" {
			http.Error(w, "name required", http.StatusBadRequest)
			return
		}
		if err := cm.RemoveServer(name); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}

func addSiteHandler(cm *ConfigManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		site := SiteConfig{
			Domain:    r.FormValue("domain"),
			DocRoot:   r.FormValue("doc_root"),
			DBName:    r.FormValue("db_name"),
			SSLMethod: r.FormValue("ssl_method"),
		}
		if site.SSLMethod == "" {
			site.SSLMethod = "certbot"
		}
		if err := cm.AddSite(r.FormValue("server"), site); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

func updateServerHandler(cm *ConfigManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		name := r.FormValue("name")
		updated := ServerConfig{
			Name:    name,
			Host:    r.FormValue("host"),
			SSHPort: r.FormValue("ssh_port"),
			User:    r.FormValue("user"),
			SSHPass: r.FormValue("ssh_password"),
			DBUser:  r.FormValue("db_user"),
			DBPass:  r.FormValue("db_pass"),
		}
		if urls := r.FormValue("status_urls"); urls != "" {
			updated.StatusURLs = strings.Split(urls, ",")
		}
		if err := cm.UpdateServer(name, updated); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

func updateSiteHandler(cm *ConfigManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		updated := SiteConfig{
			Domain:    r.FormValue("domain"),
			DocRoot:   r.FormValue("doc_root"),
			DBName:    r.FormValue("db_name"),
			SSLMethod: r.FormValue("ssl_method"),
		}
		if updated.SSLMethod == "" {
			updated.SSLMethod = "certbot"
		}
		if err := cm.UpdateSite(r.FormValue("server"), r.FormValue("old_domain"), updated); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

// =====================================================================
// MAIN
// =====================================================================

func main() {
	cm, err := NewConfigManager("/etc/vps-dashboard/config.json")
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	dashUser := os.Getenv("DASH_USER")
	dashPass := os.Getenv("DASH_PASS")
	if dashUser == "" || dashPass == "" {
		log.Fatal("DASH_USER and DASH_PASS must be set")
	}

	// Flexible backup scheduler
	go func() {
		lastFiredMinute := ""
		for {
			time.Sleep(30 * time.Second)

			s := cm.GetSettings()
			now := time.Now()
			currentMinute := now.Format("2006-01-02T15:04")

			if s.AutoBackupMode == "custom" {
				if s.CustomBackupTime == "" || s.CustomBackupFired {
					continue
				}
				target, err := time.Parse(time.RFC3339, s.CustomBackupTime)
				if err != nil {
					log.Printf("custom backup time parse error: %v", err)
					continue
				}
				if now.Before(target) {
					continue
				}
				log.Printf("Custom one-off backup triggered (target=%s)", target.Format(time.RFC3339))
				go runBackup(cm, "")
				s.CustomBackupFired = true
				cm.UpdateSettings(s)
				continue
			}

			if !s.AutoBackupEnabled {
				continue
			}
			if now.Hour() != s.AutoBackupHour || now.Minute() != s.AutoBackupMinute {
				continue
			}
			if lastFiredMinute == currentMinute {
				continue
			}

			shouldRun := false
			switch s.AutoBackupMode {
			case "daily":
				shouldRun = true
			case "weekly":
				shouldRun = int(now.Weekday()) == s.AutoBackupDay
			case "monthly":
				shouldRun = now.Day() == s.AutoBackupDay
			case "semimonthly":
				shouldRun = now.Day() == 1 || now.Day() == 15
			}
			if !shouldRun {
				continue
			}

			log.Printf("Scheduled backup triggered (%s)", s.AutoBackupMode)
			go runBackup(cm, "")
			lastFiredMinute = currentMinute
		}
	}()

	// Website checks every 60s
	go func() {
		for {
			runWebsiteChecks(cm)
			time.Sleep(60 * time.Second)
		}
	}()

	// SSL check daily at 09:00
	go func() {
		for {
			now := time.Now()
			next := time.Date(now.Year(), now.Month(), now.Day(), 9, 0, 0, 0, now.Location())
			if next.Before(now) {
				next = next.Add(24 * time.Hour)
			}
			time.Sleep(time.Until(next))
			runSSLChecks(cm, "")
		}
	}()

	// Routes
	http.HandleFunc("/", basicAuth(dashboardHandler, dashUser, dashPass))
	http.HandleFunc("/status", basicAuth(statusHandler, dashUser, dashPass))
	http.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	http.HandleFunc("/backup", basicAuth(func(w http.ResponseWriter, r *http.Request) {
		go runBackup(cm, r.URL.Query().Get("name"))
		w.Write([]byte("backup started"))
	}, dashUser, dashPass))
	http.HandleFunc("/ssl/check", basicAuth(func(w http.ResponseWriter, r *http.Request) {
		go runSSLChecks(cm, r.URL.Query().Get("name"))
		w.Write([]byte("ssl check started"))
	}, dashUser, dashPass))
	http.HandleFunc("/ssl/renew", basicAuth(func(w http.ResponseWriter, r *http.Request) {
		go renewSSLCerts(cm, r.URL.Query().Get("name"))
		w.Write([]byte("ssl renewal started"))
	}, dashUser, dashPass))
	http.HandleFunc("/backup/site", basicAuth(func(w http.ResponseWriter, r *http.Request) {
		go runBackup(cm, r.URL.Query().Get("server"))
		w.Write([]byte("site backup started"))
	}, dashUser, dashPass))
	http.HandleFunc("/ssl/renew/site", basicAuth(func(w http.ResponseWriter, r *http.Request) {
		go renewSiteSSL(cm, r.URL.Query().Get("server"), r.URL.Query().Get("domain"))
		w.Write([]byte("site ssl renewal started"))
	}, dashUser, dashPass))

	http.HandleFunc("/api/settings", basicAuth(settingsHandler(cm), dashUser, dashPass))
	http.HandleFunc("/api/backups/list", basicAuth(func(w http.ResponseWriter, r *http.Request) {
		folders, err := listGDriveBackups(cm)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(folders)
	}, dashUser, dashPass))
	http.HandleFunc("/api/backups/delete", basicAuth(func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		if name == "" {
			http.Error(w, "name required", http.StatusBadRequest)
			return
		}
		if err := deleteGDriveBackup(cm, name); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Write([]byte("deleted " + name))
	}, dashUser, dashPass))
	http.HandleFunc("/api/backups/prune", basicAuth(func(w http.ResponseWriter, r *http.Request) {
		s := cm.GetSettings()
		n, err := pruneOldBackups(cm, s.KeepBackups)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		fmt.Fprintf(w, "pruned %d old backups (kept %d)", n, s.KeepBackups)
	}, dashUser, dashPass))

	http.HandleFunc("/api/servers", basicAuth(serversHandler(cm), dashUser, dashPass))
	http.HandleFunc("/api/servers/add", basicAuth(addServerHandler(cm), dashUser, dashPass))
	http.HandleFunc("/api/servers/remove", basicAuth(removeServerHandler(cm), dashUser, dashPass))
	http.HandleFunc("/api/servers/update", basicAuth(updateServerHandler(cm), dashUser, dashPass))

	http.HandleFunc("/api/sites/add", basicAuth(addSiteHandler(cm), dashUser, dashPass))
	http.HandleFunc("/api/sites/remove", basicAuth(func(w http.ResponseWriter, r *http.Request) {
		server := r.URL.Query().Get("server")
		domain := r.URL.Query().Get("domain")
		if err := cm.RemoveSite(server, domain); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}, dashUser, dashPass))
	http.HandleFunc("/api/sites/update", basicAuth(updateSiteHandler(cm), dashUser, dashPass))

	log.Println("listening on 127.0.0.1:9090")
	log.Fatal(http.ListenAndServe("127.0.0.1:9090", nil))
}
