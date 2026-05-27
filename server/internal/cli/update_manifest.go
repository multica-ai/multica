package cli

// Fork-specific: Manifest-based update mechanism for Chinese market (Huawei OBS).
// This complements the upstream GitHub-release-based update in update.go.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultUpdateManifestURL = "https://multica.obs.cn-east-3.myhuaweicloud.com/cli/manifest.json"
)

type UpdateManifest struct {
	Version             string                `json:"version"`
	PublishedAt         string                `json:"published_at,omitempty"`
	MinSupportedVersion string                `json:"min_supported_version,omitempty"`
	ReleaseNotes        string                `json:"release_notes,omitempty"`
	Assets              []UpdateManifestAsset `json:"assets"`
}

type UpdateManifestAsset struct {
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	Channel     string `json:"channel,omitempty"`
	URL         string `json:"download_url"`
	Checksum    string `json:"checksum"`
	ArchiveName string `json:"archive_name,omitempty"`
}

func resolveUpdateManifestURL() string {
	if env := strings.TrimSpace(os.Getenv("MULTICA_UPDATE_MANIFEST_URL")); env != "" {
		return env
	}
	cfg, err := LoadCLIConfig()
	if err == nil && strings.TrimSpace(cfg.UpdateManifestURL) != "" {
		return strings.TrimSpace(cfg.UpdateManifestURL)
	}
	return DefaultUpdateManifestURL
}

func FetchUpdateManifestFromURL(url string) (*UpdateManifest, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest(http.MethodGet, strings.TrimSpace(url), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("update manifest returned %d", resp.StatusCode)
	}

	var manifest UpdateManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return nil, err
	}
	if strings.TrimSpace(manifest.Version) == "" {
		return nil, fmt.Errorf("update manifest missing version")
	}
	return &manifest, nil
}

func resolveManagedInstallPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	binaryName := "multica"
	if runtime.GOOS == "windows" {
		binaryName = "multica.exe"
	}
	return filepath.Join(home, ".multica", "bin", binaryName), nil
}

func ResolveInstalledBinaryPath() (string, error) {
	managedPath, err := resolveManagedInstallPath()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(managedPath); err == nil {
		return managedPath, nil
	}

	exePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve executable path: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exePath)
	if err == nil {
		return resolved, nil
	}
	return exePath, nil
}

func IsManagedInstall() bool {
	managedPath, err := resolveManagedInstallPath()
	if err != nil {
		return false
	}
	current, err := ResolveInstalledBinaryPath()
	if err != nil {
		return false
	}
	return current == managedPath
}

// ShouldUpdate reports whether the manifest indicates a newer version than current.
func ShouldUpdate(currentVersion string, latest *UpdateManifest) (bool, error) {
	if latest == nil {
		return false, fmt.Errorf("nil manifest")
	}
	return IsNewerVersion(latest.Version, currentVersion), nil
}

// FetchLatestManifestRelease fetches the latest release info from the configured manifest URL.
func FetchLatestManifestRelease() (*UpdateManifest, error) {
	return FetchUpdateManifestFromURL(resolveUpdateManifestURL())
}

// FindManifestAsset returns the asset matching the current OS/arch from the manifest.
func FindManifestAsset(manifest *UpdateManifest, goos, goarch string) (*UpdateManifestAsset, error) {
for i := range manifest.Assets {
a := &manifest.Assets[i]
if a.OS == goos && a.Arch == goarch {
return a, nil
}
}
return nil, fmt.Errorf("no manifest asset for %s/%s", goos, goarch)
}

// UpdateViaManifestDownload downloads the CLI binary from the OBS manifest's
// download_url and verifies it with the manifest checksum. This is the primary
// update path for the Fork distribution; the GitHub Release path in update.go
// is retained as fallback.
func UpdateViaManifestDownload(targetVersion string) (string, error) {
return UpdateViaManifestDownloadWithTimeout(targetVersion, DefaultUpdateDownloadTimeout)
}

