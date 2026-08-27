package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

func handleUSBList(w http.ResponseWriter, r *http.Request, _ string) {
	drives, err := ListUSBDrives()
	if err != nil {
		http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(drives)
}

// path pattern: /api/storage/usb/{device}/mount or /unmount
func handleUSBMountAction(w http.ResponseWriter, r *http.Request, _ string) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/storage/usb/")
	parts := strings.Split(rest, "/")
	if len(parts) != 2 {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	device, action := parts[0], parts[1]

	var err error
	var mountPoint string
	switch action {
	case "mount":
		mountPoint, err = MountUSBDrive(device)
	case "unmount":
		err = UnmountUSBDrive(device)
	default:
		http.Error(w, `{"error":"unknown action"}`, http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"mountPoint": mountPoint})
}

func handleFilesList(w http.ResponseWriter, r *http.Request, _ string) {
	device := r.URL.Query().Get("device")
	path := r.URL.Query().Get("path")
	entries, err := listDirectory(device, path)
	if err != nil {
		http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entries)
}

func handleFileDownload(w http.ResponseWriter, r *http.Request, _ string) {
	device := r.URL.Query().Get("device")
	path := r.URL.Query().Get("path")
	resolved, err := resolveSafePath(device, path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.ServeFile(w, r, resolved)
}
