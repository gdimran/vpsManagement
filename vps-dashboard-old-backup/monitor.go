package main

import (
	"context"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

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
	SSLError     string  	   `json:"ssl_error"`
}

var (
	statusMutex  sync.RWMutex
	siteStatuses = make(map[string]SiteStatus)
)

func checkWebsite(url string) SiteStatus {
	status := SiteStatus{URL: url, CheckedAt: time.Now()}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		status.Error = err.Error()
		return status
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; VPSMonitor/1.0)")

	client := &http.Client{Timeout: 15 * time.Second}
	start := time.Now()
	resp, err := client.Do(req)
	status.ResponseTime = time.Since(start)

	if err != nil {
		status.Error = err.Error()
		return status
	}
	defer resp.Body.Close()

	status.StatusCode = resp.StatusCode
	status.Up = resp.StatusCode >= 200 && resp.StatusCode < 400

	// Extract SSL certificate info if HTTPS
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

				if result.Up {
					log.Printf("[UPTIME] %s OK (%d, %v)", u, result.StatusCode, result.ResponseTime)
				} else {
					log.Printf("[UPTIME] %s DOWN: %s", u, result.Error)
				}
			}(url)
		}
	}
	wg.Wait()
}
