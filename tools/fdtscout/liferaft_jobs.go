package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

// LifeRaftProtocol is deliberately a closed set -- validated against this exact list at save
// time, never passed through to a shell command unchecked.
type LifeRaftProtocol string

const (
	ProtocolSMB  LifeRaftProtocol = "smb"
	ProtocolFTP  LifeRaftProtocol = "ftp"
	ProtocolFTPS LifeRaftProtocol = "ftps"
)

// allowedRetentionDays is the exact seven-option set agreed on with the user -- a dropdown, not a
// free-typed number, so a job can never end up with a retention window nobody actually chose.
var allowedRetentionDays = []int{1, 3, 7, 14, 30, 60, 90}

func isAllowedRetention(days int) bool {
	for _, d := range allowedRetentionDays {
		if d == days {
			return true
		}
	}
	return false
}

// LifeRaftJob is a job's OWN config -- protocol, host, share/path, schedule, retention -- and a
// CredentialID reference, never inline credentials. Jobs are general app state, not a credential
// themselves, so they live under DataDir, not /etc (config.go's own documented split: /etc for
// things that hold a credential directly, DataDir for everything else).
type LifeRaftJob struct {
	ID            string           `json:"id"`
	Label         string           `json:"label"`
	Protocol      LifeRaftProtocol `json:"protocol"`
	Host          string           `json:"host"`
	Port          int              `json:"port"`
	Share         string           `json:"share,omitempty"` // SMB share name -- ignored for FTP/FTPS
	Path          string           `json:"path"`            // subpath within the share (SMB) or root path (FTP) -- "/" default
	CredentialID  string           `json:"credentialId"`
	ScheduleCron  string           `json:"scheduleCron"` // standard 5-field cron expression
	RetentionDays int              `json:"retentionDays"`
	Enabled       bool             `json:"enabled"`
	CreatedAt     time.Time        `json:"createdAt"`
}

const lifeRaftJobsFile = DataDir + "/liferaft-jobs.json"

// lifeRaftCronTag marks every crontab line LifeRaft itself manages, in its own namespace separate
// from the user-facing Scheduled Tasks tab's `# fdtscout:<id>:<name>` lines -- a LifeRaft job's
// schedule isn't meant to be hand-edited or deleted from that general tab (doing so would silently
// desync it from the job's own retention/versioning state), so it gets a tag that tab's own
// taskLineRe pattern in scheduledtasks.go doesn't match.
var lifeRaftCronLineRe = regexp.MustCompile(`^(.*)\s+# fdtscout-liferaft:(\S+)$`)

func loadLifeRaftJobsRaw() ([]LifeRaftJob, error) {
	data, err := os.ReadFile(lifeRaftJobsFile)
	if err != nil {
		if os.IsNotExist(err) {
			return []LifeRaftJob{}, nil
		}
		return nil, err
	}
	var jobs []LifeRaftJob
	if err := json.Unmarshal(data, &jobs); err != nil {
		return nil, fmt.Errorf("couldn't parse %s: %w", lifeRaftJobsFile, err)
	}
	return jobs, nil
}

func saveLifeRaftJobsRaw(jobs []LifeRaftJob) error {
	if err := os.MkdirAll(DataDir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(jobs, "", "  ")
	if err != nil {
		return err
	}
	tmp := lifeRaftJobsFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, lifeRaftJobsFile)
}

func ListLifeRaftJobs() ([]LifeRaftJob, error) {
	return loadLifeRaftJobsRaw()
}

// lifeRaftJobsUsingCredential is the in-use check DeleteLifeRaftCredential's own doc comment says
// is required before that route can safely exist -- now it can.
func lifeRaftJobsUsingCredential(credentialID string) []string {
	jobs, err := loadLifeRaftJobsRaw()
	if err != nil {
		return nil
	}
	var labels []string
	for _, j := range jobs {
		if j.CredentialID == credentialID {
			labels = append(labels, j.Label)
		}
	}
	return labels
}

