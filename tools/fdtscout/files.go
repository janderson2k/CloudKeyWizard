package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The file browser is deliberately scoped to ONLY currently-mounted USB drives under
// usbMountRoot -- never an arbitrary filesystem path. resolveSafePath is the one chokepoint every
// browse/download request goes through; it re-checks the device is actually mounted right now
// (not just that the name looks plausible) and rejects any path that resolves outside that
// specific mount point, which is what actually stops a ".." (or an absolute-path override) from
// escaping to the rest of the filesystem -- string-checking the input alone isn't reliable,
// filepath.Clean + a prefix check on the final resolved path is.
//
// Read-only by design for this first pass (browse + download only, no upload/delete/rename) --
// the user's request didn't specify write access, and read-only is the safer default until asked
// for more.

type FileEntry struct {
	Name    string    `json:"name"`
	IsDir   bool      `json:"isDir"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
}

func resolveSafePath(device, requestedPath string) (string, error) {
	if !validDeviceName.MatchString(device) {
		return "", fmt.Errorf("invalid device name")
	}
	drives, err := ListUSBDrives()
	if err != nil {
		return "", err
	}
	var mountPoint string
	for _, d := range drives {
		if d.Device == device && d.Mounted {
			mountPoint = d.MountPoint
			break
		}
	}
	if mountPoint == "" {
		return "", fmt.Errorf("'%s' isn't currently mounted", device)
	}

	cleaned := filepath.Clean("/" + strings.TrimPrefix(requestedPath, "/"))
	resolved := filepath.Join(mountPoint, cleaned)

	// The real boundary check: after Clean+Join, the result must still live under mountPoint.
	// filepath.Join already collapses ".." segments, but this confirms the outcome rather than
	// trusting that alone -- belt and suspenders on a security boundary.
	if resolved != mountPoint && !strings.HasPrefix(resolved, mountPoint+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes the mounted drive")
	}
	return resolved, nil
}

func listDirectory(device, requestedPath string) ([]FileEntry, error) {
	resolved, err := resolveSafePath(device, requestedPath)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return nil, fmt.Errorf("reading directory: %w", err)
	}
	out := make([]FileEntry, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue // a file that vanished/errored between ReadDir and Info -- skip, don't fail the whole listing
		}
		out = append(out, FileEntry{Name: e.Name(), IsDir: e.IsDir(), Size: info.Size(), ModTime: info.ModTime()})
	}
	return out, nil
}
