package main

import (
	"encoding/json"
	"net/http"
)

func handleDisplayStatus(w http.ResponseWriter, r *http.Request, _ string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(LoadDisplayConfig())
}

func handleDisplayUpdate(w http.ResponseWriter, r *http.Request, _ string) {
	var cfg DisplayConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	if err := SaveDisplayConfig(cfg); err != nil {
		http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusBadRequest)
		return
	}
	// Re-open/restart the cycle against the new config immediately, using the same metrics
	// collector already running -- no need to wait for the next tick or a service restart.
	startLCDDisplay(lcdManager.metrics)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(LoadDisplayConfig())
}
