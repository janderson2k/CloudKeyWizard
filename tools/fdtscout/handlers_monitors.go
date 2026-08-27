package main

import (
	"encoding/json"
	"net/http"
)

func handleMonitorsList(engine *MonitorEngine) func(http.ResponseWriter, *http.Request, string) {
	return func(w http.ResponseWriter, r *http.Request, _ string) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(engine.Snapshots())
	}
}

func handleMonitorsSave(engine *MonitorEngine) func(http.ResponseWriter, *http.Request, string) {
	return func(w http.ResponseWriter, r *http.Request, _ string) {
		var monitors []Monitor
		if err := json.NewDecoder(r.Body).Decode(&monitors); err != nil {
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}
		if err := engine.Save(monitors); err != nil {
			http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(engine.Snapshots())
	}
}

func handleMonitorHistory(engine *MonitorEngine) func(http.ResponseWriter, *http.Request, string) {
	return func(w http.ResponseWriter, r *http.Request, _ string) {
		id := r.PathValue("id")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(engine.HistoryFor(id))
	}
}

func handleMonitorRunNow(engine *MonitorEngine) func(http.ResponseWriter, *http.Request, string) {
	return func(w http.ResponseWriter, r *http.Request, _ string) {
		id := r.PathValue("id")
		result, err := engine.RunNow(id)
		if err != nil {
			http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}
