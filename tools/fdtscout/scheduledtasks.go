package main

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Scheduled tasks: manages real root crontab entries rather than reimplementing a scheduler --
// simpler, more robust, matches this codebase's existing "detect/reuse the real mechanism"
// discipline (timedatectl for NTP, resolvectl for DNS, etc.). Each entry FDT.Scout creates is
// tagged with a `# fdtscout:<id>` trailing comment so it can find/edit/remove just its own lines
// without disturbing anything already in root's crontab from elsewhere.
//
// Deliberately NOT the same trust boundary concern as the Pushbullet command channel's action
// allow-list: this UI lives entirely inside the already-authenticated FDT.Scout session, which can
// already run any command at all via the real terminal tab -- a scheduled arbitrary command here
// isn't a NEW capability, just a convenience over "ssh in and add a crontab line by hand."

type ScheduledTask struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Schedule string `json:"schedule"` // standard 5-field cron expression, e.g. "0 3 * * *"
	Command  string `json:"command"`
}

var taskLineRe = regexp.MustCompile(`^(.*)\s+# fdtscout:(\S+):(.*)$`)

func loadCrontab() (string, error) {
	out, err := exec.Command("crontab", "-l").CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "no crontab") {
			return "", nil // no crontab yet -- not an error, just an empty one
		}
		return "", fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}
	return string(out), nil
}

func saveCrontab(content string) error {
	cmd := exec.Command("crontab", "-")
	cmd.Stdin = strings.NewReader(content)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func ListScheduledTasks() ([]ScheduledTask, error) {
	content, err := loadCrontab()
	if err != nil {
		return nil, err
	}
	var tasks []ScheduledTask
	for _, line := range strings.Split(content, "\n") {
		m := taskLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		fields := strings.SplitN(strings.TrimSpace(m[1]), " ", 6)
		if len(fields) < 6 {
			continue
		}
		schedule := strings.Join(fields[:5], " ")
		tasks = append(tasks, ScheduledTask{ID: m[2], Name: m[3], Schedule: schedule, Command: fields[5]})
	}
	return tasks, nil
}

// SaveScheduledTasks replaces the FULL set of fdtscout-managed lines with the given list, leaving
// every other line in root's crontab (anything not tagged fdtscout:) completely untouched.
func SaveScheduledTasks(tasks []ScheduledTask) error {
	content, err := loadCrontab()
	if err != nil {
		return err
	}

	var kept []string
	for _, line := range strings.Split(content, "\n") {
		if line == "" {
			continue
		}
		if taskLineRe.MatchString(line) {
			continue // drop all existing fdtscout-managed lines -- rebuilt below from the given list
		}
		kept = append(kept, line)
	}

	for i, t := range tasks {
		if t.ID == "" {
			t.ID = fmt.Sprintf("task-%d-%d", time.Now().UnixNano(), i)
		}
		if strings.TrimSpace(t.Schedule) == "" || strings.TrimSpace(t.Command) == "" {
			continue
		}
		kept = append(kept, fmt.Sprintf("%s %s # fdtscout:%s:%s", t.Schedule, t.Command, t.ID, t.Name))
	}

	return saveCrontab(strings.Join(kept, "\n") + "\n")
}
