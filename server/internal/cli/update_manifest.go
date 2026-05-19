package cli

// Fork-specific: Manifest-based update mechanism for Chinese market (Huawei OBS).
// This complements the upstream GitHub-release-based update in update.go.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
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
