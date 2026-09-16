package main

import (
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"os/exec"
	"time"
)

func checkSSLCert(domain string) (time.Time, error) {
	conn, err := tls.DialWithDialer(
		&net.Dialer{Timeout: 10 * time.Second},
		"tcp",
		domain+":443",
		&tls.Config{},
	)
	if err != nil {
		return time.Time{}, err
	}
	defer conn.Close()

	state := conn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return time.Time{}, fmt.Errorf("no certificates from %s", domain)
	}
	return state.PeerCertificates[0].NotAfter, nil
}

// runSSLChecks verifies certificate expiry for every site on each server.
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

// renewSSLCerts triggers per-domain renewal for every site on each server.
func renewSSLCerts(cm *ConfigManager, filterName string) {
	cfg := cm.Get()
	for _, srv := range cfg.Servers {
		if filterName != "" && srv.Name != filterName {
			continue
		}
		for _, site := range srv.Sites {
			renewOneSite(srv, site) // ← site is used here
		}
		cm.UpdateLastSSL(srv.Name, time.Now())
	}
}

// renewSiteSSL renews a single site's certificate.
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

// renewOneSite does the SSH + certbot/cloudpanel call for a single site.
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
