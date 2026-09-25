package main

import (
	"encoding/json"
	"net/http"
)

func handleLifeRaftFilesList(w http.ResponseWriter, r *http.Request, _ string) {
	jobID := r.PathValue("id")
	path := r.URL.Query().Get("path")
	entries, err := listLifeRaftDirectory(jobID, path)
	if err != nil {
		http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entries)
}

func handleLifeRaftFileDownload(w http.ResponseWriter, r *http.Request, _ string) {
	jobID := r.PathValue("id")
	path := r.URL.Query().Get("path")
	resolved, err := resolveLifeRaftSafePath(jobID, path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.ServeFile(w, r, resolved)
}
