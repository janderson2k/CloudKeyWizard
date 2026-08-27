package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// The Monitoring tab's anchor feature: user-defined checks (ping/TCP/HTTP/DNS) against hosts THE
// USER adds by hand, run on a schedule, graphed the same way the existing CPU/mem/disk sparklines
// work. Deliberately separate from the Health tab -- Health is "how's the Cloud Key itself doing,"
// this is "what is the Cloud Key watching on the user's behalf."

type MonitorType string

const (
	MonitorPing MonitorType = "ping"
	MonitorTCP  MonitorType = "tcp"
	MonitorHTTP MonitorType = "http"
	MonitorDNS  MonitorType = "dns"
)

type Monitor struct {
	ID           string      `json:"id"`
	Label        string      `json:"label"`
	Type         MonitorType `json:"type"`
	Target       string      `json:"target"`                 // host, host:port already split into Target+Port for tcp, URL for http, hostname for dns
	Port         int         `json:"port,omitempty"`         // tcp only
	ExpectedText string      `json:"expectedText,omitempty"` // http only, optional
	IntervalSecs int         `json:"intervalSecs"`
	Enabled      bool        `json:"enabled"`
}

type MonitorResult struct {
	Time      time.Time `json:"t"`
	Up        bool      `json:"up"`
	LatencyMs float64   `json:"latencyMs"`
	Detail    string    `json:"detail,omitempty"`
}

const maxResultsPerMonitor = 2016 // 7 days at one sample/5min -- monitors default to a much slower cadence than the CPU/mem/disk collector

type MonitorEngine struct {
	mu       sync.RWMutex
	monitors []Monitor
	history  map[string][]MonitorResult // by Monitor.ID
	lastRun  map[string]time.Time
	// downSince tracks when a currently-down monitor first went down, for the "down since"
	// indicator the UI shows -- cleared the moment it comes back up.
	downSince map[string]time.Time
}

func NewMonitorEngine() *MonitorEngine {
	e := &MonitorEngine{
		history:   map[string][]MonitorResult{},
		lastRun:   map[string]time.Time{},
		downSince: map[string]time.Time{},
	}
	e.loadConfig()
	e.loadHistory()
	return e
}

func (e *MonitorEngine) loadConfig() {
	data, err := os.ReadFile(MonitorsConfigFile)
	if err != nil {
		return
	}
	var monitors []Monitor
	if json.Unmarshal(data, &monitors) == nil {
		e.mu.Lock()
		e.monitors = monitors
		e.mu.Unlock()
	}
}

func (e *MonitorEngine) saveConfig() error {
	e.mu.RLock()
	data, err := json.MarshalIndent(e.monitors, "", "  ")
	e.mu.RUnlock()
	if err != nil {
		return err
	}
	tmp := MonitorsConfigFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, MonitorsConfigFile)
}

func (e *MonitorEngine) loadHistory() {
	data, err := os.ReadFile(MonitorsHistoryFile)
	if err != nil {
		return
	}
	var history map[string][]MonitorResult
	if json.Unmarshal(data, &history) == nil {
		e.mu.Lock()
		e.history = history
		e.mu.Unlock()
	}
}

func (e *MonitorEngine) persistHistory() {
	e.mu.RLock()
	data, err := json.Marshal(e.history)
	e.mu.RUnlock()
	if err != nil {
		return
	}
	tmp := MonitorsHistoryFile + ".tmp"
	if os.WriteFile(tmp, data, 0644) == nil {
		os.Rename(tmp, MonitorsHistoryFile)
	}
}

func (e *MonitorEngine) List() []Monitor {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]Monitor, len(e.monitors))
	copy(out, e.monitors)
	return out
}

func (e *MonitorEngine) Save(monitors []Monitor) error {
	for i := range monitors {
		if monitors[i].ID == "" {
			monitors[i].ID = fmt.Sprintf("mon-%d", time.Now().UnixNano())
		}
		if monitors[i].IntervalSecs < 30 {
			monitors[i].IntervalSecs = 30 // floor -- nothing needs sub-30s polling here, and it keeps a misconfigured interval from hammering a target
		}
	}
	e.mu.Lock()
	e.monitors = monitors
	e.mu.Unlock()
	return e.saveConfig()
}

func (e *MonitorEngine) HistoryFor(id string) []MonitorResult {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]MonitorResult, len(e.history[id]))
	copy(out, e.history[id])
	return out
}

type MonitorSnapshot struct {
	Monitor
	LastResult *MonitorResult `json:"lastResult,omitempty"`
	UptimePct  float64        `json:"uptimePct"`
	DownSince  *time.Time     `json:"downSince,omitempty"`
}

