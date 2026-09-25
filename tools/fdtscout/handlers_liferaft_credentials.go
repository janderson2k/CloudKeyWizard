package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

func handleLifeRaftCredentialsList(w http.ResponseWriter, r *http.Request, _ string) {
	creds, err := ListLifeRaftCredentialsMasked()
	if err != nil {
		http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(creds)
}

// handleLifeRaftCredentialsSave covers both create (no id in the body) and update (id present) --
// same shape SaveLifeRaftCredential itself branches on, so the HTTP layer doesn't need its own
// separate create/update split.
func handleLifeRaftCredentialsSave(w http.ResponseWriter, r *http.Request, _ string) {
	var incoming LifeRaftCredential
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	saved, err := SaveLifeRaftCredential(incoming)
	if err != nil {
		http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusBadRequest)
		return
	}
	saved.Password = lifeRaftPasswordPlaceholder
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(saved)
}

// handleLifeRaftCredentialsDelete is the route DeleteLifeRaftCredential's own doc comment said
// would arrive once the job store existed -- lifeRaftJobsUsingCredential (liferaft_jobs.go) is the
// real in-use check, not a stub, so this can never silently break a scheduled job.
func handleLifeRaftCredentialsDelete(w http.ResponseWriter, r *http.Request, _ string) {
	id := r.PathValue("id")
	if inUse := lifeRaftJobsUsingCredential(id); len(inUse) > 0 {
		http.Error(w, `{"error":"in use by: `+jsonEscape(strings.Join(inUse, ", "))+` -- reassign or remove those jobs first"}`, http.StatusConflict)
		return
	}
	if err := DeleteLifeRaftCredential(id); err != nil {
		http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}
