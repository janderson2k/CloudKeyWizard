package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Static IP / DHCP control, built exactly to the user's own specified safety design (2026-08-23):
// apply the change, start a 5-minute timer, and either an explicit "Accept IP changes" confirmation
// (shown as a splash the moment anyone reaches the dashboard while a change is pending -- covers
// "presented at login" since that's the first thing rendered after logging in) or an automatic
// revert to the prior network config if nothing confirms it in time.
//
// Deliberately does NOT try to identify/edit whichever higher-level config system this OS uses
// (systemd-networkd / netplan / ifupdown / NetworkManager) -- neither the jnovack/cloudkey wiki nor
// any bundled script says which one this device runs, and guessing wrong at file syntax is exactly
// the kind of mistake that could strand it. Instead this works directly against the live kernel
// routing table with `ip` (iproute2), which is confirmed present on this hardware already -- the
// bundled WireGuard scripts use it. That sidesteps the unknown entirely for the safety-critical
// live change; only a reboot-persistence step (not required to be right for safety) touches
// anything backend-specific.
const (
	pendingIPChangeFile = DataDir + "/pending-ip-change.json"
	ipChangeWindow      = 5 * time.Minute
)

type NetworkSnapshot struct {
	Interface string   `json:"interface"`
	Addresses []string `json:"addresses"` // CIDR strings, exactly as `ip -o -4 addr show dev <iface>` reported them
	Gateway   string   `json:"gateway"`   // may be empty if there was no default route
	WasDHCP   bool     `json:"wasDhcp"`   // best-effort: was a dhclient process running for this interface beforehand
}

type PendingIPChange struct {
	Snapshot   NetworkSnapshot `json:"snapshot"`
	NewMode    string          `json:"newMode"` // "static" or "dhcp"
	NewAddress string          `json:"newAddress,omitempty"`
	NewGateway string          `json:"newGateway,omitempty"`
	AppliedAt  time.Time       `json:"appliedAt"`
	RevertAt   time.Time       `json:"revertAt"`
	Confirmed  bool            `json:"confirmed"`
}

type NetConfigManager struct {
	mu      sync.Mutex
	pending *PendingIPChange
	timer   *time.Timer
}

func NewNetConfigManager() *NetConfigManager {
	m := &NetConfigManager{}
	m.resumeFromDisk()
	return m
}

// resumeFromDisk is what makes the safety mechanism actually safe across a service restart mid-
// window -- without this, a crash or an unrelated restart (e.g. from the Certificates tab's own
// port-change self-restart) during the 5-minute window would silently lose the pending revert,
// leaving a possibly-bad IP in place forever with nothing watching it.
func (m *NetConfigManager) resumeFromDisk() {
	data, err := os.ReadFile(pendingIPChangeFile)
	if err != nil {
		return
	}
	var pending PendingIPChange
	if json.Unmarshal(data, &pending) != nil || pending.Confirmed {
		os.Remove(pendingIPChangeFile)
		return
	}
	m.mu.Lock()
	m.pending = &pending
	m.mu.Unlock()

	remaining := time.Until(pending.RevertAt)
	if remaining <= 0 {
		m.revert("resumed after restart, window already elapsed")
		return
	}
	m.armTimer(remaining)
}

func (m *NetConfigManager) armTimer(d time.Duration) {
	m.timer = time.AfterFunc(d, func() {
		m.revert("5-minute confirmation window elapsed without a response")
	})
}

func (m *NetConfigManager) persist() {
	m.mu.Lock()
	pending := m.pending
	m.mu.Unlock()
	if pending == nil {
		os.Remove(pendingIPChangeFile)
		return
	}
	data, err := json.MarshalIndent(pending, "", "  ")
	if err != nil {
		return
	}
	tmp := pendingIPChangeFile + ".tmp"
	if os.WriteFile(tmp, data, 0600) == nil {
		os.Rename(tmp, pendingIPChangeFile)
	}
}

