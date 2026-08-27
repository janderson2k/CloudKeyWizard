package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// NTP and DNS settings, consolidated under the Settings tab alongside hostname and static IP/DHCP
// (moved out of Front Panel and Health respectively -- system identity/network settings belong
// together, not scattered). Lower risk than the static IP work on purpose: a bad NTP server just
// means clock drift, and a bad DNS server breaks the DEVICE's own outbound lookups but doesn't
// affect reaching this console over its own IP (that's the browser's own DNS, not the device's) --
// user-accepted risk level, so neither gets the elaborate apply-then-revert mechanism static IP
// has. Both still detect the actual mechanism in use rather than assuming one, same discipline as
// the static IP work.

// --- NTP --------------------------------------------------------------

type NTPStatus struct {
	Available    bool     `json:"available"`
	Enabled      bool     `json:"enabled"`
	Synchronized bool     `json:"synchronized"`
	Servers      []string `json:"servers"`
	Detail       string   `json:"detail,omitempty"`
}

func ntpAvailable() bool {
	return exec.Command("systemctl", "list-unit-files", "systemd-timesyncd.service").Run() == nil
}

func getNTPStatus() NTPStatus {
	if !ntpAvailable() {
		return NTPStatus{Available: false, Detail: "systemd-timesyncd isn't installed on this device -- NTP may be managed by something else (chrony, ntpd) not handled here."}
	}
	enabledOut, _ := exec.Command("timedatectl", "show", "-p", "NTP", "--value").Output()
	syncOut, _ := exec.Command("timedatectl", "show", "-p", "NTPSynchronized", "--value").Output()
	return NTPStatus{
		Available:    true,
		Enabled:      strings.TrimSpace(string(enabledOut)) == "yes",
		Synchronized: strings.TrimSpace(string(syncOut)) == "yes",
		Servers:      readTimesyncdServers(),
	}
}

func readTimesyncdServers() []string {
	f, err := os.Open("/etc/systemd/timesyncd.conf")
	if err != nil {
		return nil
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if after, ok := strings.CutPrefix(line, "NTP="); ok {
			return strings.Fields(after)
		}
	}
	return nil
}

func setNTPServers(servers []string) error {
	if !ntpAvailable() {
		return fmt.Errorf("systemd-timesyncd isn't installed on this device -- can't set NTP servers this way")
	}
	content := "[Time]\nNTP=" + strings.Join(servers, " ") + "\n"
	if err := os.WriteFile("/etc/systemd/timesyncd.conf", []byte(content), 0644); err != nil {
		return err
	}
	if out, err := exec.Command("systemctl", "restart", "systemd-timesyncd").CombinedOutput(); err != nil {
		return fmt.Errorf("saved, but restarting systemd-timesyncd failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func setNTPEnabled(enabled bool) error {
	if !ntpAvailable() {
		return fmt.Errorf("systemd-timesyncd isn't installed on this device")
	}
	val := "false"
	if enabled {
		val = "true"
	}
	if out, err := exec.Command("timedatectl", "set-ntp", val).CombinedOutput(); err != nil {
		return fmt.Errorf("timedatectl set-ntp failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// --- DNS --------------------------------------------------------------

type DNSStatus struct {
	ManagedBy string   `json:"managedBy"`
	Servers   []string `json:"servers"`
}

func systemdResolvedActive() bool {
	return exec.Command("systemctl", "is-active", "--quiet", "systemd-resolved").Run() == nil
}

func getDNSStatus() DNSStatus {
	if systemdResolvedActive() {
		iface, _, _ := currentNetworkState()
		var servers []string
		if iface != "" {
			out, _ := exec.Command("resolvectl", "dns", iface).CombinedOutput()
			servers = parseResolvectlDNS(string(out))
		}
		return DNSStatus{ManagedBy: "systemd-resolved", Servers: servers}
	}
	return DNSStatus{ManagedBy: "/etc/resolv.conf", Servers: parseResolvConf()}
}

func setDNSServers(servers []string) error {
	if systemdResolvedActive() {
		iface, _, _ := currentNetworkState()
		if iface == "" {
			return fmt.Errorf("couldn't identify the primary network interface")
		}
		args := append([]string{"dns", iface}, servers...)
		out, err := exec.Command("resolvectl", args...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("resolvectl failed: %s: %w", strings.TrimSpace(string(out)), err)
		}
		return nil
	}
	var sb strings.Builder
	for _, s := range servers {
		sb.WriteString("nameserver " + s + "\n")
	}
	return os.WriteFile("/etc/resolv.conf", []byte(sb.String()), 0644)
}

func parseResolvectlDNS(out string) []string {
	// Lines look like "Link 2 (eth0): 1.1.1.1 8.8.8.8" -- take everything after the last ":".
	var servers []string
	for _, line := range strings.Split(out, "\n") {
		idx := strings.LastIndex(line, ":")
		if idx == -1 {
			continue
		}
		servers = append(servers, strings.Fields(line[idx+1:])...)
	}
	return servers
}

func parseResolvConf() []string {
	f, err := os.Open("/etc/resolv.conf")
	if err != nil {
		return nil
	}
	defer f.Close()
	var servers []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && fields[0] == "nameserver" {
			servers = append(servers, fields[1])
		}
	}
	return servers
}

// --- Timezone -----------------------------------------------------------
// Unlike CloudKeyWizard.exe's own timezone Extra (a curated ~50-entry list, since that Windows app
// has no natural moment to SSH in and query live before the user has even connected), FDT.Scout
// already runs as root ON the device -- so this queries the device's own real tzdata via
// `timedatectl list-timezones` instead of guessing at a fixed list, same "use the real mechanism"
// discipline as DNS/NTP detection above.

type TimezoneStatus struct {
	Current string   `json:"current"`
	Zones   []string `json:"zones"`
}

func listTimezones() []string {
	out, err := exec.Command("timedatectl", "list-timezones").Output()
	if err != nil {
		return nil
	}
	var zones []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			zones = append(zones, line)
		}
	}
	return zones
}

func getTimezoneStatus() TimezoneStatus {
	out, _ := exec.Command("timedatectl", "show", "-p", "Timezone", "--value").Output()
	return TimezoneStatus{Current: strings.TrimSpace(string(out)), Zones: listTimezones()}
}

func setTimezone(tz string) error {
	if tz == "" {
		return fmt.Errorf("timezone is required")
	}
	// Validate against the device's own real list before ever shelling out with it -- rejects a
	// typo or garbage client-supplied string instead of trusting it blindly (unlike DNS/NTP
	// servers, which have no fixed enum to validate against, a timezone does).
	valid := false
	for _, z := range listTimezones() {
		if z == tz {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("unrecognized timezone %q", tz)
	}
	if out, err := exec.Command("timedatectl", "set-timezone", tz).CombinedOutput(); err != nil {
		return fmt.Errorf("timedatectl set-timezone failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}
