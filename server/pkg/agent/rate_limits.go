package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"sort"
	"strings"
	"time"
)

// RateLimitWindow is one subscription usage window, such as Claude's 5-hour
// session or a provider's weekly cap.
type RateLimitWindow struct {
	// ID is stable per provider ("five_hour", "seven_day", "primary", ...), so
	// a mid-run update replaces the window an idle probe reported.
	ID            string     `json:"id"`
	Label         string     `json:"label,omitempty"`
	WindowMinutes int        `json:"window_minutes,omitempty"`
	UsedPercent   float64    `json:"used_percent"`
	ResetsAt      *time.Time `json:"resets_at,omitempty"`
}

// RateLimitSnapshot is the latest known subscription usage of the account a
// provider CLI is logged in to.
type RateLimitSnapshot struct {
	Provider string `json:"provider"`
	// AccountID groups runtimes that share one login. It is a digest of the
	// provider's account identity, never the identity itself.
	AccountID string            `json:"account_id,omitempty"`
	Plan      string            `json:"plan,omitempty"`
	Device    string            `json:"device,omitempty"`
	Windows   []RateLimitWindow `json:"windows"`
	// UpdatedAt is when Windows were read; CheckedAt is the latest attempt.
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
	CheckedAt time.Time  `json:"checked_at"`
	// Source is "probe" for an idle check and "run" for a task's stream event.
	Source string `json:"source,omitempty"`
	// Error is the latest failed check. Windows then hold the previous numbers.
	Error string `json:"error,omitempty"`
}

const (
	RateLimitSourceProbe = "probe"
	RateLimitSourceRun   = "run"
)

// MergeRateLimitWindows returns base with every window in update replacing
// the one with the same ID. Stream events carry one or two windows, so a
// replace would drop the rest of the picture.
func MergeRateLimitWindows(base, update []RateLimitWindow) []RateLimitWindow {
	merged := make([]RateLimitWindow, 0, len(base)+len(update))
	index := make(map[string]int, len(base))
	for _, window := range base {
		index[window.ID] = len(merged)
		merged = append(merged, window)
	}
	for _, window := range update {
		if position, ok := index[window.ID]; ok {
			merged[position] = window
			continue
		}
		index[window.ID] = len(merged)
		merged = append(merged, window)
	}
	return merged
}

// RateLimitAccountID derives the grouping key for an account identity. The
// provider is part of the input so equal identities never collide across
// providers.
func RateLimitAccountID(provider, identity string) string {
	identity = strings.ToLower(strings.TrimSpace(identity))
	if identity == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(provider + "\x00" + identity))
	return hex.EncodeToString(sum[:8])
}

func clampPercent(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func epochSecondsToTime(value float64) *time.Time {
	if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return nil
	}
	t := time.Unix(int64(value), 0).UTC()
	return &t
}

func isoToTime(value string) *time.Time {
	if value == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil
	}
	t = t.UTC()
	return &t
}

var claudeWindowMinutes = map[string]int{
	"five_hour":                  5 * 60,
	"seven_day":                  7 * 24 * 60,
	"seven_day_opus":             7 * 24 * 60,
	"seven_day_sonnet":           7 * 24 * 60,
	"seven_day_overage_included": 7 * 24 * 60,
}

var claudeWindowLabels = map[string]string{
	"seven_day_opus":   "Opus",
	"seven_day_sonnet": "Sonnet",
}

// claudeUsageWindowOrder fixes the order of get_usage windows; the response
// is a JSON object, so map iteration would shuffle them.
var claudeUsageWindowOrder = []string{"five_hour", "seven_day", "seven_day_opus", "seven_day_sonnet"}

type claudeUnifiedWindow struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    float64  `json:"resetsAt"`
}

type claudeRateLimitInfo struct {
	RateLimitType  string                         `json:"rateLimitType"`
	Utilization    *float64                       `json:"utilization"`
	ResetsAt       float64                        `json:"resetsAt"`
	UnifiedWindows map[string]claudeUnifiedWindow `json:"unifiedWindows"`
}

// ParseClaudeRateLimitEvent reads the rate_limit_info of a stream-json
// rate_limit_event. Stream utilization is a 0-1 fraction. Windows the event
// does not name are left to MergeRateLimitWindows.
func ParseClaudeRateLimitEvent(raw json.RawMessage) []RateLimitWindow {
	var info claudeRateLimitInfo
	if len(raw) == 0 || json.Unmarshal(raw, &info) != nil {
		return nil
	}
	var windows []RateLimitWindow
	if len(info.UnifiedWindows) > 0 {
		ids := make([]string, 0, len(info.UnifiedWindows))
		for id := range info.UnifiedWindows {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			window := info.UnifiedWindows[id]
			if window.Utilization == nil {
				continue
			}
			windows = append(windows, claudeStreamWindow(id, *window.Utilization, window.ResetsAt))
		}
		return windows
	}
	if info.RateLimitType != "" && info.Utilization != nil {
		windows = append(windows, claudeStreamWindow(info.RateLimitType, *info.Utilization, info.ResetsAt))
	}
	return windows
}