func (m *NetConfigManager) Status() map[string]any {
	iface, addrs, gw := currentNetworkState()
	m.mu.Lock()
	pending := m.pending
	m.mu.Unlock()

	status := map[string]any{
		"interface": iface,
		"addresses": addrs,
		"gateway":   gw,
	}
	if pending != nil {
		status["pending"] = map[string]any{
			"newMode":           pending.NewMode,
			"newAddress":        pending.NewAddress,
			"appliedAt":         pending.AppliedAt,
			"revertAt":          pending.RevertAt,
			"secondsRemaining":  int(time.Until(pending.RevertAt).Seconds()),
		}
	}
	return status
}

// Apply snapshots the current state, switches to the requested mode, and arms the revert timer.
// Returns an error only for a genuine failure to apply (bad interface, `ip` command failing) --
// once applied, nothing here can fail "successfully towards a bad state" silently, since the
// revert path is always armed before this returns.
func (m *NetConfigManager) Apply(mode, address, gateway string) error {
	m.mu.Lock()
	if m.pending != nil {
		m.mu.Unlock()
		return fmt.Errorf("a change is already pending confirmation -- accept or wait for it to resolve first")
	}
	m.mu.Unlock()

	iface, addrs, gw := currentNetworkState()
	if iface == "" {
		return fmt.Errorf("couldn't identify the primary network interface (no default route) -- refusing to guess")
	}
	snapshot := NetworkSnapshot{Interface: iface, Addresses: addrs, Gateway: gw, WasDHCP: dhclientRunning(iface)}

	switch mode {
	case "static":
		if address == "" {
			return fmt.Errorf("address is required for static mode (CIDR, e.g. 192.168.1.50/24)")
		}
		if err := flushAndSetStatic(iface, address, gateway); err != nil {
			return err
		}
	case "dhcp":
		if err := switchToDHCP(iface); err != nil {
			return err
		}
	default:
		return fmt.Errorf("mode must be 'static' or 'dhcp'")
	}

	now := time.Now()
	pending := &PendingIPChange{
		Snapshot: snapshot, NewMode: mode, NewAddress: address, NewGateway: gateway,
		AppliedAt: now, RevertAt: now.Add(ipChangeWindow),
	}
	m.mu.Lock()
	m.pending = pending
	m.mu.Unlock()
	m.persist()
	m.armTimer(ipChangeWindow)
	return nil
}

// Confirm is what the "Accept IP changes" splash calls -- cancels the revert timer and clears the
// pending state, keeping the new config permanently (best-effort persistence across reboot is
// attempted separately, see persistBestEffort below).
func (m *NetConfigManager) Confirm() error {
	m.mu.Lock()
	pending := m.pending
	if pending == nil {
		m.mu.Unlock()
		return fmt.Errorf("no pending change to confirm")
	}
	if m.timer != nil {
		m.timer.Stop()
	}
	m.pending = nil
	m.mu.Unlock()
	os.Remove(pendingIPChangeFile)
	persistBestEffort(pending)
	return nil
}

func (m *NetConfigManager) revert(reason string) {
	m.mu.Lock()
	pending := m.pending
	m.pending = nil
	m.mu.Unlock()
	if pending == nil {
		return
	}

	if pending.Snapshot.WasDHCP {
		_ = switchToDHCP(pending.Snapshot.Interface)
	} else {
		addr := ""
		if len(pending.Snapshot.Addresses) > 0 {
			addr = pending.Snapshot.Addresses[0]
		}
		_ = flushAndSetStatic(pending.Snapshot.Interface, addr, pending.Snapshot.Gateway)
		for _, extra := range pending.Snapshot.Addresses[1:] {
			_ = exec.Command("ip", "addr", "add", extra, "dev", pending.Snapshot.Interface).Run()
		}
	}
	os.Remove(pendingIPChangeFile)
	fmt.Fprintf(os.Stderr, "network config reverted (%s)\n", reason)
}

// --- low-level network operations -----------------------------------------

func currentNetworkState() (iface string, addresses []string, gateway string) {
	out, err := exec.Command("ip", "-o", "-4", "route", "show", "default").CombinedOutput()
	if err == nil {
		fields := strings.Fields(string(out))
		for i, f := range fields {
			if f == "dev" && i+1 < len(fields) {
				iface = fields[i+1]
			}
			if f == "via" && i+1 < len(fields) {
				gateway = fields[i+1]
			}
		}
	}
	if iface == "" {
		return "", nil, ""
	}

	out, err = exec.Command("ip", "-o", "-4", "addr", "show", "dev", iface).CombinedOutput()
	if err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			fields := strings.Fields(line)
			for i, f := range fields {
				if f == "inet" && i+1 < len(fields) {
					addresses = append(addresses, fields[i+1])
				}
			}
		}
	}
	return iface, addresses, gateway
}

