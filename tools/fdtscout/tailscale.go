package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Tailscale is already installable via the existing generic Install tab (installcatalog.go) and
// already gets basic systemd-unit status on the Apps tab (apps.go's tailscaled.service entry) --
// this file is specifically the piece that was missing: actually JOINING a tailnet from the GUI,
// which previously required dropping to the terminal and running `tailscale up` by hand (the
// Tailscale Extra's own description in CloudKeyWizard says exactly that). Verified the real CLI
// flags and status JSON schema against tailscale's own source
// (cmd/tailscale/cli/up.go, ipn/ipnstate/ipnstate.go) rather than guessing -- notably the real flag
// is `--auth-key`, not the more commonly assumed `--authkey`.

type TailscaleStatus struct {
	Installed    bool   `json:"installed"`
	LoggedIn     bool   `json:"loggedIn"`
	BackendState string `json:"backendState,omitempty"`
	TailscaleIP  string `json:"tailscaleIp,omitempty"`
	DNSName      string `json:"dnsName,omitempty"`
	TailnetName  string `json:"tailnetName,omitempty"`
}

func tailscaleInstalled() bool {
	return exec.Command("sh", "-c", "command -v tailscale").Run() == nil
}

// tailscaleStatusJSON mirrors tailscale's own ipnstate.Status -- only the fields this app actually
// uses, named to match its real (untagged, so Go's default exported-field-name) JSON output exactly.
type tailscaleStatusJSON struct {
	BackendState string `json:"BackendState"`
	Self         struct {
		DNSName      string   `json:"DNSName"` // FQDN with a trailing dot, e.g. "scout001.tailxxxx.ts.net."
		TailscaleIPs []string `json:"TailscaleIPs"`
	} `json:"Self"`
	CurrentTailnet *struct {
		Name string `json:"Name"`
	} `json:"CurrentTailnet"`
}

func getTailscaleStatus() TailscaleStatus {
	status := TailscaleStatus{Installed: tailscaleInstalled()}
	if !status.Installed {
		return status
	}
	out, err := exec.Command("tailscale", "status", "--json").Output()
	if err != nil {
		return status // installed but daemon not responding -- report what we know rather than fail
	}
	var parsed tailscaleStatusJSON
	if json.Unmarshal(out, &parsed) != nil {
		return status
	}
	status.BackendState = parsed.BackendState
	status.LoggedIn = parsed.BackendState == "Running"
	status.DNSName = strings.TrimSuffix(parsed.Self.DNSName, ".")
	if len(parsed.Self.TailscaleIPs) > 0 {
		status.TailscaleIP = parsed.Self.TailscaleIPs[0]
	}
	if parsed.CurrentTailnet != nil {
		status.TailnetName = parsed.CurrentTailnet.Name
	}
	return status
}

// tailscaleUpWithAuthKey joins a tailnet non-interactively using a pre-generated auth key (from
// Tailscale's own admin console -- login.tailscale.com/admin/settings/keys -- or a self-hosted
// Headscale server's equivalent). Completes synchronously; no browser step needed, which is why
// this is the recommended path in the UI over the interactive one below.
func tailscaleUpWithAuthKey(authKey, loginServer, hostname string) (string, error) {
	if !tailscaleInstalled() {
		return "", fmt.Errorf("tailscale isn't installed -- install it from the Apps tab first")
	}
	args := []string{"up", "--auth-key=" + authKey, "--accept-dns=true"}
	if loginServer != "" {
		args = append(args, "--login-server="+loginServer)
	}
	if hostname != "" {
		args = append(args, "--hostname="+hostname)
	}
	out, err := exec.Command("tailscale", args...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}
	return string(out), nil
}

// upOutputJSON mirrors tailscale's own upOutputJSON (cmd/tailscale/cli/up.go) -- what `tailscale up
// --json` prints (MarshalIndent'd, so it spans several lines) the moment it has something to report.
type upOutputJSON struct {
	AuthURL      string `json:"AuthURL"`
	QR           string `json:"QR"` // ready-to-display "data:image/png;base64,..." data URI
	BackendState string `json:"BackendState"`
	Error        string `json:"Error"`
}

// tailscaleUpInteractive starts the real browser-based login flow -- the normal way most people
// join a tailnet, no auth key needed. `tailscale up --json` prints one JSON object to stdout the
// moment it has a login URL, then keeps running in the background waiting for the user to actually
// complete that login in their own browser -- deliberately NOT waited on here (same pattern as
// device-code/OAuth flows generally: hand back the URL immediately, let the process keep running).
// Callers should poll getTailscaleStatus() to see BackendState flip to "Running" once the user
// finishes in their browser.
func tailscaleUpInteractive(loginServer, hostname string) (authURL, qrDataURI string, err error) {
	if !tailscaleInstalled() {
		return "", "", fmt.Errorf("tailscale isn't installed -- install it from the Apps tab first")
	}
	args := []string{"up", "--json", "--accept-dns=true"}
	if loginServer != "" {
		args = append(args, "--login-server="+loginServer)
	}
	if hostname != "" {
		args = append(args, "--hostname="+hostname)
	}
	cmd := exec.Command("tailscale", args...)
	stdout, pipeErr := cmd.StdoutPipe()
	if pipeErr != nil {
		return "", "", pipeErr
	}
	if startErr := cmd.Start(); startErr != nil {
		return "", "", startErr
	}
	go func() { _ = cmd.Wait() }() // reap it eventually -- the actual login continues in the background

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // the QR data URI line runs a few KB
	deadline := time.Now().Add(15 * time.Second)
	var buf strings.Builder
	collecting := false
	for scanner.Scan() {
		if time.Now().After(deadline) {
			break
		}
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if !collecting {
			if trimmed != "{" {
				continue
			}
			collecting = true
		}
		buf.WriteString(line)
		buf.WriteString("\n")
		if trimmed != "}" {
			continue
		}
		var parsed upOutputJSON
		if json.Unmarshal([]byte(buf.String()), &parsed) == nil {
			if parsed.Error != "" {
				return "", "", fmt.Errorf("%s", parsed.Error)
			}
			if parsed.AuthURL != "" {
				return parsed.AuthURL, parsed.QR, nil
			}
			if parsed.BackendState == "Running" {
				return "", "", nil // already logged in -- nothing to show, not an error
			}
		}
		collecting = false
		buf.Reset()
	}
	return "", "", fmt.Errorf("didn't get a login URL from tailscale within 15s")
}

// tailscaleLogout forgets this device's tailnet identity -- a subsequent `up` would need to
// re-authenticate from scratch. The cleaner "leave the tailnet" action for a UI button than `down`
// (which just disconnects but keeps the identity for a quick reconnect later).
func tailscaleLogout() error {
	out, err := exec.Command("tailscale", "logout").CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}
