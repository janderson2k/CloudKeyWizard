package main

import (
	"encoding/json"
	"net/http"
)

func handleSysinfo(w http.ResponseWriter, r *http.Request, _ string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(GetSystemSpecs())
}
