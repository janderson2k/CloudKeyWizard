package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Self-update over GitHub -- the one deliberate exception to this project's otherwise strict
// "nothing fetched at runtime" rule (stated plainly in app.js's own file header). It exists because
// the alternative -- reconnecting the Windows wizard over SSH and clicking Update every time --
// is real, repeated friction this same session hit directly. Kept narrow and honest about the
// tradeoff: user-initiated only (a Check button and an Update button, never automatic or silent),
// a short timeout so a device with no internet access just reports that plainly instead of hanging,
// and the downloaded binary's own -version output is trusted over the release tag/notes -- no
// separate manifest to keep in sync or get wrong.

const (
	githubReleasesAPI = "https://api.github.com/repos/janderson2k/CloudKeyWizard/releases/latest"
	fdtscoutAssetName = "fdtscout-arm64"
)

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Digest             string `json:"digest"` // "sha256:<hex>", when GitHub reports one
}

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

// FDTScoutUpdateCheck is what both the check and apply endpoints report back to the UI.
type FDTScoutUpdateCheck struct {
	Available      bool   `json:"available"`
	CurrentVersion string `json:"currentVersion"`
	LatestVersion  string `json:"latestVersion,omitempty"`
	Error          string `json:"error,omitempty"`
}

var githubAPIClient = &http.Client{Timeout: 10 * time.Second}
var githubDownloadClient = &http.Client{Timeout: 2 * time.Minute}

func fetchLatestFDTScoutAsset() (*githubAsset, error) {
	req, err := http.NewRequest("GET", githubReleasesAPI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "fdtscout-selfupdate")
	resp, err := githubAPIClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("couldn't reach github.com -- check this device has internet access: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github.com returned %s", resp.Status)
	}
	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("couldn't parse github's response: %w", err)
	}
	for _, a := range rel.Assets {
		if a.Name == fdtscoutAssetName {
			asset := a
			return &asset, nil
		}
	}
	return nil, fmt.Errorf("the latest release (%s) doesn't have a %s asset attached", rel.TagName, fdtscoutAssetName)
}

// downloadCandidate downloads the asset directly into destDir (the running binary's own directory,
// so the later rename-into-place is same-filesystem and therefore atomic -- a rename across
// filesystems isn't), verifies its sha256 against what GitHub reported (when present), and returns
// the downloaded file's path. Caller owns cleanup.
func downloadCandidate(asset *githubAsset, destDir string) (string, error) {
	req, err := http.NewRequest("GET", asset.BrowserDownloadURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := githubDownloadClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("downloading update: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned %s", resp.Status)
	}

	dst := filepath.Join(destDir, fmt.Sprintf(".fdtscout-update-%d", time.Now().UnixNano()))
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0755)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(f, h), resp.Body)
	closeErr := f.Close()
	if copyErr != nil {
		os.Remove(dst)
		return "", fmt.Errorf("downloading update: %w", copyErr)
	}
	if closeErr != nil {
		os.Remove(dst)
		return "", closeErr
	}

	if asset.Digest != "" {
		want := strings.TrimPrefix(asset.Digest, "sha256:")
		got := hex.EncodeToString(h.Sum(nil))
		if !strings.EqualFold(want, got) {
			os.Remove(dst)
			return "", fmt.Errorf("downloaded file's checksum doesn't match what GitHub reported -- refusing to install it")
		}
	}
	return dst, nil
}

// candidateVersion runs -version on the downloaded (not-yet-installed) binary to find out what it
// actually is. More robust than parsing the release tag or notes as prose, and reuses the exact
// mechanism CloudKeyWizard's own remote version check already relies on.
func candidateVersion(path string) (string, error) {
	out, err := exec.Command(path, "-version").Output()
	if err != nil {
		return "", fmt.Errorf("couldn't run the downloaded binary to check its version: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// compareVersions compares dotted numeric version strings component-wise, not lexically -- "2.9.0"
// must compare as LESS than "2.10.0" (9 < 10), which plain string comparison would get backwards.
func compareVersions(a, b string) int {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var an, bn int
		if i < len(as) {
			an, _ = strconv.Atoi(strings.TrimSpace(as[i]))
		}
		if i < len(bs) {
			bn, _ = strconv.Atoi(strings.TrimSpace(bs[i]))
		}
		if an != bn {
			if an < bn {
				return -1
			}
			return 1
		}
	}
	return 0
}

func selfBinaryPath() string {
	if p, err := os.Executable(); err == nil {
		return p
	}
	return "/opt/fdtscout/fdtscout"
}

// CheckFDTScoutSelfUpdate downloads and probes the latest release's binary (read-only -- the
// downloaded file is removed before returning either way, nothing about the running install
// changes) and reports whether it's actually newer than what's running right now.
func CheckFDTScoutSelfUpdate() FDTScoutUpdateCheck {
	result := FDTScoutUpdateCheck{CurrentVersion: Version}
	selfPath := selfBinaryPath()
	asset, err := fetchLatestFDTScoutAsset()
	if err != nil {
		result.Error = err.Error()
		return result
	}
	dst, err := downloadCandidate(asset, filepath.Dir(selfPath))
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer os.Remove(dst)
	v, err := candidateVersion(dst)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.LatestVersion = v
	result.Available = compareVersions(v, Version) > 0
	return result
}

// ApplyFDTScoutSelfUpdate re-checks and re-downloads fresh -- never trusts state from an earlier
// check call, which could be stale or from a different browser session entirely. On success,
// atomically replaces the running binary and triggers a restart shortly after returning, so the
// HTTP response reaches the caller before the service actually goes down. Mirrors the exact same
// restart command CloudKeyWizard's own SSH install/update flow already uses (ExtraCatalog.cs's
// `systemctl restart fdtscout`) rather than inventing a second restart discipline.
func ApplyFDTScoutSelfUpdate() (string, error) {
	selfPath := selfBinaryPath()
	asset, err := fetchLatestFDTScoutAsset()
	if err != nil {
		return "", err
	}
	dst, err := downloadCandidate(asset, filepath.Dir(selfPath))
	if err != nil {
		return "", err
	}

	v, err := candidateVersion(dst)
	if err != nil {
		os.Remove(dst)
		return "", err
	}
	if compareVersions(v, Version) <= 0 {
		os.Remove(dst)
		return "", fmt.Errorf("already running the latest version (%s)", Version)
	}

	if err := os.Rename(dst, selfPath); err != nil {
		os.Remove(dst)
		return "", fmt.Errorf("installing the update: %w", err)
	}

	go func() {
		time.Sleep(500 * time.Millisecond)
		_ = exec.Command("systemctl", "restart", "fdtscout").Start()
	}()

	return v, nil
}

func handleFDTScoutCheckUpdate(w http.ResponseWriter, r *http.Request, _ string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(CheckFDTScoutSelfUpdate())
}

func handleFDTScoutApplyUpdate(w http.ResponseWriter, r *http.Request, _ string) {
	w.Header().Set("Content-Type", "application/json")
	newVersion, err := ApplyFDTScoutSelfUpdate()
	if err != nil {
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "version": newVersion, "restarting": true})
}
