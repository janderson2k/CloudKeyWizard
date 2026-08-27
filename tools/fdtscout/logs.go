package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Log aggregation onto the device's real storage: the system journal's own rotation is short-lived
// (this hardware's stock image keeps very little history), so key units' logs get copied out to
// /volume with real retention -- browsable from the dashboard without needing the terminal.
// Falls back to /opt/fdtscout/data (the same directory everything else already persists to) if
// /volume isn't mounted (Format & Mount Storage wasn't run, or this is a plain Gen2) -- degraded
// retention window rather than a hard requirement on optional storage.

const logRetentionDays = 90

// logUnits mirrors the same hand-verified catalog apps.go already uses for status -- the units
// actually worth keeping a real history of, not "everything on the system."
var logUnits = []string{
	"fdtscout.service", "fail2ban.service", "unattended-upgrades.service", "plexmediaserver.service",
	"nzbget.service", "sonarr.service", "radarr.service", "prowlarr.service", "tailscaled.service",
	"wg-quick-vpn.service", "ssh.service",
}

func logsBaseDir() string {
	if info, err := os.Stat("/volume"); err == nil && info.IsDir() {
		return "/volume/fdtscout-logs"
	}
	return DataDir + "/logs"
}

// runLogAggregation pulls each unit's journal entries since the last run into a per-day file
// (<unit>-<YYYY-MM-DD>.log), then deletes anything older than logRetentionDays. journalctl's own
// --since accepts a timestamp directly, so "since last run" is tracked via a small marker file per
// unit rather than re-deriving it from file mtimes.
func runLogAggregation() {
	base := logsBaseDir()
	if err := os.MkdirAll(base, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "log aggregation: couldn't create %s: %v\n", base, err)
		return
	}

	for _, unit := range logUnits {
		markerPath := filepath.Join(base, "."+unit+".since")
		since := "24 hours ago"
		if data, err := os.ReadFile(markerPath); err == nil {
			since = strings.TrimSpace(string(data))
		}
		now := time.Now().Format("2006-01-02 15:04:05")

		out, err := exec.Command("journalctl", "-u", unit, "--since", since, "-o", "short-iso", "--no-pager").CombinedOutput()
		if err != nil && len(out) == 0 {
			continue // unit likely doesn't exist on this device -- not an error worth logging repeatedly
		}
		if len(strings.TrimSpace(string(out))) > 0 {
			dayFile := filepath.Join(base, fmt.Sprintf("%s-%s.log", unit, time.Now().Format("2006-01-02")))
			f, err := os.OpenFile(dayFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err == nil {
				f.Write(out)
				f.Close()
			}
		}
		os.WriteFile(markerPath, []byte(now), 0644)
	}

	pruneOldLogs(base)
}

func pruneOldLogs(base string) {
	entries, err := os.ReadDir(base)
	if err != nil {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -logRetentionDays)
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			os.Remove(filepath.Join(base, e.Name()))
		}
	}
}

func runLogAggregationLoop(stop <-chan struct{}) {
	runLogAggregation() // catch up immediately on startup rather than waiting a full hour
	tick := time.NewTicker(time.Hour)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
			runLogAggregation()
		}
	}
}

// --- Browsing --------------------------------------------------------------------------------

type LogFileInfo struct {
	Name     string    `json:"name"`
	Unit     string    `json:"unit"`
	Date     string    `json:"date"`
	SizeKB   float64   `json:"sizeKb"`
	Modified time.Time `json:"modified"`
}

func listLogFiles() []LogFileInfo {
	base := logsBaseDir()
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	var out []LogFileInfo
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") || !strings.HasSuffix(e.Name(), ".log") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		unit, date := splitLogFileName(e.Name())
		out = append(out, LogFileInfo{
			Name: e.Name(), Unit: unit, Date: date,
			SizeKB: float64(info.Size()) / 1024, Modified: info.ModTime(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Modified.After(out[j].Modified) })
	return out
}

func splitLogFileName(name string) (unit, date string) {
	name = strings.TrimSuffix(name, ".log")
	idx := strings.LastIndex(name, "-")
	// unit names end in ".service", followed by "-YYYY-MM-DD" -- find the date suffix by checking
	// the last 10 chars look like a date, rather than assuming a fixed split point.
	if len(name) >= 11 {
		maybeDate := name[len(name)-10:]
		if _, err := time.Parse("2006-01-02", maybeDate); err == nil {
			return strings.TrimSuffix(name[:len(name)-11], "-"), maybeDate
		}
	}
	if idx >= 0 {
		return name[:idx], name[idx+1:]
	}
	return name, ""
}

// readLogFile returns the last maxLines lines of a log file (bounded read, not the whole
// potentially-large file into the browser at once) -- name must be one of listLogFiles()'s own
// results, never a client-supplied path, closing off path traversal.
func readLogFile(name string, maxLines int) (string, error) {
	valid := false
	for _, f := range listLogFiles() {
		if f.Name == name {
			valid = true
			break
		}
	}
	if !valid {
		return "", fmt.Errorf("no such log file")
	}
	data, err := os.ReadFile(filepath.Join(logsBaseDir(), name))
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(data), "\n")
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return strings.Join(lines, "\n"), nil
}