// Snapshots returns the current state of every monitor for the dashboard's list view -- latest
// result, an uptime % over the retained history, and a down-since timestamp when applicable.
func (e *MonitorEngine) Snapshots() []MonitorSnapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]MonitorSnapshot, 0, len(e.monitors))
	for _, mon := range e.monitors {
		snap := MonitorSnapshot{Monitor: mon}
		results := e.history[mon.ID]
		if len(results) > 0 {
			last := results[len(results)-1]
			snap.LastResult = &last
			upCount := 0
			for _, r := range results {
				if r.Up {
					upCount++
				}
			}
			snap.UptimePct = float64(upCount) / float64(len(results)) * 100
		}
		if t, down := e.downSince[mon.ID]; down {
			snap.DownSince = &t
		}
		out = append(out, snap)
	}
	return out
}

// RunNow executes one monitor immediately (used by an on-demand "check now" action, and by the
// Pushbullet ping command for a target not already on the watch list) without waiting for its
// scheduled interval.
func (e *MonitorEngine) RunNow(id string) (MonitorResult, error) {
	for _, mon := range e.List() {
		if mon.ID == id {
			result := checkMonitor(mon)
			e.mu.Lock()
			e.history[mon.ID] = append(e.history[mon.ID], result)
			if len(e.history[mon.ID]) > maxResultsPerMonitor {
				e.history[mon.ID] = e.history[mon.ID][len(e.history[mon.ID])-maxResultsPerMonitor:]
			}
			e.mu.Unlock()
			return result, nil
		}
	}
	return MonitorResult{}, fmt.Errorf("no such monitor: %s", id)
}

// Run loops forever (until stop) checking whether each enabled monitor's own interval has elapsed,
// and if so, runs it. Deliberately one shared ticker checking all monitors' due-ness rather than
// one goroutine per monitor -- simpler, and the number of monitors here is expected to be small
// (tens, not thousands).
func (e *MonitorEngine) Run(stop <-chan struct{}) {
	tick := time.NewTicker(10 * time.Second)
	persistTick := time.NewTicker(5 * time.Minute)
	defer tick.Stop()
	defer persistTick.Stop()
	for {
		select {
		case <-stop:
			e.persistHistory()
			return
		case <-tick.C:
			e.runDue()
		case <-persistTick.C:
			e.persistHistory()
		}
	}
}

func (e *MonitorEngine) runDue() {
	now := time.Now()
	for _, mon := range e.List() {
		if !mon.Enabled {
			continue
		}
		last, ran := e.lastRun[mon.ID]
		if ran && now.Sub(last) < time.Duration(mon.IntervalSecs)*time.Second {
			continue
		}
		e.lastRun[mon.ID] = now
		go e.runOne(mon)
	}
}

func (e *MonitorEngine) runOne(mon Monitor) {
	result := checkMonitor(mon)

	e.mu.Lock()
	e.history[mon.ID] = append(e.history[mon.ID], result)
	if len(e.history[mon.ID]) > maxResultsPerMonitor {
		e.history[mon.ID] = e.history[mon.ID][len(e.history[mon.ID])-maxResultsPerMonitor:]
	}
	wasDown := !e.downSince[mon.ID].IsZero()
	if !result.Up && !wasDown {
		e.downSince[mon.ID] = result.Time
	} else if result.Up {
		delete(e.downSince, mon.ID)
	}
	e.mu.Unlock()
}

func checkMonitor(mon Monitor) MonitorResult {
	switch mon.Type {
	case MonitorPing:
		up, latency, detail := pingHost(mon.Target, 3)
		return MonitorResult{Time: time.Now(), Up: up, LatencyMs: latency, Detail: detail}
	case MonitorTCP:
		up, latency, detail := tcpCheck(mon.Target, mon.Port, 5)
		return MonitorResult{Time: time.Now(), Up: up, LatencyMs: latency, Detail: detail}
	case MonitorHTTP:
		up, status, latency, detail := httpCheck(mon.Target, mon.ExpectedText, 10)
		if detail == "" {
			detail = fmt.Sprintf("HTTP %d", status)
		}
		return MonitorResult{Time: time.Now(), Up: up, LatencyMs: latency, Detail: detail}
	case MonitorDNS:
		up, addrs, detail := dnsCheck(mon.Target, 5)
		if up && detail == "" {
			detail = fmt.Sprintf("resolved: %v", addrs)
		}
		return MonitorResult{Time: time.Now(), Up: up, Detail: detail}
	default:
		return MonitorResult{Time: time.Now(), Up: false, Detail: "unknown monitor type"}
	}
}
