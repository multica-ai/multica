package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update multica to the latest version",
	RunE:  runUpdate,
}

// githubRelease is the subset of the GitHub releases API response we need.
type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

func runUpdate(_ *cobra.Command, _ []string) error {
	fmt.Fprintf(os.Stderr, "Current version: %s (commit: %s)\n", version, commit)

	// Check latest version from GitHub.
	latest, err := fetchLatestRelease()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not check latest version: %v\n", err)
	} else {
		latestVer := strings.TrimPrefix(latest.TagName, "v")
		currentVer := strings.TrimPrefix(version, "v")
		if currentVer == latestVer {
			fmt.Fprintln(os.Stderr, "Already up to date.")
			return nil
		}
		fmt.Fprintf(os.Stderr, "Latest version:  %s\n\n", latest.TagName)
	}

	// Detect installation method and update accordingly.
	if isBrewInstall() {
		return updateViaBrew()
	}

	// Not installed via brew — show manual instructions.
	fmt.Fprintln(os.Stderr, "multica was not installed via Homebrew.")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "To install via Homebrew (recommended):")
	fmt.Fprintln(os.Stderr, "  brew install multica-ai/tap/multica")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Or download the latest release from:")
	fmt.Fprintln(os.Stderr, "  https://github.com/multica-ai/multica/releases/latest")
	return nil
}

// isBrewInstall checks whether the running multica binary was installed via Homebrew.
func isBrewInstall() bool {
	exePath, err := os.Executable()
	if err != nil {
		return false
	}
	// Resolve symlinks (brew links binaries from Cellar into prefix/bin).
	resolved, err := filepath.EvalSymlinks(exePath)
	if err != nil {
		resolved = exePath
	}

	// Check if the resolved path is inside a Homebrew prefix.
	// Common prefixes: /opt/homebrew (Apple Silicon), /usr/local (Intel Mac), or custom.
	brewPrefix := getBrewPrefix()
	if brewPrefix != "" && strings.HasPrefix(resolved, brewPrefix) {
		return true
	}

	// Fallback: check well-known Homebrew paths.
	for _, prefix := range []string{"/opt/homebrew", "/usr/local", "/home/linuxbrew/.linuxbrew"} {
		if strings.HasPrefix(resolved, prefix+"/Cellar/") {
			return true
		}
	}
	return false
}

// getBrewPrefix returns the Homebrew prefix by running `brew --prefix`, or empty string.
func getBrewPrefix() string {
	out, err := exec.Command("brew", "--prefix").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func updateViaBrew() error {
	fmt.Fprintln(os.Stderr, "Updating via Homebrew...")

	cmd := exec.Command("brew", "upgrade", "multica-ai/tap/multica")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("brew upgrade failed: %w\nYou can try manually: brew upgrade multica-ai/tap/multica", err)
	}

	fmt.Fprintln(os.Stderr, "Update complete.")
	return nil
}

func fetchLatestRelease() (*githubRelease, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/multica-ai/multica/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}
	return &release, nil
}
