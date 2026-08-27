package main

import (
	"encoding/json"
	"net/http"
)

func handleNetworkStatus(netConfig *NetConfigManager) func(http.ResponseWriter, *http.Request, string) {
	return func(w http.ResponseWriter, r *http.Request, _ string) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(netConfig.Status())
	}
}

type applyNetworkRequest struct {
	Mode    string `json:"mode"` // "static" or "dhcp"
	Address string `json:"address,omitempty"`
	Gateway string `json:"gateway,omitempty"`
}

func handleNetworkApply(netConfig *NetConfigManager) func(http.ResponseWriter, *http.Request, string) {
	return func(w http.ResponseWriter, r *http.Request, _ string) {
		var req applyNetworkRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}
		if err := netConfig.Apply(req.Mode, req.Address, req.Gateway); err != nil {
			http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(netConfig.Status())
	}
}

func handleNetworkConfirm(netConfig *NetConfigManager) func(http.ResponseWriter, *http.Request, string) {
	return func(w http.ResponseWriter, r *http.Request, _ string) {
		if err := netConfig.Confirm(); err != nil {
			http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}
}