func claudeStreamWindow(id string, utilization, resetsAt float64) RateLimitWindow {
	return RateLimitWindow{
		ID:            id,
		Label:         claudeWindowLabels[id],
		WindowMinutes: claudeWindowMinutes[id],
		UsedPercent:   clampPercent(utilization * 100),
		ResetsAt:      epochSecondsToTime(resetsAt),
	}
}

type claudeUsageWindow struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    string   `json:"resets_at"`
}

type claudeModelScopedWindow struct {
	DisplayName string   `json:"display_name"`
	Utilization *float64 `json:"utilization"`
	ResetsAt    string   `json:"resets_at"`
}

type claudeUsageResponse struct {
	SubscriptionType    string                     `json:"subscription_type"`
	RateLimitsAvailable bool                       `json:"rate_limits_available"`
	RateLimits          map[string]json.RawMessage `json:"rate_limits"`
}

// ClaudeUsage is the parsed answer to the get_usage control request.
type ClaudeUsage struct {
	Plan    string
	Windows []RateLimitWindow
	// Available is false when the login has no plan limits, as with an API key.
	Available bool
}

// ParseClaudeUsageResponse reads a get_usage control response. Its
// utilization values are already percentages.
func ParseClaudeUsageResponse(raw json.RawMessage) (ClaudeUsage, error) {
	var response claudeUsageResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return ClaudeUsage{}, err
	}
	usage := ClaudeUsage{Plan: response.SubscriptionType, Available: response.RateLimitsAvailable && response.RateLimits != nil}
	if !usage.Available {
		return usage, nil
	}
	for _, id := range claudeUsageWindowOrder {
		var window *claudeUsageWindow
		if json.Unmarshal(response.RateLimits[id], &window) != nil || window == nil || window.Utilization == nil {
			continue
		}
		usage.Windows = append(usage.Windows, RateLimitWindow{
			ID:            id,
			Label:         claudeWindowLabels[id],
			WindowMinutes: claudeWindowMinutes[id],
			UsedPercent:   clampPercent(*window.Utilization),
			ResetsAt:      isoToTime(window.ResetsAt),
		})
	}
	var scoped []claudeModelScopedWindow
	if json.Unmarshal(response.RateLimits["model_scoped"], &scoped) == nil {
		for _, window := range scoped {
			if window.DisplayName == "" || window.Utilization == nil {
				continue
			}
			usage.Windows = append(usage.Windows, RateLimitWindow{
				ID:            "seven_day_" + strings.ToLower(strings.Join(strings.Fields(window.DisplayName), "_")),
				Label:         window.DisplayName,
				WindowMinutes: 7 * 24 * 60,
				UsedPercent:   clampPercent(*window.Utilization),
				ResetsAt:      isoToTime(window.ResetsAt),
			})
		}
	}
	return usage, nil
}

type codexRateLimitWindow struct {
	UsedPercent        *float64 `json:"usedPercent"`
	WindowDurationMins int      `json:"windowDurationMins"`
	ResetsAt           float64  `json:"resetsAt"`
}

type codexRateLimits struct {
	PlanType  string                `json:"planType"`
	Primary   *codexRateLimitWindow `json:"primary"`
	Secondary *codexRateLimitWindow `json:"secondary"`
}

// CodexRateLimits is the parsed rate-limit state of a Codex login.
type CodexRateLimits struct {
	AccountID string
	Plan      string
	Windows   []RateLimitWindow
}

// ParseCodexRateLimits reads the result of account/rateLimits/read or the
// params of an account/rateLimits/updated notification. Both carry a
// rateLimits object; only the read result names the account.
func ParseCodexRateLimits(raw json.RawMessage) (CodexRateLimits, bool) {
	var payload struct {
		AccountID  string           `json:"accountId"`
		RateLimits *codexRateLimits `json:"rateLimits"`
	}
	if json.Unmarshal(raw, &payload) != nil || payload.RateLimits == nil {
		return CodexRateLimits{}, false
	}
	limits := CodexRateLimits{
		AccountID: RateLimitAccountID("codex", payload.AccountID),
		Plan:      payload.RateLimits.PlanType,
	}
	for _, entry := range []struct {
		id     string
		window *codexRateLimitWindow
	}{{"primary", payload.RateLimits.Primary}, {"secondary", payload.RateLimits.Secondary}} {
		if entry.window == nil || entry.window.UsedPercent == nil {
			continue
		}
		limits.Windows = append(limits.Windows, RateLimitWindow{
			ID:            entry.id,
			WindowMinutes: entry.window.WindowDurationMins,
			UsedPercent:   clampPercent(*entry.window.UsedPercent),
			ResetsAt:      epochSecondsToTime(entry.window.ResetsAt),
		})
	}
	return limits, len(limits.Windows) > 0
}
