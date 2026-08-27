package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// The two-way command channel: FDT.Scout polls Pushbullet for pushes addressed to its own
// callsign and replies. Designed over 3 rounds of discussion with the user -- see QUE.MD for the
// full history. Two command classes:
//   - Queries (status/disk/ip/uptime/apps/health/ping) -- read-only, answer immediately.
//   - Actions (restart/start/stop/enable/disable <app>) -- bounded to the SAME fixed catalog
//     apps.go's own start/stop/enable/disable already enforces (never a free-form command), and
//     gated behind a confirm step where BOTH messages must carry the callsign -- a bare "CONFIRM"
//     does nothing, closing the gap the user specifically caught during design.
const broadcastCallsign = "ALL"

type pendingAction struct {
	Verb        string
	AppID       string
	AppName     string
	RequestedAt time.Time
}

var (
	pendingMu sync.Mutex
	pending   *pendingAction
)

const pendingActionTTL = 5 * time.Minute

// sentPushIdens tracks the Pushbullet "iden" of every push FDT.Scout has sent itself (both alerts
// and command replies), so the poller can recognize and skip its OWN pushes on the next cycle
// rather than re-reading them as new incoming commands. Real bug found live: a reply's title is
// this device's own callsign (see handleIncomingPush's send below), which means the reply itself
// satisfies the callsign-prefix check the next time it's polled -- without this guard, FDT.Scout
// would try to parse its own reply text as a new command, typically hit "unrecognized," reply to
// THAT, and loop. Entries expire after 10 minutes (well past the ~20s poll interval) so this never
// grows unbounded.
var (
	sentMu    sync.Mutex
	sentIdens = map[string]time.Time{}
)

func markPushAsOurs(iden string) {
	if iden == "" {
		return
	}
	sentMu.Lock()
	defer sentMu.Unlock()
	sentIdens[iden] = time.Now()
	for id, t := range sentIdens {
		if time.Since(t) > 10*time.Minute {
			delete(sentIdens, id)
		}
	}
}

func wasSentByUs(iden string) bool {
	sentMu.Lock()
	defer sentMu.Unlock()
	_, ok := sentIdens[iden]
	return ok
}

type pbPush struct {
	Iden      string  `json:"iden"`
	Title     string  `json:"title"`
	Body      string  `json:"body"`
	Modified  float64 `json:"modified"`
	Type      string  `json:"type"`
	Dismissed bool    `json:"dismissed"`
}

type pbPushesResponse struct {
	Pushes []pbPush `json:"pushes"`
}

// runPushbulletListener polls for new pushes every pollInterval until stop is closed. Does nothing
// at all (not even an HTTP call) unless Pushbullet is configured and enabled -- no accidental
// network calls without the user having actually set this up.
func runPushbulletListener(stop <-chan struct{}, metrics *MetricsCollector) {
	cursor := float64(time.Now().Unix())
	tick := time.NewTicker(20 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
			cfg := LoadPushbulletConfig()
			if !cfg.Enabled || cfg.Token == "" {
				continue
			}
			newCursor, err := pollOnce(cfg, cursor, metrics)
			if err != nil {
				fmt.Fprintf(os.Stderr, "pushbullet poll failed: %v\n", err)
				continue
			}
			if newCursor > cursor {
				cursor = newCursor
			}
		}
	}
}

func pollOnce(cfg PushbulletConfig, since float64, metrics *MetricsCollector) (float64, error) {
	url := fmt.Sprintf("https://api.pushbullet.com/v2/pushes?modified_after=%f&active=true", since)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return since, err
	}
	req.Header.Set("Access-Token", cfg.Token)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return since, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return since, fmt.Errorf("pushbullet returned HTTP %d", resp.StatusCode)
	}

	var parsed pbPushesResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return since, err
	}

	newest := since
	for _, p := range parsed.Pushes {
		if p.Modified > newest {
			newest = p.Modified
		}
		if p.Dismissed || p.Type != "note" || wasSentByUs(p.Iden) {
			continue
		}
		handleIncomingPush(cfg, strings.TrimSpace(p.Title+" "+p.Body), metrics)
	}
	return newest, nil
}

// handleIncomingPush is the callsign gate: anything not addressed to this device's own callsign
// (or the reserved broadcast callsign) is silently ignored, full stop -- including a bare CONFIRM,
// which must ALSO carry the callsign to do anything.
func handleIncomingPush(cfg PushbulletConfig, text string, metrics *MetricsCollector) {
	if text == "" {
		return
	}
	mine := callsignPrefix(cfg)
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return
	}
	prefix := strings.ToUpper(fields[0])
	if prefix != mine && prefix != broadcastCallsign {
		return
	}
	rest := strings.TrimSpace(strings.TrimPrefix(text, fields[0]))
	if rest == "" {
		return
	}
	reply := dispatchCommand(cfg, rest, metrics)
	if reply != "" {
		go func() {
			iden, err := sendPush(cfg, mine, reply)
			if err != nil {
				fmt.Fprintf(os.Stderr, "pushbullet reply failed: %v\n", err)
				return
			}
			markPushAsOurs(iden)
		}()
	}
}

