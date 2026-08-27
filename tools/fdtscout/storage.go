package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

const usbMountRoot = "/media/fdtscout"

// validDeviceName guards every place a device name flows into a shell-executed command (mount
// point path, "/dev/"+device passed to mount/umount) -- alnum only, matches real lsblk NAME output
// (sda1, sdb2, mmcblk0p1, ...) and rejects anything that could be a path-traversal or command-
// injection attempt smuggled in through the API.
var validDeviceName = regexp.MustCompile(`^[a-zA-Z0-9]+$`)

// lsblkRawSize mirrors the exact fix CloudKeyWizard's own Services/BlockDeviceService.cs already
// needed for this same hardware: `lsblk -b -J` reports SIZE as a raw JSON number on this device's
// lsblk build, not a quoted string -- a plain `string` field silently fails to unmarshal it.
// Accepting either shape here the same way that C# fix does.
type lsblkRawSize struct{ value string }

func (s *lsblkRawSize) UnmarshalJSON(data []byte) error {
	var asString string
	if json.Unmarshal(data, &asString) == nil {
		s.value = asString
		return nil
	}
	var asNumber json.Number
	if err := json.Unmarshal(data, &asNumber); err != nil {
		return err
	}
	s.value = asNumber.String()
	return nil
}

type lsblkDevice struct {
	Name       string        `json:"name"`
	Size       lsblkRawSize  `json:"size"`
	Type       string        `json:"type"`
	Tran       *string       `json:"tran"`
	Mountpoint *string       `json:"mountpoint"`
	FSType     *string       `json:"fstype"`
	Children   []lsblkDevice `json:"children,omitempty"`
}

type lsblkOutput struct {
	BlockDevices []lsblkDevice `json:"blockdevices"`
}

// USBDrive is one partition (or a whole unpartitioned disk) on a USB-attached device -- the unit
// this app lets you mount/browse. Reported flat, not as the tree lsblk itself returns, since
// that's what the Storage tab actually wants to show and act on.
type USBDrive struct {
	Device      string `json:"device"`      // e.g. "sda1", the lsblk NAME
	Path        string `json:"path"`        // e.g. "/dev/sda1"
	Size        string `json:"size"`
	FSType      string `json:"fstype"`
	Mounted     bool   `json:"mounted"`
	MountPoint  string `json:"mountPoint,omitempty"`
}

// ListUSBDrives walks lsblk's device tree looking for partitions (or bare disks with no
// partition table) whose PARENT disk reports tran=="usb" -- lsblk only reports the transport type
// on the top-level disk entry, not on each partition underneath it, so a partition's own `tran`
// field is typically empty and has to be inherited from its parent instead.
func ListUSBDrives() ([]USBDrive, error) {
	out, err := exec.Command("lsblk", "-b", "-J", "-o", "NAME,SIZE,TYPE,TRAN,MOUNTPOINT,FSTYPE").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("lsblk failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	var parsed lsblkOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, fmt.Errorf("couldn't parse lsblk output: %w", err)
	}

	var drives []USBDrive
	for _, disk := range parsed.BlockDevices {
		isUSB := disk.Tran != nil && *disk.Tran == "usb"
		if !isUSB {
			continue
		}
		if len(disk.Children) == 0 {
			// Unpartitioned disk -- treat the whole disk as the mountable unit.
			drives = append(drives, usbDriveFrom(disk))
			continue
		}
		for _, part := range disk.Children {
			drives = append(drives, usbDriveFrom(part))
		}
	}
	return drives, nil
}

func usbDriveFrom(d lsblkDevice) USBDrive {
	drive := USBDrive{Device: d.Name, Path: "/dev/" + d.Name, Size: humanizeBytes(d.Size.value)}
	if d.FSType != nil {
		drive.FSType = *d.FSType
	}
	if d.Mountpoint != nil && *d.Mountpoint != "" {
		drive.Mounted = true
		drive.MountPoint = *d.Mountpoint
	}
	return drive
}

func humanizeBytes(raw string) string {
	var bytes int64
	if _, err := fmt.Sscanf(raw, "%d", &bytes); err != nil {
		return raw
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	size := float64(bytes)
	unit := 0
	for size >= 1024 && unit < len(units)-1 {
		size /= 1024
		unit++
	}
	return fmt.Sprintf("%.1f %s", size, units[unit])
}

// MountUSBDrive creates a dedicated mount point under usbMountRoot (never reuses/overwrites an
// arbitrary existing directory) and mounts the device with no explicit -t, letting the kernel
// autodetect the filesystem the same way `mount` always does for a plain unspecified-type mount.
func MountUSBDrive(device string) (string, error) {
	if !validDeviceName.MatchString(device) {
		return "", fmt.Errorf("invalid device name")
	}
	mountPoint := filepath.Join(usbMountRoot, device)
	if err := os.MkdirAll(mountPoint, 0755); err != nil {
		return "", fmt.Errorf("creating mount point: %w", err)
	}
	if out, err := exec.Command("mount", "/dev/"+device, mountPoint).CombinedOutput(); err != nil {
		return "", fmt.Errorf("mount failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return mountPoint, nil
}

func UnmountUSBDrive(device string) error {
	if !validDeviceName.MatchString(device) {
		return fmt.Errorf("invalid device name")
	}
	if out, err := exec.Command("umount", "/dev/"+device).CombinedOutput(); err != nil {
		return fmt.Errorf("umount failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}
