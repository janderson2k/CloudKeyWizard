package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
)

// The front panel's title is the system hostname -- confirmed by reading jnovack/cloudkey's own
// README, and true of this app's own framebuffer drawing too (see framebuffer.go/lcddisplay.go).
// So "change the text on the front panel" is implemented honestly as "change the hostname," not a
// fictional free-text field the hardware doesn't support.
//
// FDT.Scout draws to /dev/fb0 directly now (see framebuffer.go) -- jnovack's own cloudkey.service
// is no longer part of this flow at all. This file still checks for it, because a device set up
// before this change may still have it installed and running, which could conflict with this
// binary's own framebuffer access (two processes opening the same fb device is not universally
// safe across every driver) -- if it's found, it gets stopped and disabled rather than restarted.

// validHostname is deliberately conservative (RFC 1123 label rules) -- a bad hostname can break
// more than the LCD (some services, DHCP, mDNS all read it), so this rejects anything that isn't
// obviously safe rather than trying to be permissive.
var validHostname = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)

type LCDStatus struct {
	Hostname string `json:"hostname"`
	// OwnDisplayOpen is true when THIS binary's own framebuffer drawing (framebuffer.go) is
	// currently active -- the real signal for "is FDT.Scout actually driving the panel," distinct
	// from the legacy jnovack fields below.
	OwnDisplayOpen bool `json:"ownDisplayOpen"`
	// LegacyCloudkeyInstalled/Active: jnovack/cloudkey's own LCD app, from before this app started
	// drawing to the framebuffer directly. No longer installed by this app's wizard (that step was
	// removed), but a device converted before this change may still have it running.
	LegacyCloudkeyInstalled bool `json:"legacyCloudkeyInstalled"`
	LegacyCloudkeyActive    bool `json:"legacyCloudkeyActive"`
	// CkUiActive flags UniFi's ORIGINAL LCD app still being active -- if it is, it's very likely
	// what's actually holding /dev/fb0 (the framebuffer), which would explain "hostname changed,
	// but the panel still shows the UniFi logo": two processes can't both own the same framebuffer
	// device cleanly, and whichever grabbed it first (or last, depending on the driver) wins the
	// physical screen regardless of what the *other* one is doing.
	CkUiInstalled bool `json:"ckUiInstalled"`
	CkUiActive    bool `json:"ckUiActive"`
	// RecentLog is the tail of `journalctl -u cloudkey` -- surfaced directly in the UI so
	// diagnosing "why doesn't the panel match" doesn't require dropping to a separate shell.
	RecentLog string `json:"recentLog"`
	// Warnings is non-fatal diagnostic notes from the most recent setHostname call (e.g. ck-ui
	// still holding the framebuffer) -- empty on a plain status GET, only populated right after a
	// POST that found something worth flagging. The hostname change itself can still have
	// succeeded even when this is non-empty.
	Warnings []string `json:"warnings,omitempty"`
	// ApparmorBlocking is real, confirmed diagnosis (2026-08-23, from an actual device's
	// journalctl output) for "the panel only ever shows the UniFi logo": cloudkey.service's own
	// systemd unit runs it as root with NO device-cgroup restrictions (confirmed by reading the
	// unit file itself), yet it still gets EPERM opening /dev/fb0 and /dev/input/event1. Root
	// being refused an operation that plain file permissions would allow is the signature of a
	// mandatory access control layer (AppArmor/SELinux) blocking it above and beyond normal
	// permissions -- not a permissions bug, and not (as first guessed, before this log existed)
	// ck-ui still holding the device. This checks the kernel log for an actual AppArmor DENIED
	// entry naming fb0/event/cloudkey to confirm or rule this out with real evidence, not a guess.
	ApparmorBlocking   bool   `json:"apparmorBlocking"`
	ApparmorProfile    string `json:"apparmorProfile,omitempty"`
	ApparmorDenialLine string `json:"apparmorDenialLine,omitempty"`
}

