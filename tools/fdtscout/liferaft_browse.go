package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// validLifeRaftJobID guards jobID before it's ever used to build a filesystem path (jobDataDir
// does a plain filepath.Join, which does NOT stop ".." from escaping lifeRaftDataRoot on its own)
// -- matches exactly randomLifeRaftID("job")'s own output shape, "job-" plus lowercase hex.
// Checked BEFORE the os.Stat existence check below, not instead of it, since a nonexistent-but-
// otherwise-valid-shaped ID still needs to fail cleanly with "no such job."
var validLifeRaftJobID = regexp.MustCompile(`^job-[0-9a-f]+$`)

// LifeRaft's own file browser -- read-only, download only, no upload/delete/rename, same
// deliberate scope limit files.go's own USB browser already documents for itself. This is
// explicitly the answer to "no restore-to-source function, build a file browser so the user can
// manage downloading instead": browsing/downloading the CURRENT mirror only. Restoring a specific
// older version is a v2 feature (list versions of a path, download one) layered on the same
// resolveLifeRaftSafePath chokepoint -- not built in this pass, since the user's request was
// specifically "no restore function... build in a file browser."

// resolveLifeRaftSafePath is the same real boundary check files.go's resolveSafePath already
// proved out: filepath.Clean + Join collapses ".." segments, and the final resolved path is then
// independently confirmed to still live under the job's own current/ directory -- string-checking
// the input alone was never the actual guarantee.
func resolveLifeRaftSafePath(jobID, requestedPath string) (string, error) {
	if !validLifeRaftJobID.MatchString(jobID) {
		return "", fmt.Errorf("invalid job id")
	}
	root := jobCurrentDir(jobID)
	if _, err := os.Stat(root); err != nil {
		return "", fmt.Errorf("no such job, or it hasn't run yet")
	}

	cleaned := filepath.Clean("/" + strings.TrimPrefix(requestedPath, "/"))
	resolved := filepath.Join(root, cleaned)
	if resolved != root && !strings.HasPrefix(resolved, root+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes this job's storage")
	}
	return resolved, nil
}

func listLifeRaftDirectory(jobID, requestedPath string) ([]FileEntry, error) {
	resolved, err := resolveLifeRaftSafePath(jobID, requestedPath)
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
			continue
		}
		out = append(out, FileEntry{Name: e.Name(), IsDir: e.IsDir(), Size: info.Size(), ModTime: info.ModTime()})
	}
	return out, nil
}
