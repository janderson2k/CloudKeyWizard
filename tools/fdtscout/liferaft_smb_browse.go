package main

import (
	"fmt"
	"strings"
	"time"
)

// SMB browsing for the job-creation form: pick a share, then click through its directory tree,
// instead of typing host/share/path blind. This is a config-time "audition" action (the user
// hasn't saved a job yet, or is editing one) -- each call connects fresh and disconnects when
// done, no session held open across requests. Read-only, same as everything else LifeRaft does:
// this only ever calls Session.ListSharenames/Share.ReadDir, never anything that could write.
// FTP browsing isn't built here -- not asked for, and this same pattern extends to it later if
// wanted.

// ListSMBShares connects with the given saved credential and lists share names -- the first step
// of browsing, before picking a path within one of them. Windows admin/hidden shares (C$, ADMIN$,
// IPC$, ...) are filtered out -- never a useful backup target, just clutter in the picker.
func ListSMBShares(host string, port int, credentialID string) ([]string, error) {
	username, domain, password, err := DecryptedLifeRaftCredential(credentialID)
	if err != nil {
		return nil, fmt.Errorf("credential: %w", err)
	}
	conn, session, err := connectSMBSession(host, port, username, domain, password)
	if err != nil {
		return nil, err
	}
	defer session.Logoff()
	defer conn.Close()

	names, err := session.ListSharenames()
	if err != nil {
		return nil, fmt.Errorf("listing shares: %w", err)
	}
	out := make([]string, 0, len(names))
	for _, n := range names {
		if strings.HasSuffix(n, "$") {
			continue
		}
		out = append(out, n)
	}
	return out, nil
}

// BrowseSMBEntry is one file/dir found while browsing -- distinct from sourceEntry (sync-engine-
// internal, files only): this includes directories too, since browsing is about picking a path to
// back up, not enumerating files to copy.
type BrowseSMBEntry struct {
	Name    string    `json:"name"`
	IsDir   bool      `json:"isDir"`
	Size    int64     `json:"size,omitempty"`
	ModTime time.Time `json:"modTime,omitempty"`
}

// ListSMBPath lists the immediate contents of one directory within one share.
func ListSMBPath(host string, port int, credentialID, share, dirPath string) ([]BrowseSMBEntry, error) {
	username, domain, password, err := DecryptedLifeRaftCredential(credentialID)
	if err != nil {
		return nil, fmt.Errorf("credential: %w", err)
	}
	conn, session, err := connectSMBSession(host, port, username, domain, password)
	if err != nil {
		return nil, err
	}
	defer session.Logoff()
	defer conn.Close()

	shareConn, err := session.Mount(share)
	if err != nil {
		return nil, fmt.Errorf("mounting share %q: %w", share, err)
	}
	defer shareConn.Umount()

	entries, err := shareConn.ReadDir(smbJoin(dirPath))
	if err != nil {
		return nil, fmt.Errorf("listing %q: %w", dirPath, err)
	}
	out := make([]BrowseSMBEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, BrowseSMBEntry{Name: e.Name(), IsDir: e.IsDir(), Size: e.Size(), ModTime: e.ModTime()})
	}
	return out, nil
}
