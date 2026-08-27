package main

import (
	"os/exec"
	"strings"
)

// This is a fixed, hand-verified catalog -- every unit/container name here was checked against
// what CloudKey Wizard's own Optional Extras actually install (Services/ExtraCatalog.cs and the
// bundled scripts under Scripts/runbook/), not guessed. Deliberately NOT a general "run arbitrary
// systemctl commands from the web" endpoint: actions are only ever applied to a name from this
// fixed list, never a client-supplied unit string, since that would turn this into an unbounded
// remote-command primitive on top of an already root-capable console.
//
// Scope: shows status for anything CloudKey Wizard can install as an ongoing service, and offers
// start/stop (+ enable/disable for systemd-native ones) for whatever's already installed but not
// running. Does NOT install anything new -- for something not installed yet, use CloudKey Wizard
// (the Windows app) itself. Reinstalling all of that install logic a second time here, in Go,
// would mean maintaining two divergent copies of the same scripts forever.
type appKind string

const (
	kindSystemd appKind = "systemd"
	kindDocker  appKind = "docker"
	kindPattern appKind = "pattern" // dynamically-named units, e.g. AutoSSH's per-relay units
)

type appDef struct {
	ID     string
	Name   string
	Kind   appKind
	Unit   string // systemd unit name, or docker container name, or a systemctl glob for kindPattern
	Detail string
}

var appCatalog = []appDef{
	{ID: "fdtscout", Name: "FDT.Scout (this console)", Kind: kindSystemd, Unit: "fdtscout.service", Detail: "Always running -- you're viewing it live."},
	{ID: "fail2ban", Name: "fail2ban", Kind: kindSystemd, Unit: "fail2ban.service"},
	{ID: "unattended-upgrades", Name: "Automatic security updates", Kind: kindSystemd, Unit: "unattended-upgrades.service"},
	{ID: "plex", Name: "Plex Media Server", Kind: kindSystemd, Unit: "plexmediaserver.service"},
	{ID: "nzbget", Name: "NZBGet", Kind: kindSystemd, Unit: "nzbget.service"},
	{ID: "sonarr", Name: "Sonarr", Kind: kindSystemd, Unit: "sonarr.service"},
	{ID: "radarr", Name: "Radarr", Kind: kindSystemd, Unit: "radarr.service"},
	{ID: "prowlarr", Name: "Prowlarr", Kind: kindSystemd, Unit: "prowlarr.service"},
	{ID: "home-assistant", Name: "Home Assistant", Kind: kindDocker, Unit: "homeassistant"},
	{ID: "tailscale", Name: "Tailscale client", Kind: kindSystemd, Unit: "tailscaled.service"},
	{ID: "wireguard-egress", Name: "WireGuard fail-closed egress", Kind: kindSystemd, Unit: "wg-quick-vpn.service", Detail: "Also installs netns-vpn.service and vpn-heal.timer -- toggling this unit is the meaningful on/off switch for the tunnel itself."},
	{ID: "autossh", Name: "AutoSSH rescue tunnel(s)", Kind: kindPattern, Unit: "autossh-tunnel-*.service", Detail: "Named per relay -- start/stop/enable/disable a specific one from the terminal (systemctl ... autossh-tunnel-<name>.service) since there can be more than one."},
}

type AppStatus struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Installed  bool   `json:"installed"`
	Active     bool   `json:"active"`
	Enabled    bool   `json:"enabled"`
	Detail     string `json:"detail"`
	Toggleable bool   `json:"toggleable"`
	// Count/Names is only populated for kindPattern (AutoSSH) -- there can be more than one unit.
	Count int      `json:"count,omitempty"`
	Names []string `json:"names,omitempty"`
}

func getAppStatuses() []AppStatus {
	out := make([]AppStatus, 0, len(appCatalog))
	for _, def := range appCatalog {
		switch def.Kind {
		case kindSystemd:
			out = append(out, statusForSystemdUnit(def))
		case kindDocker:
			out = append(out, statusForDockerContainer(def))
		case kindPattern:
			out = append(out, statusForPattern(def))
		}
	}
	return out
}

