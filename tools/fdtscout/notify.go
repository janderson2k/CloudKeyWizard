package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// Pushbullet integration: outbound proactive alerts (this file) plus the two-way callsign command
// channel (pushbullet_listen.go). Designed over several rounds of discussion with the user rather
// than guessed: the API access token comes from Pushbullet's own Settings -> Account -> Access
// Tokens page, never the user's actual Pushbullet login -- FDT.Scout only ever holds that token
// string. Config lives under /etc (same directory class as the TLS cert) since it holds a
// credential, not general app state.

type PushbulletConfig struct {
	Enabled  bool   `json:"enabled"`
	Token    string `json:"accessToken"`
	Callsign string `json:"callsign"`

	// Individual alert triggers, each off by default -- this is opt-in behavior, not something that
	// starts pushing notifications the moment a token is entered. See handlers_settings.go for the
	// Settings-tab form that sets these.
	AlertDiskFull    bool `json:"alertDiskFull"`
	AlertServiceDown bool `json:"alertServiceDown"`
	AlertLockout     bool `json:"alertLockout"`
	AlertIPChange    bool `json:"alertIpChange"`
	AlertDigest      bool `json:"alertDigest"`
}

func LoadPushbulletConfig() PushbulletConfig {
	var cfg PushbulletConfig
	data, err := os.ReadFile(PushbulletConfigFile)
	if err != nil {
		return cfg
	}
	_ = json.Unmarshal(data, &cfg)
	return cfg
}

func SavePushbulletConfig(cfg PushbulletConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := PushbulletConfigFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, PushbulletConfigFile)
}

// sendPush posts one note-type push to the configured Pushbullet account and returns its Pushbullet
// "iden" -- callers register that iden via markPushAsOurs so the inbound poller (pushbullet_listen.go)
// can recognize and skip this exact push on its next poll, rather than re-reading FDT.Scout's own
// replies as if they were new incoming commands. Best-effort: a failed push (bad token, Pushbullet
// down, no network) is logged to stderr and otherwise swallowed -- nothing in this app should ever
// fail or block on a notification not going out.
func sendPush(cfg PushbulletConfig, title, body string) (string, error) {
	if !cfg.Enabled || cfg.Token == "" {
		return "", nil
	}
	payload, _ := json.Marshal(map[string]string{"type": "note", "title": title, "body": body})
	req, err := http.NewRequest("POST", "https://api.pushbullet.com/v2/pushes", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Access-Token", cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("pushbullet returned HTTP %d", resp.StatusCode)
	}
	var created struct {
		Iden string `json:"iden"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&created)
	return created.Iden, nil
}

func notifyAsync(title, body string) {
	cfg := LoadPushbulletConfig()
	if !cfg.Enabled || cfg.Token == "" {
		return
	}
	// Proactive alerts (unlike command replies, whose title already IS the callsign) had no device
	// identifier at all -- "Daily digest" / "Disk space low" look identical from every device on the
	// same Pushbullet account, with no way to tell which one actually sent it. Real report: a user
	// running this on more than one device couldn't tell them apart. Tag every alert's title with the
	// same callsign the command channel already uses, so it's the one consistent identifier across
	// both directions of this integration.
	taggedTitle := fmt.Sprintf("%s: %s", callsignPrefix(cfg), title)
	go func() {
		iden, err := sendPush(cfg, taggedTitle, body)
		if err != nil {
			fmt.Fprintf(os.Stderr, "pushbullet send failed: %v\n", err)
			return
		}
		markPushAsOurs(iden)
	}()
}

func callsignPrefix(cfg PushbulletConfig) string {
	name := strings.TrimSpace(cfg.Callsign)
	if name == "" {
		if hostname, err := os.Hostname(); err == nil {
			name = hostname
		} else {
			name = "SCOUT"
		}
	}
	return strings.ToUpper(name)
}

// --- Individual alert triggers, each a thin wrapper naming what actually happened -----------

func notifyDiskFull(path string, pct float64) {
	notifyAsync("Disk space low", fmt.Sprintf("%s is at %.0f%% used.", path, pct))
}

func notifyServiceDown(name string) {
	notifyAsync("Service stopped", fmt.Sprintf("%s is no longer running.", name))
}

func notifyLockout(username string) {
	notifyAsync("Login lockout", fmt.Sprintf("Account %q was locked out after repeated failed login attempts.", username))
}

func notifyIPChange(oldIP, newIP string) {
	notifyAsync("Public IP changed", fmt.Sprintf("%s -> %s", oldIP, newIP))
}

func notifyDigest(metrics *MetricsCollector) {
	history := metrics.History()
	if len(history) == 0 {
		return
	}
	last := history[len(history)-1]
	volLine := ""
	if last.VolumePct >= 0 {
		volLine = fmt.Sprintf(", /volume %.0f%%", last.VolumePct)
	}
	body := fmt.Sprintf("CPU %.0f%%, mem %.0f%%, / %.0f%%%s", last.CPUPct, last.MemPct, last.RootPct, volLine)
	notifyAsync("Daily digest", body)
}

// runServiceWatch periodically diffs getAppStatuses() against its own previous snapshot and fires
// notifyServiceDown for any app that transitions from active to inactive while still installed --
// deliberately edge-triggered (fires once per transition, not once per poll) so a service that's
// simply never been installed, or has been down for a while already, doesn't spam a push every
// cycle.
func runServiceWatch(stop <-chan struct{}) {
	prevActive := map[string]bool{}
	tick := time.NewTicker(2 * time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
			cfg := LoadPushbulletConfig()
			if !cfg.Enabled || !cfg.AlertServiceDown {
				continue
			}
			for _, app := range getAppStatuses() {
				if !app.Installed {
					continue
				}
				was, known := prevActive[app.ID]
				prevActive[app.ID] = app.Active
				if known && was && !app.Active {
					notifyServiceDown(app.Name)
				}
			}
		}
	}
}

// runDigestTimer fires notifyDigest roughly once every 24h from process start. Not clock-aligned
// to a fixed time of day -- that's a nicety, not a correctness requirement, for a "quiet periodic
// summary."
func runDigestTimer(stop <-chan struct{}, metrics *MetricsCollector) {
	tick := time.NewTicker(24 * time.Hour)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
			cfg := LoadPushbulletConfig()
			if cfg.Enabled && cfg.AlertDigest {
				notifyDigest(metrics)
			}
		}
	}
}
