package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

type addUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func handleListUsers(users *UserStore, authLog *AuthLog) func(http.ResponseWriter, *http.Request, string) {
	return func(w http.ResponseWriter, r *http.Request, _ string) {
		list := users.List()
		lastLogins := authLog.LastLoginByUser()
		for i := range list {
			if t, ok := lastLogins[list[i].Username]; ok {
				list[i].LastLogin = &t
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(list)
	}
}

func handleAuthLog(authLog *AuthLog) func(http.ResponseWriter, *http.Request, string) {
	return func(w http.ResponseWriter, r *http.Request, _ string) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(authLog.History(100))
	}
}

// handleAddUser also serves as "change password for an existing user" -- AddOrReplace
// upserts, and the frontend's "Users" tab uses the same form for both (matching Windows app's
// own "no separate flow for a common variant of the same action" pattern in its password-change
// utility).
func handleAddUser(users *UserStore) func(http.ResponseWriter, *http.Request, string) {
	return func(w http.ResponseWriter, r *http.Request, _ string) {
		var req addUserRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}
		req.Username = strings.TrimSpace(req.Username)
		if len(req.Password) < 8 {
			http.Error(w, `{"error":"password must be at least 8 characters"}`, http.StatusBadRequest)
			return
		}
		if err := users.AddOrReplace(req.Username, req.Password); err != nil {
			http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}
}

func handleRemoveUser(users *UserStore) func(http.ResponseWriter, *http.Request, string) {
	return func(w http.ResponseWriter, r *http.Request, requester string) {
		target := strings.TrimPrefix(r.URL.Path, "/api/users/")
		if target == requester {
			http.Error(w, `{"error":"can't remove the account you're currently logged in as"}`, http.StatusBadRequest)
			return
		}
		if err := users.Remove(target); err != nil {
			http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}
}

func jsonEscape(s string) string {
	b, _ := json.Marshal(s)
	// Marshal wraps in quotes; strip them since callers here are embedding into a hand-built
	// {"error":"..."} string rather than marshaling a struct.
	return string(b[1 : len(b)-1])
}
