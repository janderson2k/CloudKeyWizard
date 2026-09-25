package main

import (
	"encoding/json"
	"net/http"
)

type smbBrowseRequest struct {
	Host         string `json:"host"`
	Port         int    `json:"port"`
	CredentialID string `json:"credentialId"`
	Share        string `json:"share,omitempty"` // empty = list shares; set = list a path within it
	Path         string `json:"path,omitempty"`
}

// handleLifeRaftSMBBrowse covers both steps of browsing from one endpoint: no Share in the
// request lists available shares, a Share present lists that share's Path. Keeps the frontend's
// "pick a share, then click through folders" flow to one call shape instead of two separate routes
// with subtly different request/response conventions.
func handleLifeRaftSMBBrowse(w http.ResponseWriter, r *http.Request, _ string) {
	var req smbBrowseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	if req.Host == "" || req.CredentialID == "" {
		http.Error(w, `{"error":"host and credential are required"}`, http.StatusBadRequest)
		return
	}
	if req.Port == 0 {
		req.Port = 445
	}

	w.Header().Set("Content-Type", "application/json")

	if req.Share == "" {
		shares, err := ListSMBShares(req.Host, req.Port, req.CredentialID)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string][]string{"shares": shares})
		return
	}

	entries, err := ListSMBPath(req.Host, req.Port, req.CredentialID, req.Share, req.Path)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string][]BrowseSMBEntry{"entries": entries})
}
