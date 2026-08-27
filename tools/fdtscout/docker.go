package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Docker as a first-class FDT.Scout feature -- not just the implementation detail buried inside
// the Home Assistant Extra (apps.go's own kindDocker only knows about that one container by name).
// Trimmed scope, converged over several rounds with the user: install/verify (not a real on/off
// toggle -- something may already depend on Docker once it's installed), full lifecycle control on
// existing containers, a real create-from-image form, log viewing, and an explicit storage-location
// picker rather than a silent default -- all deliberately scoped OUT: compose stacks (fights the
// same RAM/storage constraints already established for this hardware), exec-into-container shells,
// image search, network/volume management UI, bulk operations. No image allow-list here, unlike
// Pushbullet's bounded actions -- this lives entirely inside the already-authenticated session,
// which already has full root terminal access, so pulling/running whatever image is requested is a
// nicer UI over something already possible by hand, not a new capability.

// --- Daemon status -------------------------------------------------------

type DockerStatus struct {
	Installed       bool   `json:"installed"`
	Running         bool   `json:"running"`
	Version         string `json:"version"`
	ContainerCount  int    `json:"containerCount"`
	StorageRoot     string `json:"storageRoot"`
	VolumeMounted   bool   `json:"volumeMounted"`
	StorageOnVolume bool   `json:"storageOnVolume"`
}

func dockerInstalled() bool {
	return exec.Command("sh", "-c", "command -v docker").Run() == nil
}

func getDockerStatus() DockerStatus {
	status := DockerStatus{Installed: dockerInstalled()}
	if !status.Installed {
		return status
	}
	status.Running = exec.Command("systemctl", "is-active", "--quiet", "docker").Run() == nil
	if out, err := exec.Command("docker", "version", "--format", "{{.Server.Version}}").Output(); err == nil {
		status.Version = strings.TrimSpace(string(out))
	}
	if out, err := exec.Command("docker", "ps", "-aq").Output(); err == nil {
		lines := strings.Fields(strings.TrimSpace(string(out)))
		status.ContainerCount = len(lines)
	}
	status.StorageRoot = currentDockerDataRoot()
	if _, err := os.Stat("/volume"); err == nil {
		status.VolumeMounted = true
	}
	status.StorageOnVolume = strings.HasPrefix(status.StorageRoot, "/volume")
	return status
}