func getLCDStatus() LCDStatus {
	hostname, _ := os.Hostname()
	legacyInstalled := exec.Command("systemctl", "list-unit-files", "cloudkey.service").Run() == nil
	legacyActive := exec.Command("systemctl", "is-active", "--quiet", "cloudkey.service").Run() == nil
	ckUiInstalled := exec.Command("systemctl", "list-unit-files", "ck-ui.service").Run() == nil
	ckUiActive := exec.Command("systemctl", "is-active", "--quiet", "ck-ui.service").Run() == nil

	var recentLog string
	if out, err := exec.Command("journalctl", "-u", "cloudkey", "-n", "15", "--no-pager").CombinedOutput(); err == nil {
		recentLog = strings.TrimSpace(string(out))
	}

	blocking, profile, denialLine := checkApparmorDenial()

	lcdManager.mu.Lock()
	ownDisplayOpen := lcdManager.fb != nil
	lcdManager.mu.Unlock()

	return LCDStatus{
		Hostname:                strings.TrimSpace(hostname),
		OwnDisplayOpen:          ownDisplayOpen,
		LegacyCloudkeyInstalled: legacyInstalled,
		LegacyCloudkeyActive:    legacyActive,
		CkUiInstalled:           ckUiInstalled,
		CkUiActive:              ckUiActive,
		RecentLog:               recentLog,
		ApparmorBlocking:        blocking,
		ApparmorProfile:         profile,
		ApparmorDenialLine:      denialLine,
	}
}

// checkApparmorDenial looks for a real, specific AppArmor DENIED entry naming the framebuffer,
// the reset-button input device, or the cloudkey binary itself -- confirming or ruling out the
// mandatory-access-control hypothesis with actual kernel log evidence rather than a guess.
// Checked in two places since either can hold the relevant entry depending on this device's
// logging config: `journalctl -k` (the kernel ring buffer via systemd, usually accessible even
// where dmesg itself is locked down) first, falling back to `dmesg` directly.
func checkApparmorDenial() (blocking bool, profile string, denialLine string) {
	sources := [][]string{
		{"journalctl", "-k", "--no-pager", "-n", "500"},
		{"dmesg"},
	}
	for _, cmd := range sources {
		out, err := exec.Command(cmd[0], cmd[1:]...).CombinedOutput()
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(out), "\n") {
			lower := strings.ToLower(line)
			if !strings.Contains(lower, "apparmor") || !strings.Contains(lower, "denied") {
				continue
			}
			if !strings.Contains(lower, "fb0") && !strings.Contains(lower, "event1") && !strings.Contains(lower, "cloudkey") && !strings.Contains(lower, "fdtscout") {
				continue
			}
			return true, extractApparmorProfile(line), strings.TrimSpace(line)
		}
	}
	return false, "", ""
}

var apparmorProfileRe = regexp.MustCompile(`profile="([^"]+)"`)

