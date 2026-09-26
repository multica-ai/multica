package providerusage

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const antigravityQuotaURL = "https://daily-cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary"

var antigravityWindowOrder = []string{
	"gemini_hourly",
	"gemini_weekly",
	"third_party_hourly",
	"third_party_weekly",
}

// AntigravityCollector reads Cloud Code quota with the OAuth file Gemini /
// Antigravity already wrote. It does not prompt the keychain or scan for a
// local language-server port. An expired token is left for the CLI to refresh.
type AntigravityCollector struct {
	AuthPath string
	Do       HTTPDoer
	Now      func() time.Time
}

func (c AntigravityCollector) Collect(ctx context.Context) Result {
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	path := c.AuthPath
	if path == "" {
		path = antigravityAuthPath()
	}
	token, project, expired, found := loadAntigravityToken(path, now)
	if !found {
		return emptyResult(ProviderAntigravity, ReasonSessionUnavailable, now)
	}
	if expired {
		return Result{Upload: false}
	}
	payload := map[string]string{}
	if project != "" {
		payload["project"] = project
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return Result{Upload: false}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, antigravityQuotaURL, bytes.NewReader(raw))
	if err != nil {
		return Result{Upload: false}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Client-Metadata", "ideType=IDE_UNSPECIFIED,platform=PLATFORM_UNSPECIFIED,pluginType=GEMINI")
	body, status, retryAfter, err := performVendor(c.Do, req)
	if err != nil {
		return Result{Upload: false}
	}
	reason, backoff, transient := classifyVendorStatus(status, retryAfter)
	if transient {
		return Result{Upload: false, Backoff: backoff}
	}
	if reason != "" {
		return emptyResult(ProviderAntigravity, reason, now)
	}
	windows, plan, ok := ParseAntigravityQuota(body, now)
	if !ok {
		return Result{Upload: false}
	}
	return Result{
		Upload: true,
		Snapshot: Snapshot{
			Provider:    ProviderAntigravity,
			PlanName:    plan,
			CollectedAt: now,
			Windows:     windows,
		},
	}
}

func antigravityAuthPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".gemini", "oauth_creds.json")
}

func loadAntigravityToken(path string, now time.Time) (token, project string, expired, found bool) {
	body, err := readRegularFile(path)
	if err != nil {
		return "", "", false, false
	}
	root, ok := decodeObject(body)
	if !ok {
		return "", "", false, false
	}
	token = strings.TrimSpace(stringField(root, "access_token"))
	if token == "" {
		return "", "", false, false
	}
	project = stringField(root, "projectId")
	if project == "" {
		project = stringField(root, "project_id")
	}
	if expiry, hasExpiry := asFloat(root["expiry_date"]); hasExpiry && expiry > 0 {
		when := time.UnixMilli(int64(expiry))
		if !when.After(now) {
			return "", project, true, true
		}
	}
	return token, project, false, true
}

// ParseAntigravityQuota reduces grouped or per-model quota buckets to the
// four windows the runtime page can show. Remaining fraction is converted
// to percent used.
func ParseAntigravityQuota(body []byte, now time.Time) (windows []Window, plan string, ok bool) {
	root, parsed := decodeObject(body)
	if !parsed {
		return nil, "", false
	}
	plan = antigravityPlan(root)
	best := map[string]Window{}
	groups := antigravityGroups(root)
	if len(groups) > 0 {
		for _, item := range groups {
			group := asMap(item)
			family := antigravityFamily(stringField(group, "displayName"))
			for _, bucketItem := range asSlice(group["buckets"]) {
				bucket := asMap(bucketItem)
				bucketFamily := family
				if bucketFamily == "" {
					bucketFamily = antigravityFamily(strings.Join([]string{
						stringField(bucket, "modelId"),
						stringField(bucket, "name"),
						stringField(bucket, "bucketId"),
					}, " "))
				}
				antigravityConsider(best, bucket, bucketFamily, now)
			}
		}
	} else {
		for _, bucketItem := range asSlice(root["buckets"]) {
			bucket := asMap(bucketItem)
			family := antigravityFamily(strings.Join([]string{
				stringField(bucket, "modelId"),
				stringField(bucket, "name"),
				stringField(bucket, "bucketId"),
			}, " "))
			antigravityConsider(best, bucket, family, now)
		}
	}
	for _, id := range antigravityWindowOrder {
		if window, exists := best[id]; exists {
			windows = append(windows, window)
		}
	}
	return windows, plan, len(windows) > 0
}

