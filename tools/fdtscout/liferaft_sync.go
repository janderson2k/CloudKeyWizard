package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// The sync engine: connects via the read-only sourceReader (liferaft_source.go), recursively
// lists the source, diffs against the last-known manifest, and applies the deletion-protection
// model agreed on with the user -- new/changed files land in current/, anything a change or a
// remote deletion would otherwise destroy is moved into versions/ first, never overwritten in
// place. Change detection is size+mtime only (confirmed with the user, not a hash of every file
// every run -- not realistic on this hardware against a real SMB/FTP share). The live current/
// mirror itself is never pruned; only versions/ is subject to the job's own retention window.

func jobDataDir(jobID string) string      { return filepath.Join(lifeRaftDataRoot, jobID) }
func jobCurrentDir(jobID string) string   { return filepath.Join(jobDataDir(jobID), "current") }
func jobVersionsDir(jobID string) string  { return filepath.Join(jobDataDir(jobID), "versions") }
func jobManifestPath(jobID string) string { return filepath.Join(jobDataDir(jobID), ".manifest.json") }
func jobRunsPath(jobID string) string     { return filepath.Join(jobDataDir(jobID), ".runs.json") }

// manifestEntry is what's persisted between runs to detect changes without re-listing every byte
// of every file's content -- exactly the size+mtime comparison already agreed on.
type manifestEntry struct {
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
}
type manifest map[string]manifestEntry // key: forward-slash relative path

func loadManifest(jobID string) manifest {
	data, err := os.ReadFile(jobManifestPath(jobID))
	if err != nil {
		return manifest{}
	}
	var m manifest
	if json.Unmarshal(data, &m) != nil {
		return manifest{}
	}
	return m
}

func saveManifest(jobID string, m manifest) error {
	if err := os.MkdirAll(jobDataDir(jobID), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	tmp := jobManifestPath(jobID) + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, jobManifestPath(jobID))
}

// LifeRaftRunResult is both the live return value of RunLifeRaftJob and, unmarshaled the same way,
// one entry in a job's persisted run history. ID is what makes this record findable and updatable
// in place -- RunLifeRaftJob writes a "running" placeholder under this ID the instant it starts
// (see the real bug this closes, below), then replaces the same record with the final result.
type LifeRaftRunResult struct {
	ID               string    `json:"id"`
	JobID            string    `json:"jobId"`
	StartedAt        time.Time `json:"startedAt"`
	FinishedAt       time.Time `json:"finishedAt"`
	Status           string    `json:"status"` // "running" | "ok" | "failed" | "partial"
	FilesAdded       int       `json:"filesAdded"`
	FilesChanged     int       `json:"filesChanged"`
	FilesDeleted     int       `json:"filesDeleted"`
	BytesTransferred int64     `json:"bytesTransferred"`
	VersionsPruned   int       `json:"versionsPruned"`
	Errors           []string  `json:"errors,omitempty"`
	Err              error     `json:"-"` // the fatal (connection-level) error, if any -- per-file errors go in Errors instead
}

const maxLifeRaftRunHistory = 200

// upsertLifeRaftRun replaces the existing record with this ID, or appends a new one. Real bug this
// closes: a run used to be recorded only once, at the very end, via a single defer -- if the whole
// process died mid-run (a panic in a goroutine this codebase doesn't otherwise recover, an external
// kill, a service restart) that defer never ran, and the run vanished with literally no trace: not
// failed, not still running, just gone, indistinguishable from never having started. RunLifeRaftJob
// now writes a "running" placeholder under a stable ID the instant it starts and upserts the same
// record when it finishes, so even a hard process death leaves a durable, visible "stuck running"
// entry in history instead of silence.
func upsertLifeRaftRun(result LifeRaftRunResult) {
	var runs []LifeRaftRunResult
	if data, err := os.ReadFile(jobRunsPath(result.JobID)); err == nil {
		_ = json.Unmarshal(data, &runs)
	}
	replaced := false
	for i := range runs {
		if runs[i].ID == result.ID {
			runs[i] = result
			replaced = true
			break
		}
	}
	if !replaced {
		runs = append(runs, result)
	}
	if len(runs) > maxLifeRaftRunHistory {
		runs = runs[len(runs)-maxLifeRaftRunHistory:]
	}
	if data, err := json.Marshal(runs); err == nil {
		tmp := jobRunsPath(result.JobID) + ".tmp"
		if os.WriteFile(tmp, data, 0644) == nil {
			os.Rename(tmp, jobRunsPath(result.JobID))
		}
	}
}

