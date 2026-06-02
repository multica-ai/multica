package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// DefaultUpdateDownloadTimeout is retained for API compatibility with cmd_update.
const DefaultUpdateDownloadTimeout = 120 * time.Second

// ErrSelfHostedUpdate is returned by all update paths since external GitHub
// updates are no longer supported in the self-hosted / isolated platform.
var ErrSelfHostedUpdate = fmt.Errorf("self-hosted: updates managed manually")

// GitHubRelease is retained for API compatibility but never populated.
type GitHubRelease struct {
	TagName string               `json:"tag_name"`
	HTMLURL string               `json:"html_url"`
	Assets  []GitHubReleaseAsset `json:"assets"`
}

// GitHubReleaseAsset is retained for API compatibility.
type GitHubReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// knownBrewPrefixes lists the install roots Homebrew uses on each platform.
var knownBrewPrefixes = []string{"/opt/homebrew", "/usr/local", "/home/linuxbrew/.linuxbrew"}

// MatchKnownBrewPrefix returns the Homebrew prefix whose Cellar contains path,
// or "" if path is not under a known Cellar.
func MatchKnownBrewPrefix(path string) string {
	for _, prefix := range knownBrewPrefixes {
		if strings.HasPrefix(path, prefix+"/Cellar/") {
			return prefix
		}
	}
	return ""
}

// IsBrewInstall checks whether the running wallts binary was installed via Homebrew.
func IsBrewInstall() bool {
	exePath, err := os.Executable()
	if err != nil {
		return false
	}
	resolved, err := filepath.EvalSymlinks(exePath)
	if err != nil {
		resolved = exePath
	}

	brewPrefix := GetBrewPrefix()
	if brewPrefix != "" && strings.HasPrefix(resolved, brewPrefix) {
		return true
	}

	return MatchKnownBrewPrefix(resolved) != ""
}

// GetBrewPrefix returns the Homebrew prefix by running `brew --prefix`, or empty string.
func GetBrewPrefix() string {
	out, err := exec.Command("brew", "--prefix").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// FetchLatestRelease always returns an error — external GitHub polling is disabled.
func FetchLatestRelease() (*GitHubRelease, error) {
	return nil, ErrSelfHostedUpdate
}

// IsReleaseVersion always returns false — release detection is disabled.
func IsReleaseVersion(_ string) bool {
	return false
}

// IsNewerVersion always returns false — version comparison is disabled.
func IsNewerVersion(_, _ string) bool {
	return false
}

// UpdateViaBrew always returns an error — Homebrew updates are disabled.
func UpdateViaBrew() (string, error) {
	return "", ErrSelfHostedUpdate
}

// UpdateViaDownload always returns an error — direct download updates are disabled.
func UpdateViaDownload(_ string) (string, error) {
	return "", ErrSelfHostedUpdate
}

// UpdateViaDownloadWithTimeout always returns an error — direct download updates are disabled.
func UpdateViaDownloadWithTimeout(_ string, _ time.Duration) (string, error) {
	return "", ErrSelfHostedUpdate
}