func extractApparmorProfile(line string) string {
	m := apparmorProfileRe.FindStringSubmatch(line)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// fixApparmorProfile sets a specific AppArmor profile to "complain" mode (logs violations but
// allows the action) rather than disabling AppArmor system-wide -- a scoped, reversible
// remediation for exactly the one profile confirmed to be blocking the display, not a blanket
// security regression. Requires apparmor-utils' aa-complain, which may not be installed on this
// minimal image; reports plainly if it isn't rather than trying a riskier workaround.
//
// After a successful complain-mode switch, retries opening THIS binary's own framebuffer (the
// primary display path now -- see framebuffer.go) rather than restarting jnovack's legacy
// cloudkey.service, which is no longer what's expected to be drawing here.
func fixApparmorProfile(profile string) error {
	if profile == "" {
		return fmt.Errorf("no AppArmor profile identified -- nothing to fix")
	}
	if _, err := exec.LookPath("aa-complain"); err != nil {
		return fmt.Errorf("aa-complain isn't installed on this device (part of the apparmor-utils package) -- can't apply this automatically; install apparmor-utils or adjust the profile manually")
	}
	out, err := exec.Command("aa-complain", profile).CombinedOutput()
	if err != nil {
		return fmt.Errorf("aa-complain failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	startLCDDisplay(lcdManager.metrics)
	return nil
}

// setHostname sets the new hostname three ways, deliberately redundant: `hostnamectl` (the normal
// systemd-native path), a direct sethostname(2) syscall, and a direct /etc/hostname write. This is
// defense in depth against `hostnamectl` silently degrading to "wrote the file, didn't touch the
// live kernel hostname" when systemd-hostnamed isn't fully present -- plausible on this hardware's
// heavily stripped-down UniFi OS image, and NOT something confirmed one way or the other without
// live access to the actual device (flagged in QUE.MD). Doing all three costs nothing on a normal
// system and closes the gap if hostnamectl alone doesn't work here.
func setHostname(newHostname string) (LCDStatus, error) {
	newHostname = strings.TrimSpace(newHostname)
	if !validHostname.MatchString(newHostname) {
		return LCDStatus{}, fmt.Errorf("'%s' isn't a valid hostname -- letters, digits, and hyphens only, can't start/end with a hyphen, max 63 characters", newHostname)
	}

	var warnings []string

	if out, err := exec.Command("hostnamectl", "set-hostname", newHostname).CombinedOutput(); err != nil {
		warnings = append(warnings, fmt.Sprintf("hostnamectl failed (%s), fell back to direct methods", strings.TrimSpace(string(out))))
	}

	if err := syscall.Sethostname([]byte(newHostname)); err != nil {
		warnings = append(warnings, fmt.Sprintf("direct sethostname(2) call also failed: %v", err))
	}

	if err := os.WriteFile("/etc/hostname", []byte(newHostname+"\n"), 0644); err != nil {
		warnings = append(warnings, fmt.Sprintf("writing /etc/hostname also failed: %v", err))
	}

	status := getLCDStatus()
	if status.Hostname != newHostname {
		warnings = append(warnings, fmt.Sprintf("kernel hostname still reports '%s' after all three attempts -- something on this device is overriding it", status.Hostname))
	}

	if status.CkUiInstalled && status.CkUiActive {
		warnings = append(warnings, "UniFi's original LCD app (ck-ui.service) is still active -- it very likely still owns the framebuffer, which would explain the panel not updating regardless of hostname. This app's own purge step is supposed to stop/disable it; if it's back, something re-enabled it.")
	}

	// Legacy jnovack cloudkey.service predates this app's own framebuffer drawing -- a device
	// converted before that change may still have it installed and running, which can conflict
	// with THIS binary also trying to own /dev/fb0. Stopped and disabled rather than restarted,
	// since it's superseded, not restarted alongside.
	if status.LegacyCloudkeyInstalled && status.LegacyCloudkeyActive {
		if out, err := exec.Command("systemctl", "disable", "--now", "cloudkey.service").CombinedOutput(); err != nil {
			warnings = append(warnings, fmt.Sprintf("found jnovack's legacy cloudkey.service still running (from before this app drew to the display directly) and tried to stop it, but that failed: %s -- it may still be competing for the framebuffer", strings.TrimSpace(string(out))))
		} else {
			warnings = append(warnings, "found and stopped jnovack's legacy cloudkey.service (from before this app started drawing to the display directly) -- it was superseded, not needed alongside this app's own display code.")
		}
	}

	if status.ApparmorBlocking {
		warnings = append(warnings, fmt.Sprintf("AppArmor is blocking something from opening the display (profile: %s) -- confirmed via the kernel log, not a guess. See the Front Panel tab for the exact denial and a one-click attempt to fix it.", status.ApparmorProfile))
	} else if !status.OwnDisplayOpen {
		warnings = append(warnings, "This app's own framebuffer display isn't currently open -- check the recent log below or the server's own startup log for why.")
	}

	refreshLCD()

	final := getLCDStatus()
	final.Warnings = warnings
	return final, nil
}
