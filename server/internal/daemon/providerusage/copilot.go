package providerusage

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const copilotUsageURL = "https://api.github.com/copilot_internal/user"

var copilotWindowOrder = []string{"premium_interactions", "chat", "completions"}

// TokenCommand returns a GitHub CLI token. Tests inject it so production
// never has to run `gh` during unit tests. The token stays in memory.
type TokenCommand func(ctx context.Context) (string, error)

// CopilotCollector reads GitHub Copilot quotas with the GitHub CLI session
// already on this machine. Environment API tokens are ignored: they are not
// the signed-in `gh` session.
type CopilotCollector struct {
	HostsPath string
	Token     TokenCommand
	Do        HTTPDoer
	Now       func() time.Time
}

func (c CopilotCollector) Collect(ctx context.Context) Result {
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	path := c.HostsPath
	if path == "" {
		path = copilotHostsPath()
	}
	token := copilotTokenFromHosts(path)
	if token == "" {
		read := c.Token
		if read == nil {
			read = runGHAuthToken
		}
		var err error
		token, err = read(ctx)
		token = strings.TrimSpace(token)
		if err != nil || token == "" || strings.Contains(token, "\n") {
			return emptyResult(ProviderCopilot, ReasonNotLoggedIn, now)
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, copilotUsageURL, nil)
	if err != nil {
		return Result{Upload: false}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	body, status, retryAfter, err := performVendor(c.Do, req)
	if err != nil {
		return Result{Upload: false}
	}
	reason, backoff, transient := classifyVendorStatus(status, retryAfter)
	if transient {
		return Result{Upload: false, Backoff: backoff}
	}
	if reason != "" {
		return emptyResult(ProviderCopilot, reason, now)
	}
	windows, plan, ok := ParseCopilotUsage(body)
	if !ok {
		return Result{Upload: false}
	}
	return Result{
		Upload: true,
		Snapshot: Snapshot{
			Provider:    ProviderCopilot,
			PlanName:    plan,
			CollectedAt: now,
			Windows:     windows,
		},
	}
}

func copilotHostsPath() string {
	dir := os.Getenv("GH_CONFIG_DIR")
	if strings.TrimSpace(dir) == "" {
		if runtime.GOOS == "windows" {
			dir = os.Getenv("AppData")
			if dir == "" {
				dir, _ = os.UserConfigDir()
			}
			dir = filepath.Join(dir, "GitHub CLI")
		} else {
			dir = os.Getenv("XDG_CONFIG_HOME")
			if strings.TrimSpace(dir) == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					return ""
				}
				dir = filepath.Join(home, ".config")
			}
			dir = filepath.Join(dir, "gh")
		}
	}
	return filepath.Join(dir, "hosts.yml")
}

func copilotTokenFromHosts(path string) string {
	body, err := readRegularFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(body), "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "github.com:" {
			start = i
			break
		}
	}
	if start < 0 {
		return ""
	}
	for _, line := range lines[start+1:] {
		if line != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			break
		}
		trimmed := strings.TrimSpace(line)
		const key = "oauth_token:"
		if !strings.HasPrefix(trimmed, key) {
			continue
		}
		value := strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, key)), `"'`)
		if value == "" || strings.ContainsAny(value, " \t\r") {
			return ""
		}
		return value
	}
	return ""
}

func runGHAuthToken(ctx context.Context) (string, error) {
	path, err := exec.LookPath("gh")
	if err != nil || !filepath.IsAbs(path) {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "auth", "token", "--hostname", "github.com")
	cmd.Stdin = bytes.NewReader(nil)
	var stdout bytes.Buffer
	cmd.Stdout = &limitedWriter{w: &stdout, n: 4 * 1024}
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(stdout.String()), nil
}

// ParseCopilotUsage maps quota_snapshots onto percent-used windows.
// Unlimited quotas are omitted. The token is not part of the result.
func ParseCopilotUsage(body []byte) (windows []Window, plan string, ok bool) {
	root, parsed := decodeObject(body)
	if !parsed {
		return nil, "", false
	}
	if name, isString := root["copilot_plan"].(string); isString {
		plan = trimPlan(name)
	}
	if plan == "" {
		if name, isString := root["plan"].(string); isString {
			plan = trimPlan(name)
		}
	}
	quotas := asMap(root["quota_snapshots"])
	if quotas == nil {
		return nil, "", false
	}
	seen := map[string]struct{}{}
	for _, id := range copilotWindowOrder {
		if window, added := copilotWindow(id, asMap(quotas[id]), root); added {
			windows = append(windows, window)
			seen[id] = struct{}{}
		}
	}
	var extra []string
	for id := range quotas {
		if _, already := seen[id]; already || !validCopilotID(id) {
			continue
		}
		extra = append(extra, id)
	}
	sort.Strings(extra)
	for _, id := range extra {
		if len(windows) >= 8 {
			break
		}
		if window, added := copilotWindow(id, asMap(quotas[id]), root); added {
			windows = append(windows, window)
		}
	}
	return windows, plan, len(windows) > 0
}

func copilotWindow(id string, quota, root map[string]any) (Window, bool) {
	if quota == nil || !validCopilotID(id) {
		return Window{}, false
	}
	if unlimited, isBool := quota["unlimited"].(bool); isBool && unlimited {
		return Window{}, false
	}
	entitlement, hasEntitlement := asFloat(quota["entitlement"])
	if !hasEntitlement || entitlement <= 0 {
		return Window{}, false
	}
	used, hasUsed := asFloat(quota["used"])
	if !hasUsed {
		if remaining, hasRemaining := asFloat(quota["remaining"]); hasRemaining {
			used = entitlement - remaining
		}
	}
	if used < 0 {
		used = 0
	}
	percent, ok := usablePercent(used / entitlement * 100)
	if !ok {
		return Window{}, false
	}
	window := Window{ID: id, PercentUsed: percent}
	reset := parseResetValue(quota["reset_date"])
	if reset == nil {
		reset = parseResetValue(quota["reset_at"])
	}
	if reset == nil {
		reset = parseResetValue(quota["resets_at"])
	}
	if reset == nil {
		reset = parseResetValue(root["quota_reset_date"])
	}
	window.ResetsAt = reset
	return window, true
}

func validCopilotID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if r >= 'a' && r <= 'z' || r == '_' || (r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}
