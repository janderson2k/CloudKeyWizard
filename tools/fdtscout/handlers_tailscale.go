package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

func handleTailscaleStatus(w http.ResponseWriter, r *http.Request, _ string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(getTailscaleStatus())
}

type tailscaleJoinRequest struct {
	AuthKey     string `json:"authKey"`
	LoginServer string `json:"loginServer"`
	Hostname    string `json:"hostname"`
}

// handleTailscaleJoin covers both real ways to join a tailnet: with an auth key (synchronous,
// recommended -- returns immediately) or interactively via a browser login URL (asynchronous --
// returns the URL/QR to display, the actual login continues in the background; the frontend polls
// /api/tailscale afterward to see when it completes).
func handleTailscaleJoin(w http.ResponseWriter, r *http.Request, _ string) {
	var req tailscaleJoinRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	req.AuthKey = strings.TrimSpace(req.AuthKey)
	req.LoginServer = strings.TrimSpace(req.LoginServer)
	req.Hostname = strings.TrimSpace(req.Hostname)

	w.Header().Set("Content-Type", "application/json")

	if req.AuthKey != "" {
		out, err := tailscaleUpWithAuthKey(req.AuthKey, req.LoginServer, req.Hostname)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error(), "output": out})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"output": out})
		return
	}

	authURL, qr, err := tailscaleUpInteractive(req.LoginServer, req.Hostname)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"authUrl": authURL, "qr": qr})
}

func handleTailscaleLogout(w http.ResponseWriter, r *http.Request, _ string) {
	if err := tailscaleLogout(); err != nil {
		http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}
