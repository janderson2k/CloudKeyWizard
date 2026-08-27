package main

import (
	"bufio"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// SystemSpecs is static-ish hardware info shown once at the top of the Health tab -- "what is this
// box," distinct from the sampled-over-time charts below it ("how is it doing right now").
type SystemSpecs struct {
	CPUModel   string  `json:"cpuModel"`
	CPUCores   int     `json:"cpuCores"`
	MemTotalMB float64 `json:"memTotalMb"`
	RootTotalGB float64 `json:"rootTotalGb"`
	VolumeTotalGB float64 `json:"volumeTotalGb,omitempty"`
	Kernel     string  `json:"kernel"`
	Arch       string  `json:"arch"`
	Hostname   string  `json:"hostname"`
}

func GetSystemSpecs() SystemSpecs {
	specs := SystemSpecs{}
	specs.CPUModel, specs.CPUCores = readCPUInfo()
	specs.MemTotalMB, _ = readMemMB()
	specs.RootTotalGB = readDiskTotalGB("/")
	if _, err := os.Stat("/volume"); err == nil {
		specs.VolumeTotalGB = readDiskTotalGB("/volume")
	}
	if out, err := exec.Command("uname", "-r").Output(); err == nil {
		specs.Kernel = strings.TrimSpace(string(out))
	}
	if out, err := exec.Command("uname", "-m").Output(); err == nil {
		specs.Arch = strings.TrimSpace(string(out))
	}
	specs.Hostname, _ = os.Hostname()
	return specs
}

// readCPUInfo is deliberately defensive about which /proc/cpuinfo field actually has a usable
// name -- "model name" (the common x86 field) is frequently empty or absent on ARM, where the
// SoC name more often shows up under "Hardware" or "Model" instead. Tries all three, first match
// wins. Core count is just a count of "processor" lines, which is reliable across architectures.
func readCPUInfo() (model string, cores int) {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return "", 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch key {
		case "processor":
			cores++
		case "model name", "Hardware", "Model":
			if model == "" && value != "" {
				model = value
			}
		}
	}
	return model, cores
}

func readDiskTotalGB(path string) float64 {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0
	}
	totalBytes := float64(stat.Blocks) * float64(stat.Bsize)
	return totalBytes / 1024 / 1024 / 1024
}
