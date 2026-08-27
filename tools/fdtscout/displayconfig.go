package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Multi-screen cycling display config, on by default. Widget values are read live from the same
// collectors the Health tab already uses (MetricsCollector for CPU/mem/disk, os.Hostname/
// primaryLocalIP for identity) rather than duplicating sampling logic -- CPU/mem/disk on the panel
// update on the same ~60s cadence as the Health tab's own charts (the metrics sampler's interval),
// not on every screen flip; that's a deliberate trade rather than running a second higher-
// frequency sampler just for a tiny display.

const displayConfigFile = "/etc/fdtscout/display-config.json"

type WidgetType string

const (
	WidgetHostname   WidgetType = "hostname"
	WidgetIP         WidgetType = "ip"
	WidgetTime       WidgetType = "time"
	WidgetCPU        WidgetType = "cpu"
	WidgetMem        WidgetType = "mem"
	WidgetDiskRoot   WidgetType = "diskRoot"
	WidgetDiskVolume WidgetType = "diskVolume"
	WidgetUptime     WidgetType = "uptime"
	WidgetCustomText WidgetType = "customText"
)

type Widget struct {
	Type       WidgetType `json:"type"`
	Label      string     `json:"label,omitempty"`      // optional prefix, e.g. "CPU: " -- defaults per-type if empty
	CustomText string     `json:"customText,omitempty"` // only used when Type == WidgetCustomText
}

type Screen struct {
	Name    string   `json:"name"`
	Widgets []Widget `json:"widgets"`
}

type DisplayConfig struct {
	Enabled      bool     `json:"enabled"`
	CycleSeconds int      `json:"cycleSeconds"`
	Screens      []Screen `json:"screens"`
}

// defaultDisplayConfig matches exactly what was agreed: on by default, hostname/IP/time on one
// screen, CPU/RAM/disk utilization on another -- a sensible out-of-the-box experience, fully
// editable from there.
func defaultDisplayConfig() DisplayConfig {
	return DisplayConfig{
		Enabled:      true,
		CycleSeconds: 10,
		Screens: []Screen{
			{
				Name: "System",
				Widgets: []Widget{
					{Type: WidgetHostname},
					{Type: WidgetIP},
					{Type: WidgetTime},
				},
			},
			{
				Name: "Resources",
				Widgets: []Widget{
					{Type: WidgetCPU},
					{Type: WidgetMem},
					{Type: WidgetDiskRoot},
				},
			},
		},
	}
}

func LoadDisplayConfig() DisplayConfig {
	data, err := os.ReadFile(displayConfigFile)
	if err != nil {
		return defaultDisplayConfig()
	}
	var cfg DisplayConfig
	if json.Unmarshal(data, &cfg) != nil || len(cfg.Screens) == 0 {
		return defaultDisplayConfig()
	}
	if cfg.CycleSeconds <= 0 {
		cfg.CycleSeconds = 10
	}
	return cfg
}

func SaveDisplayConfig(cfg DisplayConfig) error {
	if cfg.CycleSeconds <= 0 {
		return fmt.Errorf("cycle interval must be at least 1 second")
	}
	if len(cfg.Screens) == 0 {
		return fmt.Errorf("at least one screen is required")
	}
	for i, s := range cfg.Screens {
		if len(s.Widgets) == 0 {
			return fmt.Errorf("screen %d (%q) has no widgets", i+1, s.Name)
		}
		if len(s.Widgets) > 4 {
			return fmt.Errorf("screen %d (%q) has %d widgets -- the display only fits about 4 lines legibly", i+1, s.Name, len(s.Widgets))
		}
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll("/etc/fdtscout", 0700); err != nil {
		return err
	}
	tmp := displayConfigFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, displayConfigFile)
}

// RenderWidget evaluates one widget's current value as the line of text it should show. Live
// system values (CPU/mem/disk) come from the passed-in metrics collector's most recent sample
// rather than being computed here, so there's exactly one place doing that math.
func RenderWidget(w Widget, metrics *MetricsCollector) string {
	label := w.Label
	switch w.Type {
	case WidgetHostname:
		h, _ := os.Hostname()
		return withLabel(label, h)
	case WidgetIP:
		if label == "" {
			label = "IP: "
		}
		ip := primaryLocalIP()
		if ip == "" {
			ip = "(none)"
		}
		return label + ip
	case WidgetTime:
		return time.Now().Format("15:04:05  Jan 2")
	case WidgetCPU:
		if label == "" {
			label = "CPU: "
		}
		return fmt.Sprintf("%s%.0f%%", label, latestOrZero(metrics).CPUPct)
	case WidgetMem:
		if label == "" {
			label = "RAM: "
		}
		return fmt.Sprintf("%s%.0f%%", label, latestOrZero(metrics).MemPct)
	case WidgetDiskRoot:
		if label == "" {
			label = "Disk: "
		}
		return fmt.Sprintf("%s%.0f%%", label, latestOrZero(metrics).RootPct)
	case WidgetDiskVolume:
		if label == "" {
			label = "Vol: "
		}
		s := latestOrZero(metrics)
		if s.VolumePct < 0 {
			return label + "n/a"
		}
		return fmt.Sprintf("%s%.0f%%", label, s.VolumePct)
	case WidgetUptime:
		if label == "" {
			label = "Up: "
		}
		return label + formatUptime(readUptime())
	case WidgetCustomText:
		return w.CustomText
	default:
		return ""
	}
}

func withLabel(label, value string) string {
	if label == "" {
		return value
	}
	return label + value
}

func latestOrZero(metrics *MetricsCollector) Sample {
	history := metrics.History()
	if len(history) == 0 {
		return Sample{VolumePct: -1}
	}
	return history[len(history)-1]
}

func formatUptime(seconds float64) string {
	d := time.Duration(seconds) * time.Second
	days := int(d.Hours() / 24)
	hours := int(d.Hours()) % 24
	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	return fmt.Sprintf("%dh %dm", hours, int(d.Minutes())%60)
}