// installDocker is idempotent and deliberately one-way -- "install/verify," not a real toggle.
// Something (Home Assistant, or a container the user runs from the new Docker tab) may already
// depend on Docker once it's installed, so there's no matching "uninstall" path offered here.
func installDocker() error {
	if !dockerInstalled() {
		if out, err := exec.Command("sh", "-c", "apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y docker.io").CombinedOutput(); err != nil {
			return fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
		}
	}
	if out, err := exec.Command("systemctl", "enable", "--now", "docker").CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// --- Storage location, an explicit user choice rather than a silent default -----------------

const dockerDaemonConfigFile = "/etc/docker/daemon.json"

// currentDockerDataRoot reads daemon.json's own data-root if set, otherwise reports Docker's real
// documented default -- never guessed.
func currentDockerDataRoot() string {
	data, err := os.ReadFile(dockerDaemonConfigFile)
	if err == nil {
		var cfg map[string]any
		if json.Unmarshal(data, &cfg) == nil {
			if root, ok := cfg["data-root"].(string); ok && root != "" {
				return root
			}
		}
	}
	return "/var/lib/docker"
}

// SetDockerStorageRoot moves Docker's own image/container storage to newRoot -- stops the daemon,
// copies the existing data tree (rsync -a, so a failure partway through doesn't lose the original
// until the copy is confirmed), updates daemon.json, and restarts. If the daemon fails to come back
// up on the new path, this reverts daemon.json to the previous root and restarts there instead,
// rather than leaving Docker down.
func SetDockerStorageRoot(newRoot string) error {
	if !dockerInstalled() {
		return fmt.Errorf("Docker isn't installed")
	}
	oldRoot := currentDockerDataRoot()
	if oldRoot == newRoot {
		return nil
	}
	if err := os.MkdirAll(newRoot, 0710); err != nil {
		return fmt.Errorf("couldn't create %s: %w", newRoot, err)
	}

	if out, err := exec.Command("systemctl", "stop", "docker", "docker.socket").CombinedOutput(); err != nil {
		return fmt.Errorf("couldn't stop docker to move its storage: %s: %w", strings.TrimSpace(string(out)), err)
	}

	if info, statErr := os.Stat(oldRoot); statErr == nil && info.IsDir() {
		if out, err := exec.Command("rsync", "-a", oldRoot+"/", newRoot+"/").CombinedOutput(); err != nil {
			exec.Command("systemctl", "start", "docker").Run() // best-effort: don't leave Docker down on a failed copy
			return fmt.Errorf("copying existing data failed, Docker restarted on its old path: %s: %w", strings.TrimSpace(string(out)), err)
		}
	}

	if err := writeDockerDataRoot(newRoot); err != nil {
		exec.Command("systemctl", "start", "docker").Run()
		return fmt.Errorf("couldn't update daemon.json, Docker restarted on its old path: %w", err)
	}

	if out, err := exec.Command("systemctl", "start", "docker").CombinedOutput(); err != nil || exec.Command("systemctl", "is-active", "--quiet", "docker").Run() != nil {
		// Revert -- the new path didn't work, don't leave the daemon down or pointed somewhere broken.
		writeDockerDataRoot(oldRoot)
		exec.Command("systemctl", "start", "docker").Run()
		return fmt.Errorf("docker failed to start on the new path (reverted to %s): %s", oldRoot, strings.TrimSpace(string(out)))
	}
	return nil
}

func writeDockerDataRoot(root string) error {
	cfg := map[string]any{}
	if data, err := os.ReadFile(dockerDaemonConfigFile); err == nil {
		json.Unmarshal(data, &cfg)
	}
	cfg["data-root"] = root
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll("/etc/docker", 0755); err != nil {
		return err
	}
	tmp := dockerDaemonConfigFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, dockerDaemonConfigFile)
}

// --- Containers -----------------------------------------------------------------------------

type ContainerInfo struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Image   string  `json:"image"`
	Status  string  `json:"status"` // docker's own human-readable status, e.g. "Up 3 hours"
	State   string  `json:"state"`  // running/exited/paused/created/restarting/dead
	Ports   string  `json:"ports"`
	Created string  `json:"created"`
	SizeMB  float64 `json:"sizeMb,omitempty"`
}

// dockerPsLine mirrors the fields `docker ps --format '{{json .}}'` actually emits -- named exactly
// as Docker names them, not guessed.
type dockerPsLine struct {
	ID        string `json:"ID"`
	Names     string `json:"Names"`
	Image     string `json:"Image"`
	Status    string `json:"Status"`
	State     string `json:"State"`
	Ports     string `json:"Ports"`
	CreatedAt string `json:"CreatedAt"`
	Size      string `json:"Size"` // e.g. "1.2MB (virtual 120MB)" -- only the first (writable-layer) number is shown
}

func ListContainers() ([]ContainerInfo, error) {
	out, err := exec.Command("docker", "ps", "-a", "--format", "{{json .}}").Output()
	if err != nil {
		return nil, fmt.Errorf("docker ps failed: %w", err)
	}
	var containers []ContainerInfo
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var raw dockerPsLine
		if json.Unmarshal([]byte(line), &raw) != nil {
			continue
		}
		containers = append(containers, ContainerInfo{
			ID: raw.ID, Name: raw.Names, Image: raw.Image, Status: raw.Status,
			State: raw.State, Ports: raw.Ports, Created: raw.CreatedAt,
			SizeMB: parseDockerSizeMB(raw.Size),
		})
	}
	return containers, nil
}