// liferaftLiveProgress is what a running job reports about itself right now -- in-memory only,
// never persisted, rebuilt fresh every run. FilesTotal/FilesProcessed come from the source listing
// completing before the diff loop starts, so the resulting fraction is a real "how far through the
// source tree" count, not a guessed or fabricated percentage.
type liferaftLiveProgress struct {
	StartedAt        time.Time `json:"startedAt"`
	FilesTotal       int       `json:"filesTotal"`
	FilesProcessed   int       `json:"filesProcessed"`
	FilesAdded       int       `json:"filesAdded"`
	FilesChanged     int       `json:"filesChanged"`
	FilesDeleted     int       `json:"filesDeleted"`
	BytesTransferred int64     `json:"bytesTransferred"`
}

// liferaftRunningSet tracks which jobs are mid-run right now, and their live progress -- in-memory
// only, never persisted, so it can never go stale across a restart the way a disk-backed flag
// could. The run lock (acquireLifeRaftRunLock) already guarantees only one job runs at a time
// across the whole device; this is purely a UI-visibility mechanism, not a second concurrency guard.
var (
	liferaftRunningMu  sync.Mutex
	liferaftRunningSet = map[string]*liferaftLiveProgress{}
)

func startLifeRaftJobProgress(jobID string, startedAt time.Time) {
	liferaftRunningMu.Lock()
	defer liferaftRunningMu.Unlock()
	liferaftRunningSet[jobID] = &liferaftLiveProgress{StartedAt: startedAt}
}

func clearLifeRaftJobProgress(jobID string) {
	liferaftRunningMu.Lock()
	defer liferaftRunningMu.Unlock()
	delete(liferaftRunningSet, jobID)
}

func setLifeRaftFilesTotal(jobID string, total int) {
	liferaftRunningMu.Lock()
	defer liferaftRunningMu.Unlock()
	if p, ok := liferaftRunningSet[jobID]; ok {
		p.FilesTotal = total
	}
}

func updateLifeRaftLiveProgress(jobID string, processed, added, changed, deleted int, bytes int64) {
	liferaftRunningMu.Lock()
	defer liferaftRunningMu.Unlock()
	if p, ok := liferaftRunningSet[jobID]; ok {
		p.FilesProcessed = processed
		p.FilesAdded = added
		p.FilesChanged = changed
		p.FilesDeleted = deleted
		p.BytesTransferred = bytes
	}
}

// LifeRaftLiveProgress returns a snapshot of a running job's progress, or nil if it isn't running.
func LifeRaftLiveProgress(jobID string) *liferaftLiveProgress {
	liferaftRunningMu.Lock()
	defer liferaftRunningMu.Unlock()
	p, ok := liferaftRunningSet[jobID]
	if !ok {
		return nil
	}
	snapshot := *p
	return &snapshot
}

func IsLifeRaftJobRunning(jobID string) bool {
	liferaftRunningMu.Lock()
	defer liferaftRunningMu.Unlock()
	_, ok := liferaftRunningSet[jobID]
	return ok
}

func ListLifeRaftRuns(jobID string) []LifeRaftRunResult {
	var runs []LifeRaftRunResult
	if data, err := os.ReadFile(jobRunsPath(jobID)); err == nil {
		_ = json.Unmarshal(data, &runs)
	}
	// Newest first -- same convention AuthLog.History already uses for its own attempt list.
	for i, j := 0, len(runs)-1; i < j; i, j = i+1, j-1 {
		runs[i], runs[j] = runs[j], runs[i]
	}
	return runs
}

