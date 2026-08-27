package main

import (
	"bufio"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Sample is one point in the metrics history.
type Sample struct {
	Time    time.Time `json:"t"`
	CPUPct  float64   `json:"cpu"`
	MemPct  float64   `json:"mem"`
	RootPct float64   `json:"diskRoot"`
	// VolumePct is -1 when /volume isn't mounted (Format & Mount Storage wasn't run, or this is a
	// Gen2 without extra storage) -- omitted from the chart client-side rather than plotted as 0.
	VolumePct float64 `json:"diskVolume"`
	// NetRxKBs/NetTxKBs are throughput (KB/s), not cumulative totals -- computed the same
	// delta-since-last-sample way as CPUPct, since /proc/net/dev only exposes a running counter.
	// Summed across every interface except loopback.
	NetRxKBs float64 `json:"netRxKBs"`
	NetTxKBs float64 `json:"netTxKBs"`
}

const (
	sampleInterval = time.Minute
	// 7 days at one sample/minute if /volume isn't mounted -- kept small deliberately since it's
	// living on the OS's own root partition. If /volume IS mounted (Format & Mount Storage was
	// run), history extends to 90 days on that real storage instead -- "months instead of a week,"
	// per the brainstorm this came from, now that there's a drive worth using for it. Either way:
	// each point is a few dozen bytes, so even 90 days at one sample/minute (~130k points) is a few
	// MB, no need for downsampling/rollups.
	maxSamplesShort = 7 * 24 * 60
	maxSamplesLong  = 90 * 24 * 60
	persistEvery    = 5 * time.Minute
)

// metricsRetentionCap and metricsFilePath are resolved once at startup based on whether /volume is
// mounted -- see the const block's comment above for why.
func metricsRetentionCap() int {
	if _, err := os.Stat("/volume"); err == nil {
		return maxSamplesLong
	}
	return maxSamplesShort
}

func metricsFilePath() string {
	if info, err := os.Stat("/volume"); err == nil && info.IsDir() {
		return "/volume/fdtscout-metrics.json"
	}
	return MetricsFile
}

// MetricsCollector owns a fixed-capacity ring buffer of samples, sampled on a timer and
// periodically flushed to disk so history survives a service restart (at most persistEvery of
// the newest samples are lost, not the whole history).
type MetricsCollector struct {
	mu       sync.RWMutex
	samples  []Sample
	filePath string
	cap      int

	prevCPUTotal float64
	prevCPUIdle  float64
	haveCPUPrev  bool

	prevNetRx, prevNetTx float64
	prevNetAt            time.Time
	haveNetPrev          bool

	// diskAlerted tracks which paths have already triggered a "disk space low" push, so the alert
	// fires once per crossing rather than every single sample while stuck above the threshold --
	// cleared once usage drops back below a lower hysteresis bound, not the same threshold that
	// triggered it (avoids re-alerting on every 1% flicker right at the line).
	diskAlerted map[string]bool
}

const (
	diskAlertThreshold  = 85.0
	diskAlertClearBelow = 80.0
)

func NewMetricsCollector() *MetricsCollector {
	m := &MetricsCollector{
		diskAlerted: map[string]bool{},
		filePath:    metricsFilePath(),
		cap:         metricsRetentionCap(),
	}
	m.loadFromDisk()
	return m
}

func (m *MetricsCollector) checkDiskAlert(path string, pct float64) {
	if pct < 0 {
		return
	}
	if pct >= diskAlertThreshold && !m.diskAlerted[path] {
		m.diskAlerted[path] = true
		cfg := LoadPushbulletConfig()
		if cfg.Enabled && cfg.AlertDiskFull {
			notifyDiskFull(path, pct)
		}
	} else if pct < diskAlertClearBelow {
		m.diskAlerted[path] = false
	}
}

func (m *MetricsCollector) loadFromDisk() {
	data, err := os.ReadFile(m.filePath)
	if err != nil && m.filePath != MetricsFile {
		// /volume is mounted now but might not have been on a previous run -- fall back to the
		// original DataDir location once, so switching to real storage doesn't look like history
		// silently reset to zero. Read-only fallback: the next persist() writes to m.filePath.
		data, err = os.ReadFile(MetricsFile)
	}
	if err != nil {
		return
	}
	var samples []Sample
	if json.Unmarshal(data, &samples) == nil {
		m.mu.Lock()
		m.samples = samples
		m.mu.Unlock()
	}
}

func (m *MetricsCollector) persist() {
	m.mu.RLock()
	data, err := json.Marshal(m.samples)
	m.mu.RUnlock()
	if err != nil {
		return
	}
	tmp := m.filePath + ".tmp"
	if os.WriteFile(tmp, data, 0644) == nil {
		os.Rename(tmp, m.filePath)
	}
}

// Run samples on a ticker until stop is closed. Meant to be launched in its own goroutine.
func (m *MetricsCollector) Run(stop <-chan struct{}) {
	sampleTicker := time.NewTicker(sampleInterval)
	persistTicker := time.NewTicker(persistEvery)
	defer sampleTicker.Stop()
	defer persistTicker.Stop()

	m.sampleOnce() // first point immediately, don't make the dashboard wait a full minute for data
	for {
		select {
		case <-stop:
			m.persist()
			return
		case <-sampleTicker.C:
			m.sampleOnce()
		case <-persistTicker.C:
			m.persist()
		}
	}
}

func (m *MetricsCollector) sampleOnce() {
	s := Sample{Time: time.Now().UTC(), VolumePct: -1}
	s.CPUPct = m.readCPUPercent()
	s.MemPct = readMemPercent()
	s.RootPct = readDiskPercent("/")
	m.checkDiskAlert("/", s.RootPct)
	if pct := readDiskPercent("/volume"); pct >= 0 {
		s.VolumePct = pct
		m.checkDiskAlert("/volume", pct)
	}
	s.NetRxKBs, s.NetTxKBs = m.readNetThroughput()

	m.mu.Lock()
	m.samples = append(m.samples, s)
	if len(m.samples) > m.cap {
		m.samples = m.samples[len(m.samples)-m.cap:]
	}
	m.mu.Unlock()
}

func (m *MetricsCollector) History() []Sample {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Sample, len(m.samples))
	copy(out, m.samples)
	return out
}

