package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// ProcessInfo is one row of a `top`-style process table. Sourced from `ps` (procps, a standard
// Debian package -- virtually guaranteed present) rather than hand-computing per-process CPU% by
// sampling /proc/<pid>/stat deltas ourselves: `ps`'s %CPU is the same well-understood, already-
// smoothed figure `top` itself shows, and reusing it avoids reimplementing that math a second time
// for a feature that's explicitly meant to feel like `top`.
type ProcessInfo struct {
	PID     int    `json:"pid"`
	User    string `json:"user"`
	CPUPct  float64 `json:"cpuPct"`
	MemPct  float64 `json:"memPct"`
	RSSKB   int64  `json:"rssKb"`
	Elapsed string `json:"elapsed"`
	Command string `json:"command"`
}

type SystemSummary struct {
	LoadAvg1     float64 `json:"loadAvg1"`
	LoadAvg5     float64 `json:"loadAvg5"`
	LoadAvg15    float64 `json:"loadAvg15"`
	UptimeSecs   float64 `json:"uptimeSecs"`
	ProcessCount int     `json:"processCount"`
	MemTotalMB   float64 `json:"memTotalMb"`
	MemUsedMB    float64 `json:"memUsedMb"`
	Processes    []ProcessInfo `json:"processes"`
}

// GetProcessSnapshot returns the top N processes by CPU, plus the same summary line `top` prints
// above its own table (load average, uptime, process count, memory).
func GetProcessSnapshot(limit int) SystemSummary {
	summary := SystemSummary{}
	summary.LoadAvg1, summary.LoadAvg5, summary.LoadAvg15 = readLoadAvg()
	summary.UptimeSecs = readUptime()
	summary.MemTotalMB, summary.MemUsedMB = readMemMB()
	summary.Processes = readTopProcesses(limit)
	summary.ProcessCount = countAllProcesses()
	return summary
}

func readLoadAvg() (one, five, fifteen float64) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, 0, 0
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return 0, 0, 0
	}
	one, _ = strconv.ParseFloat(fields[0], 64)
	five, _ = strconv.ParseFloat(fields[1], 64)
	fifteen, _ = strconv.ParseFloat(fields[2], 64)
	return
}

func readUptime() float64 {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) < 1 {
		return 0
	}
	seconds, _ := strconv.ParseFloat(fields[0], 64)
	return seconds
}

func readMemMB() (totalMB, usedMB float64) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	var totalKB, availableKB float64
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			totalKB = parseMeminfoKB(line)
		case strings.HasPrefix(line, "MemAvailable:"):
			availableKB = parseMeminfoKB(line)
		}
	}
	return totalKB / 1024, (totalKB - availableKB) / 1024
}

func countAllProcesses() int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	count := 0
	for _, e := range entries {
		if e.IsDir() {
			if _, err := strconv.Atoi(e.Name()); err == nil {
				count++
			}
		}
	}
	return count
}

// readTopProcesses shells out to `ps` rather than parsing /proc directly -- ps already handles
// every edge case here correctly (short-lived processes disappearing mid-read, %CPU smoothing,
// command-line truncation) that a hand-rolled /proc walker would have to re-solve.
// KillProcess sends TERM (default) or KILL to a PID directly via syscall -- not `kill(1)`, so
// there's no shell-argument-injection surface to worry about, just a plain signal(2) call. The
// caller (handlers_processes.go) has already refused PID 1 and this binary's own PID before this
// is reached.
func KillProcess(pid int, signalName string) error {
	sig := syscall.SIGTERM
	if strings.EqualFold(signalName, "KILL") {
		sig = syscall.SIGKILL
	}
	if err := syscall.Kill(pid, sig); err != nil {
		return fmt.Errorf("signaling pid %d: %w", pid, err)
	}
	return nil
}

func readTopProcesses(limit int) []ProcessInfo {
	out, err := exec.Command("ps", "-eo", "pid,user,%cpu,%mem,rss,etime,comm", "--sort=-%cpu", "--no-headers").Output()
	if err != nil {
		return nil
	}
	var processes []ProcessInfo
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() && len(processes) < limit {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 7 {
			continue
		}
		pid, _ := strconv.Atoi(fields[0])
		cpuPct, _ := strconv.ParseFloat(fields[2], 64)
		memPct, _ := strconv.ParseFloat(fields[3], 64)
		rss, _ := strconv.ParseInt(fields[4], 10, 64)
		processes = append(processes, ProcessInfo{
			PID: pid, User: fields[1], CPUPct: cpuPct, MemPct: memPct, RSSKB: rss,
			Elapsed: fields[5], Command: strings.Join(fields[6:], " "),
		})
	}
	return processes
}