// RunLifeRaftJob is the whole sync pass for one job: connect, recursively list, diff against the
// manifest, apply changes with deletion-protection, save the new manifest, prune expired
// versions, and record the run. Always records a run, even a hard failure, so the run history is
// never silently missing an attempt that actually happened.
func RunLifeRaftJob(job LifeRaftJob) LifeRaftRunResult {
	startedAt := time.Now().UTC()
	startLifeRaftJobProgress(job.ID, startedAt)
	defer clearLifeRaftJobProgress(job.ID) // registered first -> runs last, after the run is recorded below

	result := LifeRaftRunResult{ID: randomLifeRaftID("run"), JobID: job.ID, StartedAt: startedAt, Status: "running"}
	upsertLifeRaftRun(result) // durable placeholder -- see upsertLifeRaftRun's own doc comment for why

	defer func() {
		// Catches any panic from this run's own call tree (SMB/FTP protocol code this codebase
		// doesn't control, exercised here for the first time against real-world data) and turns it
		// into a normal, visible "failed" run with the actual panic text -- instead of an unrecovered
		// panic silently killing the whole FDT.Scout process (a goroutine spawned by an HTTP handler,
		// unlike the handler itself, gets no automatic recovery from net/http).
		if p := recover(); p != nil {
			result.Err = fmt.Errorf("internal error: %v", p)
		}
		result.FinishedAt = time.Now().UTC()
		if result.Err != nil {
			result.Status = "failed"
			result.Errors = append(result.Errors, result.Err.Error())
		} else if len(result.Errors) > 0 {
			result.Status = "partial"
		} else {
			result.Status = "ok"
		}
		upsertLifeRaftRun(result)
		if job.NotifyPushbullet && result.Status != "ok" {
			notifyLifeRaftJobProblem(job.Label, result.Status, result.Errors)
		}
	}()

	// Refuse to run if /volume isn't a REAL mount right now -- writing into a directory a failed
	// mount left behind would silently land backup data on the small root partition, exactly the
	// storage-migration hazard already worked through for FDT.Core's own /api/health design.
	if !isRealMountpoint("/volume") {
		result.Err = fmt.Errorf("/volume is not mounted -- refusing to run rather than write to the root partition")
		return result
	}

	// A near-full disk used to just fail every single file with the same "no space left on device"
	// error, one Errors entry per file, burying the actual problem in noise. Refuse cleanly upfront
	// instead -- 0.5 GB is well below anything a real transfer could usefully make progress with.
	const minFreeGBToRun = 0.5
	if _, freeGB := diskTotalAndFreeGB("/volume"); freeGB < minFreeGBToRun {
		result.Err = fmt.Errorf("/volume has only %.2f GB free -- refusing to start a run below %.1f GB free", freeGB, minFreeGBToRun)
		return result
	}

	cleanupStrayLifeRaftTempFiles(job.ID)

	src, err := connectLifeRaftSource(job)
	if err != nil {
		result.Err = fmt.Errorf("connecting to source: %w", err)
		return result
	}
	defer src.Close()

	remote := map[string]sourceEntry{}
	if err := walkSource(src, "", remote); err != nil {
		result.Err = fmt.Errorf("listing source: %w", err)
		return result
	}

	oldManifest := loadManifest(job.ID)
	newManifest := manifest{}

	setLifeRaftFilesTotal(job.ID, len(remote))
	processed := 0
	reportProgress := func() {
		processed++
		updateLifeRaftLiveProgress(job.ID, processed, result.FilesAdded, result.FilesChanged, result.FilesDeleted, result.BytesTransferred)
	}

	for relPath, entry := range remote {
		old, existed := oldManifest[relPath]
		unchanged := existed && old.Size == entry.Size && old.ModTime.Equal(entry.ModTime)
		if unchanged {
			newManifest[relPath] = old
			reportProgress()
			continue
		}

		if existed {
			// Changed -- protect the outgoing local copy before it's overwritten.
			if err := versionExistingFile(job.ID, relPath, old.ModTime, false); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("%s: couldn't version previous copy: %v", relPath, err))
				reportProgress()
				continue
			}
		}
		n, err := copyFromSource(src, job.ID, relPath)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", relPath, err))
			if existed {
				newManifest[relPath] = old // keep the old manifest entry -- the copy failed, current/ may now be missing this file, but don't claim we have a fresh one
			}
			reportProgress()
			continue
		}
		newManifest[relPath] = manifestEntry{Size: entry.Size, ModTime: entry.ModTime}
		result.BytesTransferred += n
		if existed {
			result.FilesChanged++
		} else {
			result.FilesAdded++
		}
		reportProgress()
	}

	for relPath, old := range oldManifest {
		if _, stillPresent := remote[relPath]; stillPresent {
			continue
		}
		if err := versionExistingFile(job.ID, relPath, old.ModTime, true); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: couldn't protect deleted file: %v", relPath, err))
			newManifest[relPath] = old // couldn't safely remove it -- keep tracking it rather than losing it from the manifest
			continue
		}
		result.FilesDeleted++
		updateLifeRaftLiveProgress(job.ID, processed, result.FilesAdded, result.FilesChanged, result.FilesDeleted, result.BytesTransferred)
	}

	if err := saveManifest(job.ID, newManifest); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("saving manifest: %v", err))
	}

	pruned, _ := pruneLifeRaftVersions(job.ID, job.RetentionDays)
	result.VersionsPruned = pruned

	return result
}