func statusForSystemdUnit(def appDef) AppStatus {
	installed := exec.Command("systemctl", "list-unit-files", def.Unit).Run() == nil
	active := installed && exec.Command("systemctl", "is-active", "--quiet", def.Unit).Run() == nil
	enabled := installed && exec.Command("systemctl", "is-enabled", "--quiet", def.Unit).Run() == nil
	detail := def.Detail
	if !installed {
		detail = "Not installed -- install this from CloudKey Wizard's Optional Extras."
	}
	return AppStatus{
		ID: def.ID, Name: def.Name, Kind: string(def.Kind),
		Installed: installed, Active: active, Enabled: enabled,
		Detail: detail, Toggleable: installed,
	}
}

func statusForDockerContainer(def appDef) AppStatus {
	if exec.Command("sh", "-c", "command -v docker").Run() != nil {
		return AppStatus{ID: def.ID, Name: def.Name, Kind: string(def.Kind), Detail: "Docker isn't installed -- install this from CloudKey Wizard's Optional Extras."}
	}
	out, err := exec.Command("docker", "inspect", "--format", "{{.State.Running}}", def.Unit).CombinedOutput()
	installed := err == nil
	active := installed && strings.TrimSpace(string(out)) == "true"
	detail := def.Detail
	if !installed {
		detail = "Not installed -- install this from CloudKey Wizard's Optional Extras."
	}
	return AppStatus{
		ID: def.ID, Name: def.Name, Kind: string(def.Kind),
		Installed: installed, Active: active, Enabled: installed, // docker --restart=unless-stopped is this container's "enabled" equivalent, baked in at install time
		Detail: detail, Toggleable: installed,
	}
}

func statusForPattern(def appDef) AppStatus {
	out, _ := exec.Command("systemctl", "list-units", "--type=service", "--no-legend", "--all", def.Unit).CombinedOutput()
	var names []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		names = append(names, strings.Fields(line)[0])
	}
	installed := len(names) > 0
	active := false
	for _, n := range names {
		if exec.Command("systemctl", "is-active", "--quiet", n).Run() == nil {
			active = true
			break
		}
	}
	detail := def.Detail
	if !installed {
		detail = "Not installed -- install this from CloudKey Wizard's Optional Extras."
	}
	return AppStatus{
		ID: def.ID, Name: def.Name, Kind: string(def.Kind),
		Installed: installed, Active: active, Enabled: active,
		Detail: detail, Toggleable: false, Count: len(names), Names: names,
	}
}

// applyAppAction runs one of start/stop/enable/disable against a catalog entry's real unit name --
// never a client-supplied string. Docker-kind apps only support start/stop (no separate
// enable/disable concept; --restart=unless-stopped was set at install time).
func applyAppAction(id, action string) (AppStatus, error) {
	var def *appDef
	for i := range appCatalog {
		if appCatalog[i].ID == id {
			def = &appCatalog[i]
			break
		}
	}
	if def == nil {
		return AppStatus{}, errNotFound(id)
	}

	switch def.Kind {
	case kindSystemd:
		switch action {
		case "start", "stop", "enable", "disable", "restart":
			if out, err := exec.Command("systemctl", action, def.Unit).CombinedOutput(); err != nil {
				return AppStatus{}, errAction(action, def.Unit, out, err)
			}
		default:
			return AppStatus{}, errBadAction(action)
		}
		return statusForSystemdUnit(*def), nil
	case kindDocker:
		switch action {
		case "start", "stop", "restart":
			if out, err := exec.Command("docker", action, def.Unit).CombinedOutput(); err != nil {
				return AppStatus{}, errAction(action, def.Unit, out, err)
			}
		default:
			return AppStatus{}, errBadAction(action)
		}
		return statusForDockerContainer(*def), nil
	default:
		return AppStatus{}, errNotToggleable(id)
	}
}
