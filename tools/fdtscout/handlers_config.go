package main

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"time"
)

func handleConfigStatus(w http.ResponseWriter, r *http.Request, _ string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(LoadRuntimeConfig())
}

// handleConfigUpdate saves the new port/redirect settings and schedules a self-restart -- the
// HTTPS listener can't rebind to a different port live without dropping every open connection
// anyway, so a restart is the honest way to apply this rather than pretending it's instant. The
// response is sent (and flushed) BEFORE the restart actually fires, so the browser's fetch()
// resolves normally; the frontend is responsible for warning the user they'll need to reconnect
// at the new port if it changed.
func handleConfigUpdate(w http.ResponseWriter, r *http.Request, _ string) {
	var cfg RuntimeConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		http.Error(w, `{"error":"port must be between 1 and 65535"}`, http.StatusBadRequest)
		return
	}
	if err := SaveRuntimeConfig(cfg); err != nil {
		http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "restarting": true, "port": cfg.Port})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	go func() {
		time.Sleep(750 * time.Millisecond) // give the response time to actually reach the browser
		_ = exec.Command("systemctl", "restart", "fdtscout").Run()
	}()
}
