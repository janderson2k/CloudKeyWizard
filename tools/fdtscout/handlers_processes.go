package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
)

func handleProcesses(w http.ResponseWriter, r *http.Request, _ string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(GetProcessSnapshot(25))
}

type killRequest struct {
	Signal string `json:"signal"` // "TERM" or "KILL" -- defaults to TERM
}

// handleProcessKill signals a PID -- guarded against killing PID 1 (init; would panic the kernel
// or at minimum take the whole system down with it) and against killing this binary's own PID
// (self-signaling mid-request would just leave the console half-dead instead of actually helping
// -- if fdtscout itself needs killing, that's what `systemctl stop fdtscout` on the Apps tab is
// for, a clean shutdown rather than a self-inflicted SIGKILL). Everything else is allowed -- this
// is already a root-capable console, and a real allow-list of "safe to kill" processes isn't what
// a `top`-style kill feature is for.
func handleProcessKill(w http.ResponseWriter, r *http.Request, _ string) {
	pidStr := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/processes/"), "/kill")
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 0 {
		http.Error(w, `{"error":"invalid pid"}`, http.StatusBadRequest)
		return
	}
	if pid == 1 {
		http.Error(w, `{"error":"refusing to kill PID 1 (init) -- that would take the whole system down"}`, http.StatusBadRequest)
		return
	}
	if pid == os.Getpid() {
		http.Error(w, `{"error":"refusing to kill this console's own process -- use the Apps tab to stop the fdtscout service cleanly instead"}`, http.StatusBadRequest)
		return
	}

	var req killRequest
	_ = json.NewDecoder(r.Body).Decode(&req) // empty/missing body is fine -- defaults to TERM below

	if err := KillProcess(pid, req.Signal); err != nil {
		http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
}
