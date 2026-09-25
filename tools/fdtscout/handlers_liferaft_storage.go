package main

import (
	"encoding/json"
	"net/http"
)

func handleLifeRaftStorageStatus(w http.ResponseWriter, r *http.Request, _ string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(GetLifeRaftStorageStatus())
}

func handleLifeRaftDrivesList(w http.ResponseWriter, r *http.Request, _ string) {
	drives, err := ListCandidateDrives()
	if err != nil {
		http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(drives)
}

type lifeRaftStorageSetupRequest struct {
	Device string `json:"device"`
	// Confirm must exactly equal Device -- same "type the exact thing you're about to destroy"
	// pattern CloudKeyWizard's own Danger-tier steps use, required here too since this is every
	// bit as destructive (unconditional whole-disk wipe) as that Windows app's own format step.
	Confirm string `json:"confirm"`
}

func handleLifeRaftStorageSetup(w http.ResponseWriter, r *http.Request, _ string) {
	var req lifeRaftStorageSetupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	if req.Device == "" || req.Confirm != req.Device {
		http.Error(w, `{"error":"confirmation must exactly match the device name -- nothing was touched"}`, http.StatusBadRequest)
		return
	}
	if err := SetUpLifeRaftStorage(req.Device); err != nil {
		http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(GetLifeRaftStorageStatus())
}
