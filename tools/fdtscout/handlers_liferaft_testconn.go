package main

import (
	"encoding/json"
	"net/http"
)

// liferaftTestRequest carries just the connection-relevant fields of a job -- a user should be
// able to test a source before ever saving the job, so this takes the form's current values
// directly rather than requiring a saved job ID.
type liferaftTestRequest struct {
	Protocol     LifeRaftProtocol `json:"protocol"`
	Host         string           `json:"host"`
	Port         int              `json:"port"`
	Share        string           `json:"share"`
	Path         string           `json:"path"`
	CredentialID string           `json:"credentialId"`
}

// handleLifeRaftTestConnection runs the EXACT connect-and-list-the-configured-path logic a real
// run would use (connectLifeRaftSource, then List against the job's own root), so a pass here means
// a real run would get past the connection step too -- not a lighter/different check that could
// give false confidence. Always closes the connection immediately after; this is an audition, not a
// hot path, same discipline the SMB share/folder browser already uses.
func handleLifeRaftTestConnection(w http.ResponseWriter, r *http.Request, _ string) {
	var req liferaftTestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	job := LifeRaftJob{
		Protocol:     req.Protocol,
		Host:         req.Host,
		Port:         req.Port,
		Share:        req.Share,
		Path:         req.Path,
		CredentialID: req.CredentialID,
	}
	if job.Path == "" {
		job.Path = "/"
	}
	switch job.Protocol {
	case ProtocolSMB:
		if job.Port == 0 {
			job.Port = 445
		}
	case ProtocolFTP, ProtocolFTPS:
		if job.Port == 0 {
			job.Port = 21
		}
	}

	w.Header().Set("Content-Type", "application/json")

	src, err := connectLifeRaftSource(job)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	defer src.Close()

	files, subdirs, err := src.List("")
	if err != nil {
		json.NewEncoder(w).Encode(map[string]string{"error": "connected, but couldn't list the configured path: " + err.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]any{
		"ok":      true,
		"files":   len(files),
		"folders": len(subdirs),
	})
}
