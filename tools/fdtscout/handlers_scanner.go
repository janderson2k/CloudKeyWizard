package main

import (
	"encoding/json"
	"net/http"
)

// handleScanSubnet scans the device's own local subnet by default -- or, if both `start` and `end`
// query params are given, a user-specified custom range instead (ScanIPRange).
func handleScanSubnet(w http.ResponseWriter, r *http.Request, _ string) {
	start := r.URL.Query().Get("start")
	end := r.URL.Query().Get("end")

	var hosts []ScanHost
	var err error
	if start != "" && end != "" {
		hosts, err = ScanIPRange(start, end)
	} else {
		hosts, err = ScanLocalSubnet()
	}
	if err != nil {
		http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(hosts)
}

type portScanRequest struct {
	Host string `json:"host"`
}

func handleScanPorts(w http.ResponseWriter, r *http.Request, _ string) {
	var req portScanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Host == "" {
		http.Error(w, `{"error":"host is required"}`, http.StatusBadRequest)
		return
	}
	results := ScanPorts(req.Host)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

func handleLANList(w http.ResponseWriter, r *http.Request, _ string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(LANDeviceList())
}

type wolRequest struct {
	MAC string `json:"mac"`
}

func handleWakeOnLAN(w http.ResponseWriter, r *http.Request, _ string) {
	var req wolRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.MAC == "" {
		http.Error(w, `{"error":"mac is required"}`, http.StatusBadRequest)
		return
	}
	if err := SendWakeOnLAN(req.MAC); err != nil {
		http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"sent": true})
}

func handleWANSpeedHistory(history *WANSpeedHistory) func(http.ResponseWriter, *http.Request, string) {
	return func(w http.ResponseWriter, r *http.Request, _ string) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(history.History())
	}
}

func handleWANSpeedRunNow(history *WANSpeedHistory) func(http.ResponseWriter, *http.Request, string) {
	return func(w http.ResponseWriter, r *http.Request, _ string) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(history.RunNow())
	}
}

func handlePublicIPHistory(tracker *PublicIPTracker) func(http.ResponseWriter, *http.Request, string) {
	return func(w http.ResponseWriter, r *http.Request, _ string) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(tracker.History())
	}
}

func handleDDNSStatus(w http.ResponseWriter, r *http.Request, _ string) {
	cfg := LoadDDNSConfig()
	if cfg.APIToken != "" {
		cfg.APIToken = maskedTokenPlaceholder
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cfg)
}

// Same "never echo a saved secret back in full" pattern as Pushbullet's own settings handler --
// see handlePushbulletUpdate's doc comment.
func handleDDNSUpdate(w http.ResponseWriter, r *http.Request, _ string) {
	var incoming DDNSConfig
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	existing := LoadDDNSConfig()
	if incoming.APIToken == "" || incoming.APIToken == maskedTokenPlaceholder {
		incoming.APIToken = existing.APIToken
	}
	if err := SaveDDNSConfig(incoming); err != nil {
		http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusBadRequest)
		return
	}
	out := incoming
	if out.APIToken != "" {
		out.APIToken = maskedTokenPlaceholder
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}
