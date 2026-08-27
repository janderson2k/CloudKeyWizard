package main

import (
	"encoding/json"
	"os"
)

// Fixed, deliberately not configurable via flags/env -- this binary is built specifically to be
// installed by CloudKey Wizard's "fdtscout" Extra at these exact paths, not a general-purpose
// tool with its own install-location flexibility.
const (
	DataDir      = "/opt/fdtscout/data"
	UsersFile    = DataDir + "/users.json"
	MetricsFile  = DataDir + "/metrics.json"
	TLSDir       = "/etc/fdtscout/tls"
	CertFile     = TLSDir + "/cert.pem"
	KeyFile      = TLSDir + "/key.pem"
	ConfigFile   = "/etc/fdtscout/config.json"
	DefaultPort  = 443
	DefaultRedir = false

	// Monitoring tab: ping/TCP/HTTP/DNS watch list + their result history, WAN speed test history,
	// public IP tracking history. Config and history are separate files (same split MetricsFile
	// already uses) so editing the watch list never risks the accumulated history data.
	MonitorsConfigFile  = DataDir + "/monitors.json"
	MonitorsHistoryFile = DataDir + "/monitor-history.json"
	WANSpeedFile        = DataDir + "/wanspeed.json"
	PublicIPFile        = DataDir + "/publicip.json"
	DDNSConfigFile      = "/etc/fdtscout/ddns.json"

	// Pushbullet: proactive alerts + the two-way callsign command channel. Config under /etc since
	// it holds a credential (the API access token), same directory class as TLS -- not under
	// DataDir, which is more general app state.
	PushbulletConfigFile = "/etc/fdtscout/pushbullet.json"
)

// RuntimeConfig is the one piece of config that's actually user-adjustable at runtime (from the
// Certificates tab: "change the port," "redirect 80 to 443") -- unlike the fixed paths above,
// which are install-time constants. Changing it requires a process restart to take effect (the
// HTTPS listener can't rebind to a new port live without dropping every open connection anyway),
// so the handler that updates this schedules a `systemctl restart fdtscout` itself after
// responding -- see handlers_config.go.
type RuntimeConfig struct {
	Port         int  `json:"port"`
	RedirectHTTP bool `json:"redirectHttp"`
}

func LoadRuntimeConfig() RuntimeConfig {
	cfg := RuntimeConfig{Port: DefaultPort, RedirectHTTP: DefaultRedir}
	data, err := os.ReadFile(ConfigFile)
	if err != nil {
		return cfg
	}
	_ = json.Unmarshal(data, &cfg) // corrupt/partial config -> fall back to whatever defaults survived
	if cfg.Port <= 0 || cfg.Port > 65535 {
		cfg.Port = DefaultPort
	}
	return cfg
}

func SaveRuntimeConfig(cfg RuntimeConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := ConfigFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, ConfigFile)
}

func ensureDirs() error {
	if err := os.MkdirAll(DataDir, 0700); err != nil {
		return err
	}
	if err := os.MkdirAll(TLSDir, 0700); err != nil {
		return err
	}
	return os.MkdirAll("/etc/fdtscout", 0700)
}