func dispatchCommand(cfg PushbulletConfig, command string, metrics *MetricsCollector) string {
	fields := strings.Fields(command)
	verb := strings.ToLower(fields[0])
	args := fields[1:]

	switch verb {
	case "confirm":
		return handleConfirm()
	case "status":
		return cmdStatus(metrics)
	case "disk":
		return cmdDisk(metrics)
	case "ip":
		return cmdIP()
	case "uptime":
		return cmdUptime()
	case "apps":
		return cmdApps()
	case "health":
		return cmdHealth()
	case "help":
		return "Queries: status, disk, ip, uptime, apps, health, ping <host>. Actions: restart/start/stop/enable/disable <app>, then CONFIRM."
	case "ping":
		if len(args) == 0 {
			return "Usage: ping <host>"
		}
		return cmdPing(args[0])
	case "restart", "start", "stop", "enable", "disable":
		if len(args) == 0 {
			return "Usage: " + verb + " <app>"
		}
		return cmdRequestAction(verb, strings.Join(args, " "))
	default:
		// Deliberately silent, not an error reply: anything starting with this device's callsign
		// but not matching a known verb is ignored rather than answered. Two reasons -- first, the
		// user asked for it explicitly (unknown input should be ignored, not argued with); second,
		// it's a second layer of defense against ever looping on unexpected text, on top of the
		// wasSentByUs() check above that closes the specific self-reply loop this was found from.
		return ""
	}
}

func handleConfirm() string {
	pendingMu.Lock()
	p := pending
	pending = nil
	pendingMu.Unlock()

	if p == nil {
		return "No pending action to confirm."
	}
	if time.Since(p.RequestedAt) > pendingActionTTL {
		return "That confirmation window expired. Re-issue the command."
	}
	status, err := applyAppAction(p.AppID, p.Verb)
	if err != nil {
		return fmt.Sprintf("Failed to %s %s: %v", p.Verb, p.AppName, err)
	}
	return fmt.Sprintf("%s %s. Now: %s.", pastTense(p.Verb), p.AppName, activeWord(status.Active))
}

var verbPastTense = map[string]string{
	"restart": "Restarted", "start": "Started", "stop": "Stopped",
	"enable": "Enabled", "disable": "Disabled",
}

func pastTense(verb string) string {
	if s, ok := verbPastTense[verb]; ok {
		return s
	}
	return verb
}

func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func activeWord(active bool) string {
	if active {
		return "running"
	}
	return "stopped"
}

func cmdRequestAction(verb, target string) string {
	def := findAppByNameOrID(target)
	if def == nil {
		return fmt.Sprintf("No known app matches %q. Send \"help\" -- or check the Apps tab for exact names.", target)
	}
	pendingMu.Lock()
	pending = &pendingAction{Verb: verb, AppID: def.ID, AppName: def.Name, RequestedAt: time.Now()}
	pendingMu.Unlock()
	return fmt.Sprintf("%s %s? Reply \"<CALLSIGN> CONFIRM\" within 5 minutes.", capitalizeFirst(verb), def.Name)
}

func findAppByNameOrID(target string) *appDef {
	target = strings.ToLower(strings.TrimSpace(target))
	for i := range appCatalog {
		if strings.ToLower(appCatalog[i].ID) == target {
			return &appCatalog[i]
		}
	}
	for i := range appCatalog {
		if strings.Contains(strings.ToLower(appCatalog[i].Name), target) {
			return &appCatalog[i]
		}
	}
	return nil
}

// --- Query command implementations -------------------------------------------------------------

func cmdStatus(metrics *MetricsCollector) string {
	history := metrics.History()
	metricsLine := "no metrics yet"
	if len(history) > 0 {
		last := history[len(history)-1]
		metricsLine = fmt.Sprintf("CPU %.0f%%, mem %.0f%%, / %.0f%%", last.CPUPct, last.MemPct, last.RootPct)
	}
	running, total := 0, 0
	for _, app := range getAppStatuses() {
		if app.Installed {
			total++
			if app.Active {
				running++
			}
		}
	}
	return fmt.Sprintf("Up %s. %s. %d/%d apps running.", formatUptimeShort(readUptime()), metricsLine, running, total)
}

func cmdDisk(metrics *MetricsCollector) string {
	history := metrics.History()
	if len(history) == 0 {
		return "No disk data yet."
	}
	last := history[len(history)-1]
	if last.VolumePct >= 0 {
		return fmt.Sprintf("/ at %.0f%%, /volume at %.0f%%", last.RootPct, last.VolumePct)
	}
	return fmt.Sprintf("/ at %.0f%% (no /volume mounted)", last.RootPct)
}

func cmdIP() string {
	_, addresses, _ := currentNetworkState()
	if len(addresses) == 0 {
		return "Couldn't determine an IP address."
	}
	return strings.Join(addresses, ", ")
}

func cmdUptime() string {
	return "Up " + formatUptimeShort(readUptime())
}

func cmdApps() string {
	var lines []string
	for _, app := range getAppStatuses() {
		state := "not installed"
		if app.Installed {
			state = "stopped"
			if app.Active {
				state = "running"
			}
		}
		lines = append(lines, fmt.Sprintf("%s: %s", app.Name, state))
	}
	return strings.Join(lines, "\n")
}

func cmdHealth() string {
	specs := GetSystemSpecs()
	line := fmt.Sprintf("%s (%d cores), %.0fMB RAM, %.1fGB /", specs.CPUModel, specs.CPUCores, specs.MemTotalMB, specs.RootTotalGB)
	if specs.VolumeTotalGB > 0 {
		line += fmt.Sprintf(", %.1fGB /volume", specs.VolumeTotalGB)
	}
	return line
}

func cmdPing(host string) string {
	up, latency, detail := pingHost(host, 3)
	if !up {
		return fmt.Sprintf("%s: no response (%s)", host, detail)
	}
	return fmt.Sprintf("%s: up, %.0fms", host, latency)
}

func formatUptimeShort(seconds float64) string {
	d := time.Duration(seconds) * time.Second
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, mins)
	}
	return fmt.Sprintf("%dh %dm", hours, mins)
}
