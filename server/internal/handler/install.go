package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	_ "embed"
)

//go:embed install_scripts/install.sh
var installSH string

//go:embed install_scripts/install.ps1
var installPS1 string

const (
	manifestURL       = "https://multica.obs.cn-east-3.myhuaweicloud.com/cli/manifest.json"
	versionCacheTTL   = 5 * time.Minute
	manifestFetchTime = 10 * time.Second
)

type cliManifest struct {
	Version string          `json:"version"`
	Commit  string          `json:"commit"`
	Date    string          `json:"date"`
	Assets  []manifestAsset `json:"assets"`
}

type manifestAsset struct {
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	Filename    string `json:"filename"`
	DownloadURL string `json:"download_url"`
	Checksum    string `json:"checksum"`
	Size        int64  `json:"size"`
}

var (
	cachedVersion   string
	cacheExpiry     time.Time
	versionCacheMu  sync.RWMutex
)

func fetchLatestCLIVersion() (string, error) {
	versionCacheMu.RLock()
	if time.Now().Before(cacheExpiry) && cachedVersion != "" {
		v := cachedVersion
		versionCacheMu.RUnlock()
		return v, nil
	}
	versionCacheMu.RUnlock()

	client := &http.Client{Timeout: manifestFetchTime}
	resp, err := client.Get(manifestURL)
	if err != nil {
		return "", fmt.Errorf("fetch manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("manifest returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read manifest: %w", err)
	}

	var m cliManifest
	if err := json.Unmarshal(body, &m); err != nil {
		return "", fmt.Errorf("parse manifest: %w", err)
	}

	version := strings.TrimPrefix(m.Version, "v")
	if version == "" {
		return "", fmt.Errorf("manifest has empty version")
	}

	versionCacheMu.Lock()
	cachedVersion = version
	cacheExpiry = time.Now().Add(versionCacheTTL)
	versionCacheMu.Unlock()

	return version, nil
}

// ServeInstallSH serves the bash install script.
func (h *Handler) ServeInstallSH(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, max-age=0")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, installSH)
}

// ServeInstallPS1 serves the PowerShell install script.
func (h *Handler) ServeInstallPS1(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, max-age=0")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, installPS1)
}

// ServeLatestCLIVersion returns the latest CLI version as plain text.
func (h *Handler) ServeLatestCLIVersion(w http.ResponseWriter, r *http.Request) {
	version, err := fetchLatestCLIVersion()
	if err != nil {
		slog.Error("failed to fetch latest CLI version", "error", err)
		http.Error(w, "failed to determine latest version", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=60")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, version+"\n")
}
