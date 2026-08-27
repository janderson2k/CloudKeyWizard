package main

import (
	"encoding/json"
	"net/http"
	"strconv"
)

func handleLogsList(w http.ResponseWriter, r *http.Request, _ string) {
	w.Header().Set("Content-Type", "application/json")
	files := listLogFiles()
	if files == nil {
		files = []LogFileInfo{}
	}
	json.NewEncoder(w).Encode(files)
}

func handleLogsRead(w http.ResponseWriter, r *http.Request, _ string) {
	name := r.URL.Query().Get("name")
	maxLines := 2000
	if v := r.URL.Query().Get("lines"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxLines = n
		}
	}
	content, err := readLogFile(name, maxLines)
	if err != nil {
		http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(content))
}
