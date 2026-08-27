package main

import (
	"encoding/json"
	"net/http"
)

func handleMetrics(collector *MetricsCollector) func(http.ResponseWriter, *http.Request, string) {
	return func(w http.ResponseWriter, r *http.Request, _ string) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(collector.History())
	}
}
