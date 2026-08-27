package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

func errNotFound(id string) error       { return fmt.Errorf("no such app: %s", id) }
func errBadAction(action string) error  { return fmt.Errorf("unsupported action: %s", action) }
func errNotToggleable(id string) error {
	return fmt.Errorf("%s can't be toggled here -- it's named per-instance; use the terminal (systemctl <action> <unit>)", id)
}
func errAction(action, unit string, out []byte, err error) error {
	return fmt.Errorf("%s %s failed: %s: %w", action, unit, strings.TrimSpace(string(out)), err)
}

func handleAppsList(w http.ResponseWriter, r *http.Request, _ string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(getAppStatuses())
}

type appActionRequest struct {
	Action string `json:"action"`
}

func handleAppAction(w http.ResponseWriter, r *http.Request, _ string) {
	id := strings.TrimPrefix(r.URL.Path, "/api/apps/")
	id = strings.TrimSuffix(id, "/action")
	var req appActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	status, err := applyAppAction(id, req.Action)
	if err != nil {
		http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}