// parseDockerSizeMB reads only the first (writable-layer) size Docker reports, e.g. "1.23MB" out of
// "1.23MB (virtual 120MB)" -- the virtual size mostly reflects the shared, already-counted base
// image, not this specific container's own footprint.
func parseDockerSizeMB(s string) float64 {
	first := strings.Fields(s)
	if len(first) == 0 {
		return 0
	}
	token := first[0]
	var numStr, unit string
	for i, r := range token {
		if (r < '0' || r > '9') && r != '.' {
			numStr, unit = token[:i], token[i:]
			break
		}
	}
	if numStr == "" {
		return 0
	}
	val, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0
	}
	switch strings.ToUpper(unit) {
	case "GB":
		return val * 1024
	case "KB":
		return val / 1024
	case "B":
		return val / 1024 / 1024
	default: // MB, or unrecognized -- assume MB rather than silently dropping the value
		return val
	}
}

// ContainerAction runs a bounded, known verb against a container ID -- never a client-supplied
// command string, same "never accept arbitrary text as a shell argument" discipline the Apps tab's
// own action allow-list already uses (id itself is passed as a single exec.Command argument, never
// interpolated into a shell string, so it can't be used to inject anything even if malformed).
func ContainerAction(id, action string) error {
	var args []string
	switch action {
	case "start", "stop", "pause", "unpause", "restart":
		args = []string{action, id}
	case "remove":
		args = []string{"rm", "-f", id}
	default:
		return fmt.Errorf("unsupported action: %s", action)
	}
	if out, err := exec.Command("docker", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func ContainerLogs(id string, lines int) (string, error) {
	if lines <= 0 {
		lines = 200
	}
	out, err := exec.Command("docker", "logs", "--tail", strconv.Itoa(lines), id).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("docker logs failed: %w", err)
	}
	return string(out), nil
}

// --- Create / run a new container from an image ----------------------------------------------

type RunContainerRequest struct {
	Image         string   `json:"image"`
	Name          string   `json:"name,omitempty"`
	Ports         []string `json:"ports,omitempty"`   // "host:container"
	Volumes       []string `json:"volumes,omitempty"` // "host:container"
	Env           []string `json:"env,omitempty"`     // "KEY=value"
	RestartPolicy string   `json:"restartPolicy,omitempty"`
	MemoryLimit   string   `json:"memoryLimit,omitempty"` // e.g. "512m" -- optional safety valve, RAM is this hardware's real binding constraint
}

var validRestartPolicies = map[string]bool{"": true, "no": true, "always": true, "unless-stopped": true, "on-failure": true}

// RunContainer pulls the image first as its own visible step (so a bad image name or a network
// hiccup fails clearly, not as a cryptic docker-run error), then runs it with the assembled flags.
// Returns the combined output of both steps for display.
func RunContainer(req RunContainerRequest) (string, error) {
	if strings.TrimSpace(req.Image) == "" {
		return "", fmt.Errorf("image is required")
	}
	if !validRestartPolicies[req.RestartPolicy] {
		return "", fmt.Errorf("unrecognized restart policy: %s", req.RestartPolicy)
	}

	var log strings.Builder
	fmt.Fprintf(&log, "$ docker pull %s\n", req.Image)
	pullOut, err := exec.Command("docker", "pull", req.Image).CombinedOutput()
	log.Write(pullOut)
	if err != nil {
		return log.String(), fmt.Errorf("pulling %s failed: %w", req.Image, err)
	}

	args := []string{"run", "-d"}
	if req.Name != "" {
		args = append(args, "--name", req.Name)
	}
	if req.RestartPolicy != "" && req.RestartPolicy != "no" {
		args = append(args, "--restart", req.RestartPolicy)
	}
	if req.MemoryLimit != "" {
		args = append(args, "--memory", req.MemoryLimit)
	}
	for _, p := range req.Ports {
		if p = strings.TrimSpace(p); p != "" {
			args = append(args, "-p", p)
		}
	}
	for _, v := range req.Volumes {
		if v = strings.TrimSpace(v); v != "" {
			args = append(args, "-v", v)
		}
	}
	for _, e := range req.Env {
		if e = strings.TrimSpace(e); e != "" {
			args = append(args, "-e", e)
		}
	}
	args = append(args, req.Image)

	fmt.Fprintf(&log, "\n$ docker %s\n", strings.Join(args, " "))
	runOut, err := exec.Command("docker", args...).CombinedOutput()
	log.Write(runOut)
	if err != nil {
		return log.String(), fmt.Errorf("docker run failed: %w", err)
	}
	return log.String(), nil
}