// SaveLifeRaftJob creates (ID == "") or updates a job, validates it fully against real
// constraints (not just "is it non-empty"), and re-syncs this one job's own crontab line to match
// -- every save keeps the schedule in lockstep with the job, rather than needing a separate manual
// "apply schedule" step that could drift from what's actually stored.
func SaveLifeRaftJob(incoming LifeRaftJob) (LifeRaftJob, error) {
	incoming.Label = strings.TrimSpace(incoming.Label)
	incoming.Host = strings.TrimSpace(incoming.Host)
	incoming.Share = strings.TrimSpace(incoming.Share)
	incoming.Path = strings.TrimSpace(incoming.Path)
	incoming.CredentialID = strings.TrimSpace(incoming.CredentialID)
	incoming.ScheduleCron = strings.TrimSpace(incoming.ScheduleCron)

	if incoming.Label == "" || incoming.Host == "" {
		return LifeRaftJob{}, fmt.Errorf("label and host are required")
	}
	switch incoming.Protocol {
	case ProtocolSMB:
		if incoming.Share == "" {
			return LifeRaftJob{}, fmt.Errorf("share name is required for SMB")
		}
		if incoming.Port == 0 {
			incoming.Port = 445
		}
	case ProtocolFTP, ProtocolFTPS:
		if incoming.Port == 0 {
			incoming.Port = 21
		}
	default:
		return LifeRaftJob{}, fmt.Errorf("protocol must be smb, ftp, or ftps")
	}
	if incoming.Path == "" {
		incoming.Path = "/"
	}
	if !isAllowedRetention(incoming.RetentionDays) {
		return LifeRaftJob{}, fmt.Errorf("retention must be one of: %v days", allowedRetentionDays)
	}
	if len(strings.Fields(incoming.ScheduleCron)) != 5 {
		return LifeRaftJob{}, fmt.Errorf("schedule must be a standard 5-field cron expression")
	}
	creds, err := loadLifeRaftCredentialsRaw()
	if err != nil {
		return LifeRaftJob{}, err
	}
	credFound := false
	for _, c := range creds {
		if c.ID == incoming.CredentialID {
			credFound = true
			break
		}
	}
	if !credFound {
		return LifeRaftJob{}, fmt.Errorf("no such credential: %s", incoming.CredentialID)
	}

	jobs, err := loadLifeRaftJobsRaw()
	if err != nil {
		return LifeRaftJob{}, err
	}

	if incoming.ID == "" {
		incoming.ID = randomLifeRaftID("job")
		incoming.CreatedAt = time.Now().UTC()
		jobs = append(jobs, incoming)
	} else {
		found := false
		for i, existing := range jobs {
			if existing.ID == incoming.ID {
				incoming.CreatedAt = existing.CreatedAt
				jobs[i] = incoming
				found = true
				break
			}
		}
		if !found {
			return LifeRaftJob{}, fmt.Errorf("no such job: %s", incoming.ID)
		}
	}

	if err := saveLifeRaftJobsRaw(jobs); err != nil {
		return LifeRaftJob{}, err
	}
	if err := syncLifeRaftCrontab(jobs); err != nil {
		// The job itself saved correctly -- a crontab sync failure shouldn't be reported as if the
		// save failed outright, but it does mean this job won't actually run on schedule until
		// fixed, so surface it rather than silently losing it.
		return incoming, fmt.Errorf("job saved, but updating its schedule failed: %w", err)
	}
	return incoming, nil
}

func DeleteLifeRaftJob(id string) error {
	jobs, err := loadLifeRaftJobsRaw()
	if err != nil {
		return err
	}
	out := jobs[:0]
	found := false
	for _, j := range jobs {
		if j.ID == id {
			found = true
			continue
		}
		out = append(out, j)
	}
	if !found {
		return fmt.Errorf("no such job: %s", id)
	}
	if err := saveLifeRaftJobsRaw(out); err != nil {
		return err
	}
	return syncLifeRaftCrontab(out)
}

// syncLifeRaftCrontab rewrites every fdtscout-liferaft:-tagged line to match the current job list
// exactly, leaving every other line in root's crontab (including the user-facing Scheduled Tasks
// tab's own lines) completely untouched -- same "replace only our own tagged lines" discipline
// SaveScheduledTasks already uses, just a separate tag namespace.
func syncLifeRaftCrontab(jobs []LifeRaftJob) error {
	content, err := loadCrontab()
	if err != nil {
		return err
	}
	var kept []string
	for _, line := range strings.Split(content, "\n") {
		if line == "" || lifeRaftCronLineRe.MatchString(line) {
			continue
		}
		kept = append(kept, line)
	}
	selfPath, err := os.Executable()
	if err != nil {
		selfPath = "/opt/fdtscout/fdtscout"
	}
	for _, j := range jobs {
		if !j.Enabled {
			continue
		}
		kept = append(kept, fmt.Sprintf("%s %s -liferaft-run %s # fdtscout-liferaft:%s", j.ScheduleCron, selfPath, j.ID, j.ID))
	}
	return saveCrontab(strings.Join(kept, "\n") + "\n")
}

// runLifeRaftScheduledJob is the entrypoint main() calls when invoked as
// `fdtscout -liferaft-run <job-id>` from cron. It never runs concurrently with another LifeRaft
// run -- see acquireLifeRaftRunLock in liferaft_sync.go -- which is what actually satisfies "queue
// jobs one at a time, never overwhelm a source or trigger a lockout" rather than the cron schedule
// itself trying to prevent overlap.
func runLifeRaftScheduledJob(jobID string) {
	jobs, err := loadLifeRaftJobsRaw()
	if err != nil {
		fmt.Fprintf(os.Stderr, "liferaft: couldn't load jobs: %v\n", err)
		os.Exit(1)
	}
	var job *LifeRaftJob
	for i := range jobs {
		if jobs[i].ID == jobID {
			job = &jobs[i]
			break
		}
	}
	if job == nil {
		fmt.Fprintf(os.Stderr, "liferaft: no such job: %s\n", jobID)
		os.Exit(1)
	}
	if !job.Enabled {
		fmt.Println("liferaft: job is disabled, skipping")
		return
	}

	unlock, err := acquireLifeRaftRunLock()
	if err != nil {
		fmt.Fprintf(os.Stderr, "liferaft: couldn't acquire run lock: %v\n", err)
		os.Exit(1)
	}
	defer unlock()

	result := RunLifeRaftJob(*job)
	if result.Err != nil {
		fmt.Fprintf(os.Stderr, "liferaft: job %s failed: %v\n", job.Label, result.Err)
		os.Exit(1)
	}
	fmt.Printf("liferaft: job %s finished: +%d changed %d deleted %d\n", job.Label, result.FilesAdded, result.FilesChanged, result.FilesDeleted)
}
