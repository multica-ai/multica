package forkdist

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/mod/semver"
)

const latestVersionCacheTTL = 10 * time.Minute

var latestCache struct {
	sync.Mutex
	version string
	at      time.Time
	err     error
}

func LatestVersion(ctx context.Context) (string, error) {
	latestCache.Lock()
	defer latestCache.Unlock()
	if latestCache.version != "" && time.Since(latestCache.at) < latestVersionCacheTTL {
		return latestCache.version, nil
	}
	if latestCache.err != nil && time.Since(latestCache.at) < time.Minute {
		return "", latestCache.err
	}

	apiBase := strings.TrimRight(envOr("MULTICA_GITHUB_API_BASE", "https://api.github.com"), "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/repos/"+UpdateRepo()+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		latestCache.err = err
		latestCache.at = time.Now()
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("latest release returned HTTP %d", resp.StatusCode)
		latestCache.err = err
		latestCache.at = time.Now()
		return "", err
	}
	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	if !semver.IsValid(normalizeVersion(payload.TagName)) {
		return "", fmt.Errorf("latest release has invalid version %q", payload.TagName)
	}
	latestCache.version = payload.TagName
	latestCache.at = time.Now()
	latestCache.err = nil
	return payload.TagName, nil
}

func IsNewerVersion(latest, current string) bool {
	latest = normalizeVersion(latest)
	current = normalizeVersion(current)
	return semver.IsValid(latest) && semver.IsValid(current) && semver.Compare(latest, current) > 0
}

func normalizeVersion(version string) string {
	version = strings.TrimSpace(version)
	if version != "" && !strings.HasPrefix(version, "v") {
		version = "v" + version
	}
	return version
}

func resetLatestVersionCache() {
	latestCache.Lock()
	latestCache.version = ""
	latestCache.at = time.Time{}
	latestCache.err = nil
	latestCache.Unlock()
}
