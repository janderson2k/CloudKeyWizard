package main

import (
	"encoding/json"
	"net/http"
)

func handleLCDStatus(w http.ResponseWriter, r *http.Request, _ string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(getLCDStatus())
}

type setHostnameRequest struct {
	Hostname string `json:"hostname"`
}

func handleLCDSet(w http.ResponseWriter, r *http.Request, _ string) {
	var req setHostnameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	status, err := setHostname(req.Hostname)
	if err != nil {
		http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// handleLCDApparmorFix responds to a click on the Front Panel tab's own "Attempt fix" button --
// it only ever acts on the single profile getLCDStatus() already identified from a real kernel-
// log AppArmor DENIED entry (see checkApparmorDenial in lcd.go), never an arbitrary client-
// supplied profile name.
func handleLCDApparmorFix(w http.ResponseWriter, r *http.Request, _ string) {
	status := getLCDStatus()
	if err := fixApparmorProfile(status.ApparmorProfile); err != nil {
		http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(getLCDStatus())
}
