package main

import (
	"encoding/json"
	"io"
	"net/http"
)

func handleCertStatus(certs *CertManager) func(http.ResponseWriter, *http.Request, string) {
	return func(w http.ResponseWriter, r *http.Request, _ string) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(certs.Info())
	}
}

func handleCertGenerate(certs *CertManager) func(http.ResponseWriter, *http.Request, string) {
	return func(w http.ResponseWriter, r *http.Request, _ string) {
		if err := certs.GenerateSelfSigned(); err != nil {
			http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(certs.Info())
	}
}

// handleCertUpload accepts multipart form fields "cert" and "key", each the raw PEM file content
// -- simpler than requiring two separate paste boxes and file-vs-paste branching in the UI, and
// works the same whether the browser sends it via <input type=file> or a pasted-into-a-hidden-
// file blob.
func handleCertUpload(certs *CertManager) func(http.ResponseWriter, *http.Request, string) {
	return func(w http.ResponseWriter, r *http.Request, _ string) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			http.Error(w, `{"error":"bad upload"}`, http.StatusBadRequest)
			return
		}
		certPEM, err := readFormFile(r, "cert")
		if err != nil {
			http.Error(w, `{"error":"missing or unreadable cert file"}`, http.StatusBadRequest)
			return
		}
		keyPEM, err := readFormFile(r, "key")
		if err != nil {
			http.Error(w, `{"error":"missing or unreadable key file"}`, http.StatusBadRequest)
			return
		}
		if err := certs.InstallUploaded(certPEM, keyPEM); err != nil {
			http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(certs.Info())
	}
}

func readFormFile(r *http.Request, field string) ([]byte, error) {
	f, _, err := r.FormFile(field)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, 1<<20))
}