// UpdateViaManifestDownloadWithTimeout is like UpdateViaManifestDownload but
// accepts a custom download timeout.
func UpdateViaManifestDownloadWithTimeout(targetVersion string, downloadTimeout time.Duration) (string, error) {
manifest, err := FetchLatestManifestRelease()
if err != nil {
return "", fmt.Errorf("fetch manifest: %w", err)
}

asset, err := FindManifestAsset(manifest, runtime.GOOS, runtime.GOARCH)
if err != nil {
return "", err
}

if strings.TrimSpace(asset.URL) == "" {
return "", fmt.Errorf("manifest asset for %s/%s has empty download_url", runtime.GOOS, runtime.GOARCH)
}

// Determine current binary path.
exePath, err := os.Executable()
if err != nil {
return "", fmt.Errorf("resolve executable path: %w", err)
}
exePath, err = filepath.EvalSymlinks(exePath)
if err != nil {
return "", fmt.Errorf("resolve symlink: %w", err)
}

timeout := updateDownloadTimeoutOrDefault(downloadTimeout)
archiveData, err := fetchURLBytes(asset.URL, timeout)
if err != nil {
return "", fmt.Errorf("download from manifest URL failed: %w", err)
}

// Verify checksum from manifest.
if strings.TrimSpace(asset.Checksum) == "" {
return "", fmt.Errorf("manifest asset has empty checksum for %s/%s", runtime.GOOS, runtime.GOARCH)
}
archiveName := asset.ArchiveName
if archiveName == "" {
archiveName = filepath.Base(asset.URL)
}
if err := verifyAssetSHA256(archiveData, asset.Checksum, archiveName); err != nil {
return "", fmt.Errorf("verify manifest download: %w", err)
}

// Extract binary.
binaryName := "multica"
if runtime.GOOS == "windows" {
binaryName = "multica.exe"
}
var binaryData []byte
if runtime.GOOS == "windows" {
binaryData, err = extractBinaryFromZip(bytes.NewReader(archiveData), binaryName)
} else {
binaryData, err = extractBinaryFromTarGz(bytes.NewReader(archiveData), binaryName)
}
if err != nil {
return "", fmt.Errorf("extract binary: %w", err)
}

// Atomic replace.
dir := filepath.Dir(exePath)
tmpFile, err := os.CreateTemp(dir, "multica-update-*")
if err != nil {
return "", fmt.Errorf("create temp file: %w", err)
}
tmpPath := tmpFile.Name()

if _, err := tmpFile.Write(binaryData); err != nil {
tmpFile.Close()
os.Remove(tmpPath)
return "", fmt.Errorf("write temp file: %w", err)
}
tmpFile.Close()

info, err := os.Stat(exePath)
if err != nil {
os.Remove(tmpPath)
return "", fmt.Errorf("stat original binary: %w", err)
}
if err := os.Chmod(tmpPath, info.Mode()); err != nil {
os.Remove(tmpPath)
return "", fmt.Errorf("chmod temp file: %w", err)
}

if err := replaceBinary(tmpPath, exePath); err != nil {
os.Remove(tmpPath)
return "", fmt.Errorf("replace binary: %w", err)
}

return fmt.Sprintf("Downloaded %s from manifest and replaced %s", archiveName, exePath), nil
}

