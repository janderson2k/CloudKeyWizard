package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

func handleDockerStatus(w http.ResponseWriter, r *http.Request, _ string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(getDockerStatus())
}

func handleDockerInstall(w http.ResponseWriter, r *http.Request, _ string) {
	if err := installDocker(); err != nil {
		http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(getDockerStatus())
}

func handleDockerContainersList(w http.ResponseWriter, r *http.Request, _ string) {
	containers, err := ListContainers()
	if err != nil {
		http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(containers)
}

// path pattern: /api/docker/containers/{id}/{action}
func handleDockerContainerAction(w http.ResponseWriter, r *http.Request, _ string) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/docker/containers/")
	parts := strings.Split(rest, "/")
	if len(parts) != 2 {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	id, action := parts[0], parts[1]
	if err := ContainerAction(id, action); err != nil {
		http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// path pattern: /api/docker/containers/{id}/logs?lines=200
func handleDockerContainerLogs(w http.ResponseWriter, r *http.Request, _ string) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/docker/containers/")
	id := strings.TrimSuffix(rest, "/logs")
	lines, _ := strconv.Atoi(r.URL.Query().Get("lines"))
	logText, err := ContainerLogs(id, lines)
	if err != nil {
		http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"log": logText})
}

func handleDockerRun(w http.ResponseWriter, r *http.Request, _ string) {
	var req RunContainerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	logText, err := RunContainer(req)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error(), "log": logText})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"log": logText})
}

type dockerStorageRequest struct {
	Path string `json:"path"`
}

func handleDockerStorageSet(w http.ResponseWriter, r *http.Request, _ string) {
	var req dockerStorageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Path) == "" {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	if err := SetDockerStorageRoot(req.Path); err != nil {
		http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(getDockerStatus())
}
