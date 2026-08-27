package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// installWorkDir mirrors SshSessionService.RemoteWorkDir on the Windows side (the same
// "/root/.cloudkey-wizard" path) -- not because anything shares state between the SSH-driven flow
// and this one, but so both leave their audit trail in the same conventional place rather than
// scattering install artifacts across two different directories with the same purpose.
const installWorkDir = "/root/.cloudkey-wizard"

func handleInstallList(w http.ResponseWriter, r *http.Request, _ string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(installCatalog)
}

type installRequest struct {
	Params map[string]string `json:"params"`
}

type installResponse struct {
	ExitCode int    `json:"exitCode"`
	Output   string `json:"output"`
}

// handleInstallRun is synchronous and blocking, deliberately -- a live-streamed install (like the
// terminal's WebSocket) would be nicer, but this ships full working installs now rather than
// waiting on that polish. Go's net/http has no default handler timeout, so a multi-minute apt
// install completes fine; the frontend just shows a spinner instead of a live log for this first
// pass.
func handleInstallRun(w http.ResponseWriter, r *http.Request, _ string) {
	id := strings.TrimPrefix(r.URL.Path, "/api/install/")
	def := findInstallDef(id)
	if def == nil {
		http.Error(w, `{"error":"no such app"}`, http.StatusNotFound)
		return
	}

	var req installRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	if req.Params == nil {
		req.Params = map[string]string{}
	}
	for _, p := range def.Params {
		if p.Required && strings.TrimSpace(req.Params[p.EnvVar]) == "" {
			http.Error(w, `{"error":"`+jsonEscape(fmt.Sprintf("'%s' is required", p.Label))+`"}`, http.StatusBadRequest)
			return
		}
	}

	if err := os.MkdirAll(installWorkDir, 0700); err != nil {
		http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusInternalServerError)
		return
	}

	var output strings.Builder

	if def.PrerequisiteScriptFile != "" {
		fmt.Fprintf(&output, "--- prerequisite: %s ---\n", def.PrerequisiteScriptFile)
		exitCode, out, err := runBundledScript(def.PrerequisiteScriptFile, nil)
		output.WriteString(out)
		if err != nil || exitCode != 0 {
			setAppStatus(def.ID, "Failed", fmt.Sprintf("Prerequisite %s failed (exit %d).", def.PrerequisiteScriptFile, exitCode))
			writeInstallResult(w, exitCode, output.String())
			return
		}
	}

	for _, sibling := range def.SiblingScriptFiles {
		if err := writeBundledScriptToWorkDir(sibling); err != nil {
			output.WriteString("failed to place sibling script " + sibling + ": " + err.Error() + "\n")
			setAppStatus(def.ID, "Failed", "Setup failed: "+err.Error())
			writeInstallResult(w, 1, output.String())
			return
		}
	}

	envPairs := make([]string, 0, len(req.Params))
	for _, p := range def.Params {
		value := strings.TrimSpace(req.Params[p.EnvVar])
		if value == "" {
			value = p.Default
		}
		envPairs = append(envPairs, p.EnvVar+"="+value)
	}

	var exitCode int
	var out string
	var err error
	if def.BundledScriptFile != "" {
		fmt.Fprintf(&output, "--- %s (%s, bundled from jnovack/cloudkey) ---\n", def.Title, def.BundledScriptFile)
		exitCode, out, err = runBundledScript(def.BundledScriptFile, envPairs)
	} else {
		fmt.Fprintf(&output, "--- %s (app-authored) ---\n", def.Title)
		exitCode, out, err = runScriptContent(def.ID, def.ScriptContent, envPairs)
	}
	output.WriteString(out)

	if err != nil || exitCode != 0 {
		setAppStatus(def.ID, "Failed", fmt.Sprintf("Exited with code %d.", exitCode))
	} else {
		setAppStatus(def.ID, "Done", "Installed via FDT.Scout.")
	}
	writeInstallResult(w, exitCode, output.String())
}

func writeInstallResult(w http.ResponseWriter, exitCode int, output string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(installResponse{ExitCode: exitCode, Output: output})
}

func writeBundledScriptToWorkDir(fileName string) error {
	content, err := bundledInstallScripts.ReadFile("scripts/" + fileName)
	if err != nil {
		return fmt.Errorf("embedded script '%s' not found: %w", fileName, err)
	}
	dest := filepath.Join(installWorkDir, fileName)
	if err := os.WriteFile(dest, content, 0755); err != nil {
		return err
	}
	return nil
}

func runBundledScript(fileName string, env []string) (int, string, error) {
	if err := writeBundledScriptToWorkDir(fileName); err != nil {
		return 1, err.Error() + "\n", err
	}
	return runScriptFile(filepath.Join(installWorkDir, fileName), env)
}

func runScriptContent(id, content string, env []string) (int, string, error) {
	dest := filepath.Join(installWorkDir, "install-"+id+".sh")
	if err := os.WriteFile(dest, []byte(content), 0755); err != nil {
		return 1, err.Error() + "\n", err
	}
	return runScriptFile(dest, env)
}

// runScriptFile matches SshSessionService.RunScriptStreamingAsync's `< /dev/null` discipline even
// though this runs locally, not over SSH -- a script's `read -rp` confirmation prompt (several of
// the bundled scripts have one) would otherwise block forever waiting for input from a process
// with no attached terminal, exactly the bug that was chased down and fixed on the SSH side
// earlier in this project. cmd.Stdin left nil already gives an immediately-EOF stdin in Go's
// os/exec (no separate `< /dev/null` needed the way a shell invocation needs it spelled out), but
// documented here so the parallel with the SSH-side fix isn't lost on a future reader.
func runScriptFile(path string, env []string) (int, string, error) {
	cmd := exec.Command("bash", path)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	exitCode := 0
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	} else if err != nil {
		exitCode = 1
	}
	return exitCode, string(out), err
}
