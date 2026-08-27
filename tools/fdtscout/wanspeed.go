package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

// WAN speed test: periodic download/upload/latency measurement, graphed like everything else on
// the Monitoring tab. Deliberately implemented as a direct HTTP throughput measurement against
// Cloudflare's own public speed-test endpoints (speed.cloudflare.com -- the same backend real
// speed-test tools like librespeed use) rather than shelling out to a separate speedtest-cli
// binary: keeps this dependency-free and consistent with the rest of the project's "no compiler/
// extra package needed on the device" philosophy. Real use case named by the user: documented
// proof of ISP degradation over time, not just "it feels slow."

const (
	speedTestHost      = "https://speed.cloudflare.com"
	speedTestDownBytes = 25_000_000 // 25MB -- big enough to get past TCP slow-start and read a real sustained rate
	speedTestUpBytes   = 10_000_000
	maxWANSpeedSamples = 24 * 90 // 90 days at one sample/hour
)

type WANSpeedSample struct {
	Time         time.Time `json:"t"`
	LatencyMs    float64   `json:"latencyMs"`
	DownloadMbps float64   `json:"downloadMbps"`
	UploadMbps   float64   `json:"uploadMbps"`
	Error        string    `json:"error,omitempty"`
}

type WANSpeedHistory struct {
	mu      sync.RWMutex
	samples []WANSpeedSample
}

func NewWANSpeedHistory() *WANSpeedHistory {
	h := &WANSpeedHistory{}
	h.load()
	return h
}

func (h *WANSpeedHistory) load() {
	data, err := os.ReadFile(WANSpeedFile)
	if err != nil {
		return
	}
	var samples []WANSpeedSample
	if json.Unmarshal(data, &samples) == nil {
		h.mu.Lock()
		h.samples = samples
		h.mu.Unlock()
	}
}

func (h *WANSpeedHistory) persist() {
	h.mu.RLock()
	data, err := json.Marshal(h.samples)
	h.mu.RUnlock()
	if err != nil {
		return
	}
	tmp := WANSpeedFile + ".tmp"
	if os.WriteFile(tmp, data, 0644) == nil {
		os.Rename(tmp, WANSpeedFile)
	}
}

func (h *WANSpeedHistory) History() []WANSpeedSample {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]WANSpeedSample, len(h.samples))
	copy(out, h.samples)
	return out
}

func (h *WANSpeedHistory) RunNow() WANSpeedSample {
	sample := runSpeedTest()
	h.mu.Lock()
	h.samples = append(h.samples, sample)
	if len(h.samples) > maxWANSpeedSamples {
		h.samples = h.samples[len(h.samples)-maxWANSpeedSamples:]
	}
	h.mu.Unlock()
	go h.persist()
	return sample
}

// Run fires RunNow on the given interval (a config-driven "test every N hours" -- the user named
// hourly/daily as reasonable defaults) until stop is closed.
func (h *WANSpeedHistory) Run(stop <-chan struct{}, interval time.Duration) {
	if interval <= 0 {
		interval = time.Hour
	}
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
			h.RunNow()
		}
	}
}

func runSpeedTest() WANSpeedSample {
	sample := WANSpeedSample{Time: time.Now()}
	client := &http.Client{Timeout: 30 * time.Second}

	// Latency: a tiny (near-zero-byte) request, timed.
	start := time.Now()
	resp, err := client.Get(speedTestHost + "/__down?bytes=0")
	if err != nil {
		sample.Error = "latency check failed: " + err.Error()
		return sample
	}
	resp.Body.Close()
	sample.LatencyMs = float64(time.Since(start).Milliseconds())

	// Download: fetch a fixed-size payload, measure sustained throughput.
	start = time.Now()
	resp, err = client.Get(speedTestHost + "/__down?bytes=" + strconv.Itoa(speedTestDownBytes))
	if err != nil {
		sample.Error = "download test failed: " + err.Error()
		return sample
	}
	n, _ := io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	elapsed := time.Since(start).Seconds()
	if elapsed > 0 && n > 0 {
		sample.DownloadMbps = float64(n) * 8 / 1_000_000 / elapsed
	}

	// Upload: POST a fixed-size random-ish payload, measure sustained throughput.
	payload := bytes.Repeat([]byte("fdtscout-speedtest-"), speedTestUpBytes/19+1)[:speedTestUpBytes]
	start = time.Now()
	resp, err = client.Post(speedTestHost+"/__up", "application/octet-stream", bytes.NewReader(payload))
	if err != nil {
		sample.Error = "upload test failed: " + err.Error()
		return sample
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	elapsed = time.Since(start).Seconds()
	if elapsed > 0 {
		sample.UploadMbps = float64(len(payload)) * 8 / 1_000_000 / elapsed
	}

	return sample
}
