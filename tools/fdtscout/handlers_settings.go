package main

import (
	"encoding/json"
	"net/http"
)

func handleNTPStatus(w http.ResponseWriter, r *http.Request, _ string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(getNTPStatus())
}

type ntpUpdateRequest struct {
	Enabled bool     `json:"enabled"`
	Servers []string `json:"servers"`
}

func handleNTPUpdate(w http.ResponseWriter, r *http.Request, _ string) {
	var req ntpUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	if len(req.Servers) > 0 {
		if err := setNTPServers(req.Servers); err != nil {
			http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusBadRequest)
			return
		}
	}
	if err := setNTPEnabled(req.Enabled); err != nil {
		http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(getNTPStatus())
}

func handleDNSStatus(w http.ResponseWriter, r *http.Request, _ string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(getDNSStatus())
}

type dnsUpdateRequest struct {
	Servers []string `json:"servers"`
}

func handleDNSUpdate(w http.ResponseWriter, r *http.Request, _ string) {
	var req dnsUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	if len(req.Servers) == 0 {
		http.Error(w, `{"error":"at least one DNS server is required"}`, http.StatusBadRequest)
		return
	}
	if err := setDNSServers(req.Servers); err != nil {
		http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(getDNSStatus())
}

func handleTimezoneStatus(w http.ResponseWriter, r *http.Request, _ string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(getTimezoneStatus())
}

type timezoneUpdateRequest struct {
	Timezone string `json:"timezone"`
}

func handleTimezoneUpdate(w http.ResponseWriter, r *http.Request, _ string) {
	var req timezoneUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	if err := setTimezone(req.Timezone); err != nil {
		http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(getTimezoneStatus())
}

// Pushbullet's own settings never echo the stored access token back to the browser in full --
// same "don't put a secret back on the wire once it's saved" posture the app already applies to
// passwords. A masked placeholder is returned instead when a token is already configured, and the
// save handler leaves the existing token untouched if the field comes back as that same
// placeholder (i.e. the user didn't retype it).
const maskedTokenPlaceholder = "••••••••(unchanged)"

func handlePushbulletStatus(w http.ResponseWriter, r *http.Request, _ string) {
	cfg := LoadPushbulletConfig()
	out := cfg
	if out.Token != "" {
		out.Token = maskedTokenPlaceholder
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

func handlePushbulletUpdate(w http.ResponseWriter, r *http.Request, _ string) {
	var incoming PushbulletConfig
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	existing := LoadPushbulletConfig()
	if incoming.Token == "" || incoming.Token == maskedTokenPlaceholder {
		incoming.Token = existing.Token
	}
	if err := SavePushbulletConfig(incoming); err != nil {
		http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusBadRequest)
		return
	}
	out := incoming
	if out.Token != "" {
		out.Token = maskedTokenPlaceholder
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}
