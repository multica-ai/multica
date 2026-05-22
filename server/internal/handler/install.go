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
	prodManifestURL        = "https://multica.obs.cn-east-3.myhuaweicloud.com/cli/manifest.json"
	testManifestURL        = "https://multica.obs.cn-east-3.myhuaweicloud.com/cli-test/manifest.json"
	cachedVersionByChannel = map[string]string{}
	cacheExpiryByChannel   = map[string]time.Time{}
	versionCacheMu         sync.RWMutex
)

func normalizeCLIChannel(channel string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(channel)) {
	case "", "prod", "production":
		return "prod", nil
	case "test":
		return "test", nil
	default:
		return "", fmt.Errorf("unsupported channel %q", channel)
	}
}

func manifestURLForChannel(channel string) string {
	if channel == "test" {
		return testManifestURL
	}
	return prodManifestURL
}

func fetchLatestCLIVersion(channel string) (string, error) {
	channel, err := normalizeCLIChannel(channel)
	if err != nil {
		return "", err
	}

	versionCacheMu.RLock()
	if expiresAt := cacheExpiryByChannel[channel]; time.Now().Before(expiresAt) && cachedVersionByChannel[channel] != "" {
		v := cachedVersionByChannel[channel]
		versionCacheMu.RUnlock()
		return v, nil
	}
	versionCacheMu.RUnlock()

	client := &http.Client{Timeout: manifestFetchTime}
	resp, err := client.Get(manifestURLForChannel(channel))
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
	cachedVersionByChannel[channel] = version
	cacheExpiryByChannel[channel] = time.Now().Add(versionCacheTTL)
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
	channel, err := normalizeCLIChannel(r.URL.Query().Get("channel"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	version, err := fetchLatestCLIVersion(channel)
	if err != nil {
		slog.Error("failed to fetch latest CLI version", "channel", channel, "error", err)
		http.Error(w, "failed to determine latest version", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=60")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, version+"\n")
}