// forkVersionRe matches the git-describe style version used by Fork builds:
// v<major>.<minor>.<patch>-<commitCount>-g<hash> with optional trailing segments
// (e.g. "-dirty" or a second describe layer like "-14-g12d314f5c").
// Examples:
//   - v0.3.6-767-g16a0ca0a1
//   - v0.3.6-767-g16a0ca0a1-14-g12d314f5c
//
// NOT matched (treated as dirty dev build):
//   - v0.3.6-767-g16a0ca0a1-dirty
var forkVersionRe = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)-(\d+)-g([0-9a-f]+)(?:-(\d+)-g([0-9a-f]+))?$`)

// IsForkVersion reports whether v looks like a Fork release version produced
// by `git describe` (e.g. "v0.3.6-767-g16a0ca0a1"). Versions with a "-dirty"
// suffix are NOT considered fork releases — those are local dev builds.
func IsForkVersion(v string) bool {
	s := strings.TrimSpace(v)
	if strings.HasSuffix(s, "-dirty") {
		return false
	}
	return forkVersionRe.MatchString(s)
}

// forkVersion holds the parsed components of a fork-style version string.
type forkVersion struct {
	Major, Minor, Patch int
	CommitCount         int
	// SecondCount captures the second commit-count segment in stacked describe
	// versions like v0.3.6-767-g16a0ca0a1-14-g12d314f5c (the "14").
	SecondCount int
}

// parseForkVersion extracts structured version info from a fork version string.
// Returns (parsed, true) on success; (zero, false) if v is not a valid fork version.
func parseForkVersion(v string) (forkVersion, bool) {
	s := strings.TrimSpace(v)
	if strings.HasSuffix(s, "-dirty") {
		return forkVersion{}, false
	}
	m := forkVersionRe.FindStringSubmatch(s)
	if m == nil {
		return forkVersion{}, false
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	patch, _ := strconv.Atoi(m[3])
	commitCount, _ := strconv.Atoi(m[4])
	var secondCount int
	if m[6] != "" {
		secondCount, _ = strconv.Atoi(m[6])
	}
	return forkVersion{
		Major:       major,
		Minor:       minor,
		Patch:       patch,
		CommitCount: commitCount,
		SecondCount: secondCount,
	}, true
}

// IsNewerForkVersion reports whether latest is strictly newer than current,
// where both are fork-style version strings. Comparison order:
// major > minor > patch > commitCount > secondCount.
func IsNewerForkVersion(latest, current string) bool {
	l, lok := parseForkVersion(latest)
	if !lok {
		return false
	}
	c, cok := parseForkVersion(current)
	if !cok {
		return false
	}
	if l.Major != c.Major {
		return l.Major > c.Major
	}
	if l.Minor != c.Minor {
		return l.Minor > c.Minor
	}
	if l.Patch != c.Patch {
		return l.Patch > c.Patch
	}
	if l.CommitCount != c.CommitCount {
		return l.CommitCount > c.CommitCount
	}
	return l.SecondCount > c.SecondCount
}

// IsUpdatableVersion reports whether v is eligible for auto-update — either a
// clean tagged release (official) or a fork release (git-describe style).
func IsUpdatableVersion(v string) bool {
	return IsReleaseVersion(v) || IsForkVersion(v)
}

// IsNewerUpdatableVersion reports whether latest is strictly newer than current
// for any version style supported by the auto-update system. Both versions must
// be of the same style (both release or both fork) for comparison to succeed.
// Returns false if either side cannot be parsed or the styles mismatch.
func IsNewerUpdatableVersion(latest, current string) bool {
	// Try official release comparison first.
	if IsReleaseVersion(latest) && IsReleaseVersion(current) {
		return IsNewerVersion(latest, current)
	}
	// Try fork comparison.
	if IsForkVersion(latest) && IsForkVersion(current) {
		return IsNewerForkVersion(latest, current)
	}
	return false
}

// FetchLatestManifestReleaseAsGitHubRelease adapts FetchLatestManifestRelease
// into the *GitHubRelease shape expected by the auto-update loop's
// fetchLatestRelease indirection. This lets the loop use the OBS manifest as
// its version source without changing the loop's core mechanics.
func FetchLatestManifestReleaseAsGitHubRelease() (*GitHubRelease, error) {
	manifest, err := FetchLatestManifestRelease()
	if err != nil {
		return nil, err
	}
	return &GitHubRelease{
		TagName: manifest.Version,
	}, nil
}