func dhclientRunning(iface string) bool {
	return exec.Command("pgrep", "-f", "dhclient.*"+iface).Run() == nil
}

func flushAndSetStatic(iface, cidr, gateway string) error {
	if out, err := exec.Command("ip", "addr", "flush", "dev", iface).CombinedOutput(); err != nil {
		return fmt.Errorf("flushing existing addresses: %s: %w", strings.TrimSpace(string(out)), err)
	}
	if cidr != "" {
		if out, err := exec.Command("ip", "addr", "add", cidr, "dev", iface).CombinedOutput(); err != nil {
			return fmt.Errorf("setting address: %s: %w", strings.TrimSpace(string(out)), err)
		}
	}
	_ = exec.Command("ip", "link", "set", iface, "up").Run()
	if gateway != "" {
		if out, err := exec.Command("ip", "route", "replace", "default", "via", gateway, "dev", iface).CombinedOutput(); err != nil {
			return fmt.Errorf("setting default route: %s: %w", strings.TrimSpace(string(out)), err)
		}
	}
	return nil
}

// switchToDHCP kills any dhclient already running for this interface (a stale one can hold the
// lease file lock and refuse to start a fresh request), flushes addresses, then requests a new
// lease -- dhclient (the standard ISC client, referenced by the jnovack wiki's own "DHCP renewed"
// verification step) is tried first, with busybox's udhcpc as a fallback if it isn't present.
func switchToDHCP(iface string) error {
	_ = exec.Command("pkill", "-f", "dhclient.*"+iface).Run()
	_ = exec.Command("ip", "addr", "flush", "dev", iface).Run()
	_ = exec.Command("ip", "link", "set", iface, "up").Run()

	if _, err := exec.LookPath("dhclient"); err == nil {
		if out, err := exec.Command("dhclient", iface).CombinedOutput(); err != nil {
			return fmt.Errorf("dhclient failed: %s: %w", strings.TrimSpace(string(out)), err)
		}
		return nil
	}
	if _, err := exec.LookPath("udhcpc"); err == nil {
		if out, err := exec.Command("udhcpc", "-i", iface, "-n", "-q").CombinedOutput(); err != nil {
			return fmt.Errorf("udhcpc failed: %s: %w", strings.TrimSpace(string(out)), err)
		}
		return nil
	}
	return fmt.Errorf("no DHCP client found (tried dhclient, udhcpc) -- can't request a lease")
}

// persistBestEffort tries to make a confirmed static IP survive a reboot by detecting which
// higher-level config system is present and writing to it -- unlike the live apply/revert above,
// getting this wrong only risks the NEXT boot reverting to the old config (recoverable by re-
// applying from this UI), not an active lockout, so a best-effort guess here is an acceptable
// trade the live mechanism above deliberately avoided.
func persistBestEffort(pending *PendingIPChange) {
	if pending.NewMode != "static" {
		return // DHCP needs no persistence -- it's every backend's default behavior already
	}
	iface := pending.Snapshot.Interface

	switch {
	case fileExists("/etc/network/interfaces"):
		block := fmt.Sprintf("\n# Written by FDT.Scout %s -- static IP confirmed %s\nauto %s\niface %s inet static\n    address %s\n",
			Version, time.Now().Format(time.RFC3339), iface, iface, pending.NewAddress)
		if pending.NewGateway != "" {
			block += fmt.Sprintf("    gateway %s\n", pending.NewGateway)
		}
		appendToFile("/etc/network/interfaces", block)
	case dirExists("/etc/netplan"):
		// Not written: netplan's YAML is indentation-sensitive and this device's existing file
		// (if any) isn't something to blindly overwrite. Left as a manual step deliberately.
	case fileExists("/etc/systemd/network"):
		// Same reasoning -- not written automatically.
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
func appendToFile(path, content string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(content)
}