// cleanupStrayLifeRaftTempFiles removes any leftover *.liferaft-tmp file under current/. These can
// only exist if a previous run's copyFromSource was interrupted (a crash, a kill) before its own
// rename-into-place ever ran -- a successful copy always renames its temp file away immediately, so
// anything still named *.liferaft-tmp is guaranteed orphaned, never a file the mirror depends on.
// Left alone across repeated interruptions, these would accumulate indefinitely and quietly eat
// into this device's own limited storage.
func cleanupStrayLifeRaftTempFiles(jobID string) {
	root := jobCurrentDir(jobID)
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.HasSuffix(d.Name(), ".liferaft-tmp") {
			os.Remove(path)
		}
		return nil
	})
}

// walkSource recursively lists a source into a flat map, depth-first, using only the narrow
// sourceReader interface -- the same List call shape works identically for SMB and FTP, so the
// recursion logic itself never needs to know which protocol it's talking to.
func walkSource(src sourceReader, dir string, out map[string]sourceEntry) error {
	files, subdirs, err := src.List(dir)
	if err != nil {
		return fmt.Errorf("listing %q: %w", dir, err)
	}
	for _, f := range files {
		out[f.Path] = f
	}
	for _, sub := range subdirs {
		if err := walkSource(src, sub, out); err != nil {
			return err // a source-side permission error deep in the tree aborts the whole run rather than silently under-backing-up
		}
	}
	return nil
}

// copyFromSource streams one file from the source into current/<relPath>, via a temp file renamed
// into place -- so an interrupted transfer never leaves a half-written file passing as a good copy.
func copyFromSource(src sourceReader, jobID, relPath string) (int64, error) {
	dest := filepath.Join(jobCurrentDir(jobID), filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
		return 0, err
	}
	r, err := src.Open(relPath)
	if err != nil {
		return 0, err
	}
	defer r.Close()

	tmp := dest + ".liferaft-tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return 0, err
	}
	n, copyErr := io.Copy(f, r)
	closeErr := f.Close()
	if copyErr != nil {
		os.Remove(tmp)
		return 0, copyErr
	}
	if closeErr != nil {
		os.Remove(tmp)
		return 0, closeErr
	}
	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		return 0, err
	}
	return n, nil
}

