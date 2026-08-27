package main

import (
	"encoding/json"
	"net/http"
)

func handleTasksList(w http.ResponseWriter, r *http.Request, _ string) {
	tasks, err := ListScheduledTasks()
	if err != nil {
		http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusInternalServerError)
		return
	}
	if tasks == nil {
		tasks = []ScheduledTask{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tasks)
}

func handleTasksSave(w http.ResponseWriter, r *http.Request, _ string) {
	var tasks []ScheduledTask
	if err := json.NewDecoder(r.Body).Decode(&tasks); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	if err := SaveScheduledTasks(tasks); err != nil {
		http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusInternalServerError)
		return
	}
	handleTasksList(w, r, "")
}
