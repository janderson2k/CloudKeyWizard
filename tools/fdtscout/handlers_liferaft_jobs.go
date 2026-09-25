package main

import (
	"encoding/json"
	"log"
	"net/http"
)

// lifeRaftJobView adds the in-memory "is this job running right now" flag alongside the persisted
// job fields -- a UI-visibility concern only, so it doesn't belong on the stored LifeRaftJob itself.
type lifeRaftJobView struct {
	LifeRaftJob
	Running bool `json:"running"`
}

func handleLifeRaftJobsList(w http.ResponseWriter, r *http.Request, _ string) {
	jobs, err := ListLifeRaftJobs()
	if err != nil {
		http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusInternalServerError)
		return
	}
	views := make([]lifeRaftJobView, len(jobs))
	for i, j := range jobs {
		views[i] = lifeRaftJobView{LifeRaftJob: j, Running: IsLifeRaftJobRunning(j.ID)}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(views)
}

func handleLifeRaftJobsSave(w http.ResponseWriter, r *http.Request, _ string) {
	var incoming LifeRaftJob
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	saved, err := SaveLifeRaftJob(incoming)
	if err != nil {
		http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(saved)
}

func handleLifeRaftJobsDelete(w http.ResponseWriter, r *http.Request, _ string) {
	id := r.PathValue("id")
	if err := DeleteLifeRaftJob(id); err != nil {
		http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// handleLifeRaftJobRunNow kicks the run off in a goroutine and returns immediately -- a real
// SMB/FTP transfer can run for minutes, and holding the HTTP request open for that (plus this
// job's own place in the global run queue ahead of it, see acquireLifeRaftRunLock) isn't a
// reasonable request/response shape. The frontend is expected to poll GET .../runs afterward,
// same pattern the Docker tab's own run-container flow and the install flow already use for
// long-running actions.
func handleLifeRaftJobRunNow(w http.ResponseWriter, r *http.Request, _ string) {
	id := r.PathValue("id")
	jobs, err := ListLifeRaftJobs()
	if err != nil {
		http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusInternalServerError)
		return
	}
	found := false
	for _, j := range jobs {
		if j.ID == id {
			found = true
			break
		}
	}
	if !found {
		http.Error(w, `{"error":"no such job"}`, http.StatusNotFound)
		return
	}
	go func() {
		if err := RunLifeRaftJobNow(id); err != nil {
			// Already recorded in this job's own run history by RunLifeRaftJob's own defer --
			// nothing more to do here than note it for the server's own log.
			log.Printf("liferaft: run-now for job %s finished with error: %v", id, err)
		}
	}()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"started": true})
}

func handleLifeRaftJobRuns(w http.ResponseWriter, r *http.Request, _ string) {
	id := r.PathValue("id")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ListLifeRaftRuns(id))
}
