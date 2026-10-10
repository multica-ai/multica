package providerusage

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const kimiUsageURL = "https://api.kimi.com/coding/v1/usages"

// KimiCollector reads Kimi Code plan windows with the CLI's OAuth file.
// An expired access token is left for the CLI to refresh: the last good
// snapshot stays, and this collector does not write a new token.
type KimiCollector struct {
	AuthPath string
	Do       HTTPDoer
	Now      func() time.Time
}

func (c KimiCollector) Collect(ctx context.Context) Result {
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	path := c.AuthPath
	if path == "" {
		path = kimiAuthPath()
	}
	token, expired, found := loadKimiToken(path, now)
	if !found {
		return emptyResult(ProviderKimi, ReasonNotLoggedIn, now)
	}
	if expired {
		return Result{Upload: false}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, kimiUsageURL, nil)
	if err != nil {
		return Result{Upload: false}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	body, status, retryAfter, err := performVendor(c.Do, req)
	if err != nil {
		return Result{Upload: false}
	}
	if status == http.StatusNotFound {
		return emptyResult(ProviderKimi, ReasonUnsupported, now)
	}
	reason, backoff, transient := classifyVendorStatus(status, retryAfter)
	if transient {
		return Result{Upload: false, Backoff: backoff}
	}
	if reason != "" {
		return emptyResult(ProviderKimi, reason, now)
	}
	windows, plan, ok := ParseKimiUsage(body)
	if !ok {
		return Result{Upload: false}
	}
	return Result{
		Upload: true,
		Snapshot: Snapshot{
			Provider:    ProviderKimi,
			PlanName:    plan,
			CollectedAt: now,
			Windows:     windows,
		},
	}
}

func kimiAuthPath() string {
	root := strings.TrimSpace(os.Getenv("KIMI_CODE_HOME"))
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		root = filepath.Join(home, ".kimi-code")
	}
	return filepath.Join(root, "credentials", "kimi-code.json")
}

func loadKimiToken(path string, now time.Time) (token string, expired, found bool) {
	body, err := readRegularFile(path)
	if err != nil {
		return "", false, false
	}
	root, ok := decodeObject(body)
	if !ok {
		return "", false, false
	}
	token, _ = root["access_token"].(string)
	token = strings.TrimSpace(token)
	if token == "" {
		return "", false, false
	}
	expires, hasExpiry := asFloat(root["expires_at"])
	if !hasExpiry || expires <= 0 {
		return "", false, false
	}
	if !time.Unix(int64(expires), 0).After(now) {
		return "", true, true
	}
	return token, false, true
}

// ParseKimiUsage maps the account summary and named limit rows onto
// weekly and rolling windows. Counts arrive as decimal strings.
func ParseKimiUsage(body []byte) (windows []Window, plan string, ok bool) {
	root, parsed := decodeObject(body)
	if !parsed {
		return nil, "", false
	}
	plan = kimiPlanName(root)
	if summary := asMap(root["usage"]); summary != nil {
		if window, added := kimiRow("weekly", summary); added {
			windows = append(windows, window)
		}
	}
	for _, item := range asSlice(root["limits"]) {
		entry := asMap(item)
		if entry == nil {
			continue
		}
		id, matched := kimiWindowID(asMap(entry["window"]))
		if !matched {
			continue
		}
		detail := asMap(entry["detail"])
		window, added := kimiRow(id, detail)
		if !added {
			continue
		}
		replaced := false
		for i := range windows {
			if windows[i].ID == window.ID {
				windows[i] = window
				replaced = true
				break
			}
		}
		if !replaced && len(windows) < 8 {
			windows = append(windows, window)
		}
	}
	return windows, plan, len(windows) > 0
}

func kimiPlanName(root map[string]any) string {
	user := asMap(root["user"])
	membership := asMap(user["membership"])
	level, _ := membership["level"].(string)
	level = strings.TrimSpace(level)
	level = strings.TrimPrefix(level, "LEVEL_")
	if level == "" {
		return ""
	}
	lower := strings.ToLower(level)
	return trimPlan(strings.ToUpper(lower[:1]) + lower[1:])
}

func kimiWindowID(window map[string]any) (string, bool) {
	if window == nil {
		return "", false
	}
	duration, ok := asFloat(window["duration"])
	if !ok || duration <= 0 {
		return "", false
	}
	unit, _ := window["timeUnit"].(string)
	switch unit {
	case "TIME_UNIT_MINUTE":
		if int(duration)%60 == 0 && int(duration)/60 == 5 {
			return "rolling", true
		}
	case "TIME_UNIT_HOUR":
		if duration == 5 {
			return "rolling", true
		}
	case "TIME_UNIT_WEEK":
		if duration == 1 {
			return "weekly", true
		}
	}
	return "", false
}

func kimiRow(id string, detail map[string]any) (Window, bool) {
	if detail == nil {
		return Window{}, false
	}
	limit, hasLimit := asFloat(detail["limit"])
	if !hasLimit || limit <= 0 {
		return Window{}, false
	}
	used, hasUsed := asFloat(detail["used"])
	if !hasUsed {
		remaining, hasRemaining := asFloat(detail["remaining"])
		if !hasRemaining || remaining < 0 || remaining > limit {
			return Window{}, false
		}
		used = limit - remaining
	}
	if used < 0 {
		used = 0
	}
	percent, ok := usablePercent(used / limit * 100)
	if !ok {
		return Window{}, false
	}
	return Window{ID: id, PercentUsed: percent, ResetsAt: parseResetValue(detail["resetTime"])}, true
}
