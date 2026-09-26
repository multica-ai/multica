package providerusage

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const openCodeUsageURL = "https://opencode.ai/zen/go/v1/usage"

var openCodeWindows = []string{"rolling", "weekly", "monthly"}

// OpenCodeCollector reads OpenCode Go plan windows with the opencode-go key
// the CLI stored at sign-in. Other vendor keys in the same file are ignored.
type OpenCodeCollector struct {
	AuthPath string
	Do       HTTPDoer
	Now      func() time.Time
}

func (c OpenCodeCollector) Collect(ctx context.Context) Result {
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	path := c.AuthPath
	if path == "" {
		path = openCodeAuthPath()
	}
	token, found := loadOpenCodeToken(path)
	if !found {
		return emptyResult(ProviderOpenCode, ReasonNotLoggedIn, now)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, openCodeUsageURL, nil)
	if err != nil {
		return Result{Upload: false}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	body, status, retryAfter, err := performVendor(c.Do, req)
	if err != nil {
		return Result{Upload: false}
	}
	if status == http.StatusForbidden {
		return emptyResult(ProviderOpenCode, ReasonUnsupported, now)
	}
	reason, backoff, transient := classifyVendorStatus(status, retryAfter)
	if transient {
		return Result{Upload: false, Backoff: backoff}
	}
	if reason != "" {
		return emptyResult(ProviderOpenCode, reason, now)
	}
	windows, ok := ParseOpenCodeUsage(body)
	if !ok {
		return Result{Upload: false}
	}
	return Result{
		Upload: true,
		Snapshot: Snapshot{
			Provider:    ProviderOpenCode,
			PlanName:    "Go",
			CollectedAt: now,
			Windows:     windows,
		},
	}
}

func openCodeAuthPath() string {
	var candidates []string
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".local", "share", "opencode", "auth.json"))
		if runtime.GOOS == "darwin" {
			candidates = append(candidates, filepath.Join(home, "Library", "Application Support", "opencode", "auth.json"))
		}
	}
	if runtime.GOOS == "windows" {
		if dir, err := os.UserConfigDir(); err == nil {
			candidates = append(candidates, filepath.Join(dir, "opencode", "auth.json"))
		}
	}
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0]
}

func loadOpenCodeToken(path string) (string, bool) {
	body, err := readRegularFile(path)
	if err != nil {
		return "", false
	}
	root, ok := decodeObject(body)
	if !ok {
		return "", false
	}
	entry, exists := root["opencode-go"]
	if !exists {
		return "", false
	}
	if token, isString := entry.(string); isString && strings.TrimSpace(token) != "" {
		return strings.TrimSpace(token), true
	}
	object := asMap(entry)
	for _, key := range []string{"key", "apiKey", "api_key", "token", "accessToken"} {
		token := stringField(object, key)
		if token != "" {
			return token, true
		}
	}
	return "", false
}

// ParseOpenCodeUsage maps Go plan windows. percent is already "percent used".
func ParseOpenCodeUsage(body []byte) ([]Window, bool) {
	root, ok := decodeObject(body)
	if !ok {
		return nil, false
	}
	usage := asMap(root["usage"])
	if usage == nil {
		return nil, false
	}
	var windows []Window
	for _, id := range openCodeWindows {
		entry := asMap(usage[id])
		percent, hasPercent := asFloat(entry["percent"])
		if !hasPercent {
			continue
		}
		value, usable := usablePercent(percent)
		if !usable {
			continue
		}
		windows = append(windows, Window{
			ID:          id,
			PercentUsed: value,
			ResetsAt:    parseResetValue(entry["resetsAt"]),
		})
	}
	return windows, len(windows) > 0
}
