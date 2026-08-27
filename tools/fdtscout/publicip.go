package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Public IP tracking: log when the ISP-assigned IP changes (so the user doesn't lose track of
// where the device is, and can be alerted on it), plus an optional Cloudflare DDNS updater since
// the box is already watching the same uplink -- turns "monitors the network" into "actively
// useful home-server infrastructure."

type PublicIPRecord struct {
	Time time.Time `json:"t"`
	IP   string    `json:"ip"`
}

type PublicIPTracker struct {
	mu      sync.RWMutex
	history []PublicIPRecord
}

const maxPublicIPRecords = 500

func NewPublicIPTracker() *PublicIPTracker {
	t := &PublicIPTracker{}
	t.load()
	return t
}

func (t *PublicIPTracker) load() {
	data, err := os.ReadFile(PublicIPFile)
	if err != nil {
		return
	}
	var history []PublicIPRecord
	if json.Unmarshal(data, &history) == nil {
		t.mu.Lock()
		t.history = history
		t.mu.Unlock()
	}
}

func (t *PublicIPTracker) persist() {
	t.mu.RLock()
	data, err := json.Marshal(t.history)
	t.mu.RUnlock()
	if err != nil {
		return
	}
	tmp := PublicIPFile + ".tmp"
	if os.WriteFile(tmp, data, 0644) == nil {
		os.Rename(tmp, PublicIPFile)
	}
}

func (t *PublicIPTracker) History() []PublicIPRecord {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]PublicIPRecord, len(t.history))
	copy(out, t.history)
	return out
}

func (t *PublicIPTracker) Current() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if len(t.history) == 0 {
		return ""
	}
	return t.history[len(t.history)-1].IP
}

func fetchPublicIP() (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://api.ipify.org")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256))
	if err != nil {
		return "", err
	}
	ip := strings.TrimSpace(string(body))
	if ip == "" {
		return "", fmt.Errorf("empty response")
	}
	return ip, nil
}

// checkOnce fetches the current public IP, and if it differs from the last recorded one, appends a
// new record, fires the IP-change alert, and applies the DDNS update if configured.
func (t *PublicIPTracker) checkOnce() {
	ip, err := fetchPublicIP()
	if err != nil {
		fmt.Fprintf(os.Stderr, "public IP check failed: %v\n", err)
		return
	}
	prev := t.Current()
	if prev == ip {
		return
	}

	t.mu.Lock()
	t.history = append(t.history, PublicIPRecord{Time: time.Now(), IP: ip})
	if len(t.history) > maxPublicIPRecords {
		t.history = t.history[len(t.history)-maxPublicIPRecords:]
	}
	t.mu.Unlock()
	go t.persist()

	if prev != "" {
		cfg := LoadPushbulletConfig()
		if cfg.Enabled && cfg.AlertIPChange {
			notifyIPChange(prev, ip)
		}
		if ddns := LoadDDNSConfig(); ddns.Enabled {
			if err := applyDDNSUpdate(ddns, ip); err != nil {
				fmt.Fprintf(os.Stderr, "DDNS update failed: %v\n", err)
			}
		}
	}
}

func (t *PublicIPTracker) Run(stop <-chan struct{}) {
	t.checkOnce() // establish a baseline immediately, don't wait a full interval for the first record
	tick := time.NewTicker(10 * time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
			t.checkOnce()
		}
	}
}

// --- Dynamic DNS (Cloudflare only, for now -- the most common/well-documented API of the
// candidates named during design) -----------------------------------------------------------

type DDNSConfig struct {
	Enabled  bool   `json:"enabled"`
	APIToken string `json:"apiToken"`
	ZoneID   string `json:"zoneId"`
	RecordID string `json:"recordId"`
	Hostname string `json:"hostname"` // display only, for the Settings tab -- the actual update is keyed by RecordID
}

func LoadDDNSConfig() DDNSConfig {
	var cfg DDNSConfig
	data, err := os.ReadFile(DDNSConfigFile)
	if err != nil {
		return cfg
	}
	_ = json.Unmarshal(data, &cfg)
	return cfg
}

func SaveDDNSConfig(cfg DDNSConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := DDNSConfigFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, DDNSConfigFile)
}

func applyDDNSUpdate(cfg DDNSConfig, ip string) error {
	if cfg.APIToken == "" || cfg.ZoneID == "" || cfg.RecordID == "" {
		return fmt.Errorf("DDNS is enabled but not fully configured")
	}
	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records/%s", cfg.ZoneID, cfg.RecordID)
	payload, _ := json.Marshal(map[string]any{"type": "A", "content": ip, "ttl": 300, "proxied": false})
	req, err := http.NewRequest("PATCH", url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIToken)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("cloudflare API returned HTTP %d", resp.StatusCode)
	}
	return nil
}