// readCPUPercent reads aggregate CPU time from /proc/stat and computes a percentage from the
// delta against the previous sample -- a single /proc/stat read is a cumulative counter since
// boot, not an instantaneous percentage, so this needs state carried between calls (m.prevCPU*).
func (m *MetricsCollector) readCPUPercent() float64 {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return 0
	}
	fields := strings.Fields(scanner.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0
	}

	var total, idle float64
	for i, field := range fields[1:] {
		v, err := strconv.ParseFloat(field, 64)
		if err != nil {
			continue
		}
		total += v
		if i == 3 { // "idle" is the 4th value in the cpu line
			idle = v
		}
	}

	defer func() { m.prevCPUTotal, m.prevCPUIdle, m.haveCPUPrev = total, idle, true }()
	if !m.haveCPUPrev {
		return 0
	}
	deltaTotal := total - m.prevCPUTotal
	deltaIdle := idle - m.prevCPUIdle
	if deltaTotal <= 0 {
		return 0
	}
	return clampPct((deltaTotal - deltaIdle) / deltaTotal * 100)
}

// readNetThroughput sums RX/TX bytes across every interface except loopback from /proc/net/dev,
// then converts to KB/s using the elapsed time since the previous sample (not just sampleInterval
// -- the timer can drift or a sample can be skipped, and using the real elapsed time keeps the
// rate honest either way).
func (m *MetricsCollector) readNetThroughput() (rxKBs, txKBs float64) {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return 0, 0
	}
	defer f.Close()

	var rxTotal, txTotal float64
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		colon := strings.Index(line, ":")
		if colon < 0 {
			continue
		}
		iface := strings.TrimSpace(line[:colon])
		if iface == "lo" || iface == "" {
			continue
		}
		fields := strings.Fields(line[colon+1:])
		if len(fields) < 9 {
			continue
		}
		rxBytes, _ := strconv.ParseFloat(fields[0], 64)
		txBytes, _ := strconv.ParseFloat(fields[8], 64)
		rxTotal += rxBytes
		txTotal += txBytes
	}

	now := time.Now()
	defer func() { m.prevNetRx, m.prevNetTx, m.prevNetAt, m.haveNetPrev = rxTotal, txTotal, now, true }()
	if !m.haveNetPrev {
		return 0, 0
	}
	elapsed := now.Sub(m.prevNetAt).Seconds()
	if elapsed <= 0 {
		return 0, 0
	}
	rxKBs = (rxTotal - m.prevNetRx) / 1024 / elapsed
	txKBs = (txTotal - m.prevNetTx) / 1024 / elapsed
	if rxKBs < 0 {
		rxKBs = 0 // counter reset (interface flap, reboot) -- report 0 rather than a bogus negative
	}
	if txKBs < 0 {
		txKBs = 0
	}
	return rxKBs, txKBs
}

func readMemPercent() float64 {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()

	var total, available float64
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			total = parseMeminfoKB(line)
		case strings.HasPrefix(line, "MemAvailable:"):
			available = parseMeminfoKB(line)
		}
	}
	if total <= 0 {
		return 0
	}
	return clampPct((total - available) / total * 100)
}

func parseMeminfoKB(line string) float64 {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0
	}
	v, _ := strconv.ParseFloat(fields[1], 64)
	return v
}

// readDiskPercent returns -1 if path doesn't exist/isn't a mount point worth reporting (so the
// caller can distinguish "0% used" from "not applicable on this device").
func readDiskPercent(path string) float64 {
	if _, err := os.Stat(path); err != nil {
		return -1
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return -1
	}
	total := float64(stat.Blocks) * float64(stat.Bsize)
	free := float64(stat.Bavail) * float64(stat.Bsize)
	if total <= 0 {
		return -1
	}
	return clampPct((total - free) / total * 100)
}

func clampPct(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}
