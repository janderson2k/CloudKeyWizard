package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// LifeRaft: a native FDT.Scout feature (not a separate installed Extra, unlike Restic) that pulls
// read-only backups from SMB/FTP sources on a schedule and keeps them on this device's own /volume
// storage. This file is specifically the prerequisite everything else depends on: setting up
// /volume from FDT.Scout's own GUI, which today only CloudKeyWizard (over SSH) can do. It
// deliberately mirrors that Windows app's own script exactly -- same wipe/format/mount-unit
// sequence, same "volume.mount" unit name and content -- so a device set up from either app looks
// identical to the other, and re-running this against an already-correctly-set-up device is a
// no-op either way. See Scripts/runbook/phase1-format-mount-volume.sh (the script this ports) for
// the full history of why the mount is a systemd .mount unit and never /etc/fstab: this board's
// vendor bootup-hook framework silently resets /etc/fstab to a minimal template on every boot,
// dropping any manually-added line, but leaves a systemd unit file alone.

// DriveRisk mirrors CloudKeyWizard's own Services/BlockDeviceService.cs DeviceRisk classification
// exactly -- same picker safety logic on both sides of this app, not reinvented here.
type DriveRisk string

const (
	DriveRecommended DriveRisk = "recommended"
	DriveDoNotSelect DriveRisk = "do-not-select"
)

// CandidateDrive is one disk (or unpartitioned whole-device) LifeRaft's storage setup could target
// -- always the WHOLE disk, never a single partition, matching the Wizard's own "superfloppy,
// no partition table" convention for this bulk-storage role.
type CandidateDrive struct {
	Name       string    `json:"name"` // lsblk NAME, e.g. "sda"
	Path       string    `json:"path"` // "/dev/sda"
	Size       string    `json:"size"`
	Mountpoint string    `json:"mountpoint,omitempty"`
	Risk       DriveRisk `json:"risk"`
	RiskReason string    `json:"riskReason"`
}

// classifyDriveRisk is the Go mirror of BlockDeviceService.ClassifyRisk -- same four rules, same
// order, same reasoning: mounted, raw NAND boot flash, eMMC RPMB secure partition, virtual
// compressed swap are all DoNotSelect; everything else is advisory-Recommended. Advisory only --
// never auto-applied without the operator explicitly selecting the device and typing the
// confirmation phrase.
func classifyDriveRisk(name, mountpoint string) (DriveRisk, string) {
	if strings.TrimSpace(mountpoint) != "" {
		return DriveDoNotSelect, fmt.Sprintf("Currently mounted at %s -- likely the live system.", mountpoint)
	}
	lower := strings.ToLower(name)
	if strings.HasPrefix(lower, "mtd") {
		return DriveDoNotSelect, "Raw NAND boot flash (bootloader/kernel), not bulk storage."
	}
	if strings.HasSuffix(lower, "rpmb") {
		return DriveDoNotSelect, "eMMC RPMB partition -- a secure/write-protected region, not a data target."
	}
	if strings.HasPrefix(lower, "zram") {
		return DriveDoNotSelect, "Virtual compressed-swap device, not a physical disk."
	}
	return DriveRecommended, "Unmounted, not a known boot/secure/virtual device -- looks like real bulk storage."
}

// ListCandidateDrives lists whole disks (type=="disk") only -- LifeRaft's storage setup always
// wipes and reformats the ENTIRE device, no partition table, so a partition isn't a valid unit of
// selection here the way it is for the separate USB-drive browse/mount feature in storage.go.
func ListCandidateDrives() ([]CandidateDrive, error) {
	out, err := exec.Command("lsblk", "-b", "-J", "-o", "NAME,SIZE,TYPE,MOUNTPOINT").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("lsblk failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	var parsed lsblkOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, fmt.Errorf("couldn't parse lsblk output: %w", err)
	}

	var drives []CandidateDrive
	for _, dev := range parsed.BlockDevices {
		if dev.Type != "disk" {
			continue
		}
		// A disk with a mounted child partition is itself unmounted at the top level but still
		// very much in use -- roll any child mountpoint up so it's caught by the same DoNotSelect
		// rule as a directly-mounted disk, not silently missed.
		mountpoint := ""
		if dev.Mountpoint != nil {
			mountpoint = *dev.Mountpoint
		}
		if mountpoint == "" {
			for _, child := range dev.Children {
				if child.Mountpoint != nil && *child.Mountpoint != "" {
					mountpoint = *child.Mountpoint
					break
				}
			}
		}
		risk, reason := classifyDriveRisk(dev.Name, mountpoint)
		drives = append(drives, CandidateDrive{
			Name: dev.Name, Path: "/dev/" + dev.Name, Size: humanizeBytes(dev.Size.value),
			Mountpoint: mountpoint, Risk: risk, RiskReason: reason,
		})
	}
	return drives, nil
}