func antigravityGroups(root map[string]any) []any {
	for _, key := range []string{"groups", "quotaGroups"} {
		if items := asSlice(root[key]); len(items) > 0 {
			return items
		}
	}
	for _, key := range []string{"response", "summary"} {
		if items := asSlice(asMap(root[key])["groups"]); len(items) > 0 {
			return items
		}
	}
	return nil
}

func antigravityPlan(root map[string]any) string {
	if name := stringField(asMap(root["currentTier"]), "name"); name != "" {
		return trimPlan(name)
	}
	return ""
}

func antigravityConsider(best map[string]Window, bucket map[string]any, family string, now time.Time) {
	if bucket == nil || family == "" {
		return
	}
	if disabled, isBool := bucket["disabled"].(bool); isBool && disabled {
		return
	}
	model := strings.ToLower(stringField(bucket, "modelId"))
	if strings.HasPrefix(model, "chat_") {
		return
	}
	percent, hasPercent := antigravityPercent(bucket)
	if !hasPercent {
		return
	}
	cadence := antigravityCadence(bucket, now)
	id := family + "_" + cadence
	window := Window{ID: id, PercentUsed: percent, ResetsAt: parseResetValue(bucket["resetTime"])}
	current, exists := best[id]
	if !exists || window.PercentUsed > current.PercentUsed {
		best[id] = window
	}
}

func antigravityPercent(bucket map[string]any) (float64, bool) {
	if fraction, ok := antigravityFraction(bucket); ok {
		return usablePercent((1 - fraction) * 100)
	}
	limit, hasLimit := asFloat(bucket["limit"])
	used, hasUsed := asFloat(bucket["used"])
	if !hasLimit || !hasUsed || limit <= 0 || used < 0 || used > limit*1.5 {
		return 0, false
	}
	return usablePercent(used / limit * 100)
}

func antigravityFraction(bucket map[string]any) (float64, bool) {
	if fraction, ok := asFloat(bucket["remainingFraction"]); ok && fraction >= 0 && fraction <= 1 {
		return fraction, true
	}
	remaining := asMap(bucket["remaining"])
	if remaining == nil {
		return 0, false
	}
	if fraction, ok := asFloat(remaining["remainingFraction"]); ok && fraction >= 0 && fraction <= 1 {
		return fraction, true
	}
	if stringField(remaining, "case") == "remainingFraction" {
		if fraction, ok := asFloat(remaining["value"]); ok && fraction >= 0 && fraction <= 1 {
			return fraction, true
		}
	}
	return 0, false
}

func antigravityFamily(text string) string {
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "gemini"):
		return "gemini"
	case strings.Contains(lower, "claude") || strings.Contains(lower, "gpt") || strings.Contains(lower, "openai"):
		return "third_party"
	default:
		return ""
	}
}

func antigravityCadence(bucket map[string]any, now time.Time) string {
	for _, key := range []string{"window", "bucketId", "displayName", "name"} {
		lower := strings.ToLower(stringField(bucket, key))
		if lower == "" {
			continue
		}
		if strings.Contains(lower, "week") {
			return "weekly"
		}
		if strings.Contains(lower, "hour") || strings.Contains(lower, "5h") || strings.Contains(lower, "session") {
			return "hourly"
		}
	}
	if reset := parseResetValue(bucket["resetTime"]); reset != nil && reset.Sub(now) > 24*time.Hour {
		return "weekly"
	}
	return "hourly"
}
