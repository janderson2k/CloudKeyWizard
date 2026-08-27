package main

import (
	"log"
	"net"
	"sync"
	"time"
)

// lcdManager owns the one FrameBuffer instance for the process's lifetime, plus the cycling
// state. Deliberately global-ish (a package-level singleton) rather than threaded through every
// caller -- there is exactly one physical display and exactly one thing drawing to it, so the
// extra indirection of passing it everywhere wouldn't buy anything. nil fb (open failed) is the
// expected, handled case, not an error state callers need to keep checking for -- refreshLCD()
// and the cycle loop just become no-ops.
var lcdManager struct {
	mu         sync.Mutex
	fb         *FrameBuffer
	screenIdx  int
	stopCycle  chan struct{}
	cycling    bool
	metrics    *MetricsCollector
}

// startLCDDisplay opens (or re-opens) the framebuffer and starts the multi-screen cycling loop if
// the config has more than one screen (a single screen just draws once and stays static -- no
// point ticking a timer for nothing to change). Safe to call more than once -- e.g. from the
// AppArmor "attempt fix" action retrying after a startup failure, or after a display config save
// -- closes any existing handle/stops any existing cycle first rather than leaking or running two
// cycles at once. Best-effort by design: a missing/inaccessible framebuffer must never take down
// the HTTPS console over it.
func startLCDDisplay(metrics *MetricsCollector) {
	lcdManager.mu.Lock()
	if lcdManager.fb != nil {
		lcdManager.fb.Close()
		lcdManager.fb = nil
	}
	if lcdManager.cycling {
		close(lcdManager.stopCycle)
		lcdManager.cycling = false
	}
	lcdManager.metrics = metrics
	lcdManager.mu.Unlock()

	cfg := LoadDisplayConfig()
	if !cfg.Enabled {
		logStartup("front-panel display is turned off in Settings")
		return
	}

	fb, err := OpenFrameBuffer()
	if err != nil {
		logStartup("front-panel display not available: " + err.Error())
		return
	}
	lcdManager.mu.Lock()
	lcdManager.fb = fb
	lcdManager.screenIdx = 0
	lcdManager.mu.Unlock()

	refreshLCD()

	if len(cfg.Screens) > 1 {
		stop := make(chan struct{})
		lcdManager.mu.Lock()
		lcdManager.stopCycle = stop
		lcdManager.cycling = true
		lcdManager.mu.Unlock()
		go runDisplayCycle(stop)
	}
}

// runDisplayCycle advances to the next screen on each tick. Re-reads the config from disk every
// tick (cheap for a file this small) rather than caching it in memory, so a config change made
// from the Settings/Front Panel UI takes effect on the very next tick without needing an explicit
// "reload" signal threaded through.
func runDisplayCycle(stop <-chan struct{}) {
	for {
		cfg := LoadDisplayConfig()
		interval := time.Duration(cfg.CycleSeconds) * time.Second
		select {
		case <-stop:
			return
		case <-time.After(interval):
			lcdManager.mu.Lock()
			lcdManager.screenIdx++
			lcdManager.mu.Unlock()
			refreshLCD()
		}
	}
}

// refreshLCD redraws the display with the current screen's widgets, live-evaluated. Called on
// startup, on every cycle tick, and any time setHostname() changes the hostname (so a hostname
// widget never has a chance to show stale information).
func refreshLCD() {
	lcdManager.mu.Lock()
	fb := lcdManager.fb
	metrics := lcdManager.metrics
	idx := lcdManager.screenIdx
	lcdManager.mu.Unlock()
	if fb == nil {
		return
	}

	cfg := LoadDisplayConfig()
	if len(cfg.Screens) == 0 {
		return
	}
	screen := cfg.Screens[idx%len(cfg.Screens)]

	lines := make([]string, 0, len(screen.Widgets))
	for _, w := range screen.Widgets {
		lines = append(lines, RenderWidget(w, metrics))
	}
	fb.DrawLines(lines)
}

func primaryLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		ipNet, ok := a.(*net.IPNet)
		if !ok || ipNet.IP.IsLoopback() {
			continue
		}
		if v4 := ipNet.IP.To4(); v4 != nil {
			return v4.String()
		}
	}
	return ""
}

func logStartup(msg string) {
	log.Println(msg)
}