// versionExistingFile moves the file CURRENTLY at current/<relPath> into
// versions/<relPath>/<timestamp> (or deleted-<timestamp> for a source-side deletion) instead of
// deleting or overwriting it -- this IS the deletion-protection feature. A missing source file
// under current/ (first run, or a previous copy that itself failed) is not an error -- there's
// simply nothing to protect.
func versionExistingFile(jobID, relPath string, refTime time.Time, deleted bool) error {
	src := filepath.Join(jobCurrentDir(jobID), filepath.FromSlash(relPath))
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}

	label := refTime.UTC().Format("20060102T150405Z")
	if deleted {
		label = "deleted-" + label
	}
	destDir := filepath.Join(jobVersionsDir(jobID), filepath.FromSlash(relPath))
	if err := os.MkdirAll(destDir, 0700); err != nil {
		return err
	}
	dest := filepath.Join(destDir, label)
	// Extremely unlikely but possible (two changes landing on the same source mtime second): don't
	// silently clobber an existing version file, append a counter instead.
	for i := 1; ; i++ {
		if _, err := os.Stat(dest); os.IsNotExist(err) {
			break
		}
		dest = filepath.Join(destDir, fmt.Sprintf("%s-%d", label, i))
	}
	return os.Rename(src, dest)
}

// pruneLifeRaftVersions removes any protected version file older than retentionDays, based on the
// version file's OWN mtime (when it was captured), not the original source file's mtime --
// deliberately: the retention window is "how long ago did we capture this old copy," not
// "how old was the file when it changed." "Forever" for the live current/ mirror is enforced
// simply by this function only ever touching versions/, never current/.
func pruneLifeRaftVersions(jobID string, retentionDays int) (int, error) {
	if !isAllowedRetention(retentionDays) {
		return 0, fmt.Errorf("invalid retention: %d", retentionDays)
	}
	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	root := jobVersionsDir(jobID)
	removed := 0

	var walk func(dir string) error
	walk = func(dir string) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil // nothing to prune if versions/ doesn't exist yet
		}
		for _, e := range entries {
			full := filepath.Join(dir, e.Name())
			if e.IsDir() {
				_ = walk(full)
				// Clean up now-empty version directories so an old, fully-pruned file's folder
				// doesn't linger forever as empty clutter.
				if remaining, _ := os.ReadDir(full); len(remaining) == 0 {
					os.Remove(full)
				}
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			if info.ModTime().Before(cutoff) {
				if os.Remove(full) == nil {
					removed++
				}
			}
		}
		return nil
	}
	_ = walk(root)
	return removed, nil
}

// --- Global one-run-at-a-time queue -----------------------------------------------------------

const lifeRaftLockFile = DataDir + "/liferaft.lock"

// acquireLifeRaftRunLock blocks (does NOT fail fast) until it can take an exclusive lock on a
// fixed lock file -- this is what actually implements "queue jobs one at a time, never run two
// concurrently" for cron-triggered runs. Cron fires each job's own process independently; if two
// jobs' schedules coincide, the second process's flock call simply blocks here until the first
// one's defer releases it, so they run strictly one after another rather than in parallel or
// racing each other for the same source connection. Confirmed with the user this should be a
// single global queue, not per-source, since this hardware can't comfortably run more than one
// SMB/FTP transfer at a time alongside everything else already running on it.
func acquireLifeRaftRunLock() (unlock func(), err error) {
	if err := os.MkdirAll(DataDir, 0755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(lifeRaftLockFile, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, fmt.Errorf("flock: %w", err)
	}
	_ = f.Truncate(0)
	_, _ = f.WriteString(strconv.Itoa(os.Getpid()))
	return func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}

// RunLifeRaftJobNow is the "Run now" button's entrypoint -- same lock, same engine, just
// triggered from the API instead of cron. Runs synchronously; the HTTP handler is expected to
// call this in a goroutine and let the frontend poll run history rather than holding the request
// open for a potentially long transfer.
func RunLifeRaftJobNow(jobID string) error {
	jobs, err := loadLifeRaftJobsRaw()
	if err != nil {
		return err
	}
	for _, j := range jobs {
		if j.ID == jobID {
			unlock, err := acquireLifeRaftRunLock()
			if err != nil {
				return err
			}
			defer unlock()
			result := RunLifeRaftJob(j)
			return result.Err
		}
	}
	return fmt.Errorf("no such job: %s", jobID)
}