// LifeRaftStorageStatus is what the setup wizard's first screen (and every other LifeRaft page)
// checks before doing anything else.
type LifeRaftStorageStatus struct {
	Ready            bool    `json:"ready"` // /volume is mounted -- LifeRaft can store data
	Device           string  `json:"device,omitempty"`
	TotalGB          float64 `json:"totalGb,omitempty"`
	FreeGB           float64 `json:"freeGb,omitempty"`
	UsedByLifeRaftGB float64 `json:"usedByLifeRaftGb,omitempty"`
}

const lifeRaftDataRoot = "/volume/liferaft"

func GetLifeRaftStorageStatus() LifeRaftStorageStatus {
	if !isRealMountpoint("/volume") {
		return LifeRaftStorageStatus{Ready: false}
	}
	status := LifeRaftStorageStatus{Ready: true}
	status.TotalGB, status.FreeGB = diskTotalAndFreeGB("/volume")
	status.UsedByLifeRaftGB = dirSizeGB(lifeRaftDataRoot)
	if out, err := exec.Command("findmnt", "-n", "-o", "SOURCE", "/volume").Output(); err == nil {
		status.Device = strings.TrimSpace(string(out))
	}
	return status
}

// isRealMountpoint confirms /volume is an actual mount, not just a directory that happens to
// exist -- the Wizard's own volume.mount unit creates the directory via its own Where= line
// regardless of whether the mount itself ever succeeds, so a plain os.Stat("/volume") (what the
// rest of this codebase's older /volume checks still do -- see the LifeRaft design notes) can't
// tell "mounted" from "empty directory left behind by a failed mount" apart. findmnt is the real
// mount table, not a directory-existence guess.
func isRealMountpoint(path string) bool {
	return exec.Command("findmnt", "--noheadings", path).Run() == nil
}

// SetUpLifeRaftStorage wipes `device` and formats/mounts it as /volume -- WHOLE DISK, deliberately
// unconditional, exactly mirroring phase1-format-mount-volume.sh's own behavior byte-for-byte
// (same wipefs + mkfs.ext4 + systemd unit content, same unit name "volume.mount"). Safe to call
// again later against an already-correctly-set-up device: writing the same unit content and
// re-enabling it is a no-op, same guarantee the shell script already documents.
func SetUpLifeRaftStorage(device string) error {
	if !validDeviceName.MatchString(device) {
		return fmt.Errorf("invalid device name")
	}
	devPath := "/dev/" + device
	if info, err := os.Stat(devPath); err != nil || info.Mode()&os.ModeDevice == 0 {
		return fmt.Errorf("%s is not a block device", devPath)
	}

	if out, err := exec.Command("wipefs", "-a", devPath).CombinedOutput(); err != nil {
		return fmt.Errorf("wipefs failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	if out, err := exec.Command("mkfs.ext4", "-F", "-L", "volume", devPath).CombinedOutput(); err != nil {
		return fmt.Errorf("mkfs.ext4 failed: %s: %w", strings.TrimSpace(string(out)), err)
	}

	uuidOut, err := exec.Command("blkid", "-s", "UUID", "-o", "value", devPath).Output()
	if err != nil {
		return fmt.Errorf("couldn't read the new filesystem's UUID: %w", err)
	}
	uuid := strings.TrimSpace(string(uuidOut))

	unit := fmt.Sprintf(`[Unit]
Description=Bulk storage drive (whole-disk ext4, no partition table)

[Mount]
What=/dev/disk/by-uuid/%s
Where=/volume
Type=ext4
Options=defaults,noatime

[Install]
WantedBy=local-fs.target
`, uuid)
	if err := os.WriteFile("/etc/systemd/system/volume.mount", []byte(unit), 0644); err != nil {
		return fmt.Errorf("writing volume.mount unit: %w", err)
	}

	if out, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl daemon-reload failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	if out, err := exec.Command("systemctl", "enable", "--now", "volume.mount").CombinedOutput(); err != nil {
		return fmt.Errorf("mounting /volume failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	if !isRealMountpoint("/volume") {
		return fmt.Errorf("volume.mount was enabled but /volume isn't actually mounted -- check `systemctl status volume.mount`")
	}
	if err := os.MkdirAll(lifeRaftDataRoot, 0700); err != nil {
		return fmt.Errorf("/volume mounted, but couldn't create %s: %w", lifeRaftDataRoot, err)
	}
	return nil
}

func diskTotalAndFreeGB(path string) (totalGB, freeGB float64) {
	total := readDiskTotalGB(path)
	usedPct := readDiskPercent(path)
	if usedPct < 0 {
		return total, 0
	}
	return total, total * (100 - usedPct) / 100
}

func dirSizeGB(path string) float64 {
	var total int64
	_ = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil // best-effort -- a file that vanishes mid-walk or a permission hiccup shouldn't fail the whole size check
		}
		if info, infoErr := d.Info(); infoErr == nil {
			total += info.Size()
		}
		return nil
	})
	return float64(total) / 1024 / 1024 / 1024
}
