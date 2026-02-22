package updater

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	githubRepo = "Arimodu/udp-broadcast-relay"
	apiURL     = "https://api.github.com/repos/" + githubRepo + "/releases/latest"
)

// Release holds information about a GitHub release.
type Release struct {
	Tag      string // e.g. "v1.0.1-abc1234"
	Version  string // base semver e.g. "1.0.1"
	AssetURL string // direct download URL for the current platform
}

// Checker periodically queries the GitHub releases API and can apply an update.
type Checker struct {
	current string // base semver of the running binary
	mu      sync.RWMutex
	latest  *Release
	checked time.Time
}

// New creates a Checker for the given version string (e.g. "1.0.0-abc1234" or "dev").
func New(currentVersion string) *Checker {
	return &Checker{current: baseVersion(currentVersion)}
}

// CurrentVersion returns the running binary's base semver.
func (c *Checker) CurrentVersion() string { return c.current }

// Check queries the GitHub releases API and caches the result.
func (c *Checker) Check() error {
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "ubr-updater/1")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("querying GitHub: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub API returned HTTP %d", resp.StatusCode)
	}

	var payload struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return fmt.Errorf("decoding GitHub response: %w", err)
	}

	ver := baseVersion(payload.TagName)
	assetName := platformAssetName()
	var assetURL string
	for _, a := range payload.Assets {
		if a.Name == assetName {
			assetURL = a.BrowserDownloadURL
			break
		}
	}

	c.mu.Lock()
	c.latest = &Release{Tag: payload.TagName, Version: ver, AssetURL: assetURL}
	c.checked = time.Now()
	c.mu.Unlock()

	return nil
}

// Status returns whether an update is available and the cached release info.
func (c *Checker) Status() (available bool, latest *Release, checked time.Time) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.latest == nil {
		return false, nil, c.checked
	}
	return isNewer(c.latest.Version, c.current), c.latest, c.checked
}

// Apply downloads the latest release asset and replaces the running binary.
// Returns a human-readable message describing what happened.
// The caller must restart the process to use the new binary.
func (c *Checker) Apply() (string, error) {
	c.mu.RLock()
	latest := c.latest
	c.mu.RUnlock()

	if latest == nil {
		return "", fmt.Errorf("no release info available — run a check first")
	}
	if latest.AssetURL == "" {
		return "", fmt.Errorf("no download asset found for %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	execPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locating executable: %w", err)
	}

	tmpPath := execPath + ".new"
	if err := downloadFile(latest.AssetURL, tmpPath); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("downloading update: %w", err)
	}

	if runtime.GOOS == "windows" {
		// Running executables cannot be replaced on Windows.
		// Leave the .new file and instruct the user.
		return fmt.Sprintf(
			"Update downloaded to %s — stop the service, replace ubr.exe with that file, then restart.", tmpPath,
		), nil
	}

	// Linux / macOS: chmod, backup, atomic rename
	if err := os.Chmod(tmpPath, 0755); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("chmod new binary: %w", err)
	}

	bakPath := execPath + ".bak"
	os.Remove(bakPath) // ignore – may not exist
	if err := os.Rename(execPath, bakPath); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("backing up current binary: %w", err)
	}
	if err := os.Rename(tmpPath, execPath); err != nil {
		os.Rename(bakPath, execPath) // best-effort rollback
		return "", fmt.Errorf("replacing binary: %w", err)
	}

	return "Update applied. Restart the server to use the new version.", nil
}

// ─── helpers ────────────────────────────────────────────────────────────────

// baseVersion strips any "v" prefix and git-hash suffix from a version string.
// "v1.0.1-abc1234" → "1.0.1", "dev" → "0.0.0".
func baseVersion(v string) string {
	v = strings.TrimPrefix(v, "v")
	if idx := strings.IndexByte(v, '-'); idx >= 0 {
		v = v[:idx]
	}
	if v == "" || v == "dev" {
		return "0.0.0"
	}
	return v
}

// platformAssetName returns the release asset filename for the current platform.
func platformAssetName() string {
	name := "ubr-" + runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

// isNewer returns true when semver a is strictly greater than b.
func isNewer(a, b string) bool {
	ap, bp := semverParts(a), semverParts(b)
	for i := range ap {
		if ap[i] > bp[i] {
			return true
		}
		if ap[i] < bp[i] {
			return false
		}
	}
	return false
}

func semverParts(v string) [3]int {
	parts := strings.SplitN(v, ".", 3)
	var out [3]int
	for i := 0; i < 3 && i < len(parts); i++ {
		out[i], _ = strconv.Atoi(parts[i])
	}
	return out
}

func downloadFile(url, dest string) error {
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}
