package main

import (
	"encoding/json"
	"net/http"
)

type aboutResponse struct {
	Version   string           `json:"version"`
	BuildDate string           `json:"buildDate"`
	AboutText string           `json:"aboutText"`
	Changelog []ChangelogEntry `json:"changelog"`
}

func handleAbout(w http.ResponseWriter, r *http.Request, _ string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(aboutResponse{
		Version:   Version,
		BuildDate: BuildDate,
		AboutText: AboutText,
		Changelog: Changelog,
	})
}
