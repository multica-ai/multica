package agent

import (
	"encoding/json"
	"math"
	"sort"
	"sync"
	"time"
)

// RateLimitSnapshot is the subscription rate-limit state an agent CLI last
// reported for the account it runs under: how much of each limit window is
// used and when it resets. Backends observe it passively from the events the
// CLI already emits while a task runs; nothing here calls the provider.
type RateLimitSnapshot struct {
	// PlanType is the provider's plan identifier when it reports one (Codex:
	// "plus", "pro", ...). Empty when unknown.
	PlanType string `json:"plan_type,omitempty"`
	// Status is the provider's overall verdict: allowed, warning or rejected.
	// Empty when the provider does not state one.
	Status  string            `json:"status,omitempty"`
	Windows []RateLimitWindow `json:"windows"`
	Credits *RateLimitCredits `json:"credits,omitempty"`
	// ObservedAt is when the backend last merged an update into the snapshot.
	ObservedAt time.Time `json:"observed_at"`
}

// RateLimitWindow is one limit window, such as the 5-hour session or the
// weekly limit.
type RateLimitWindow struct {
	Kind string `json:"kind"`
	// UsedPercent is 0-100. Providers can report slightly more than 100 when
	// usage runs past the cap.
	UsedPercent   *float64   `json:"used_percent,omitempty"`
	WindowMinutes int64      `json:"window_minutes,omitempty"`
	ResetsAt      *time.Time `json:"resets_at,omitempty"`
	Status        string     `json:"status,omitempty"`
}

// RateLimitCredits is a prepaid balance that keeps the agent running after
// the plan's windows are exhausted (Codex credits).
type RateLimitCredits struct {
	HasCredits bool   `json:"has_credits"`
	Unlimited  bool   `json:"unlimited"`
	Balance    string `json:"balance,omitempty"`
}

// Rate-limit window kinds. The UI labels windows by kind, so a provider's own
// window names are mapped onto these rather than passed through.
const (
	RateLimitWindowSession      = "session"
	RateLimitWindowWeekly       = "weekly"
	RateLimitWindowWeeklyOpus   = "weekly_opus"
	RateLimitWindowWeeklySonnet = "weekly_sonnet"
	RateLimitWindowOther        = "other"
)

// Rate-limit statuses.
const (
	RateLimitStatusAllowed  = "allowed"
	RateLimitStatusWarning  = "warning"
	RateLimitStatusRejected = "rejected"
)

const (
	sessionWindowMinutes = 5 * 60
	weeklyWindowMinutes  = 7 * 24 * 60
)

var rateLimitWindowOrder = map[string]int{
	RateLimitWindowSession:      0,
	RateLimitWindowWeekly:       1,
	RateLimitWindowWeeklyOpus:   2,
	RateLimitWindowWeeklySonnet: 3,
	RateLimitWindowOther:        4,
}

// rateLimitTracker merges a CLI's incremental rate-limit events into one
// snapshot and hands every change to the caller's callback. Events arrive on
// the backend's stdout reader goroutine; the callback must not block (see
// ExecOptions.OnRateLimits).
type rateLimitTracker struct {
	mu       sync.Mutex
	emit     func(RateLimitSnapshot)
	now      func() time.Time
	planType string
	status   string
	credits  *RateLimitCredits
	windows  map[string]RateLimitWindow
}

func newRateLimitTracker(emit func(RateLimitSnapshot)) *rateLimitTracker {
	if emit == nil {
		return nil
	}
	return &rateLimitTracker{emit: emit, now: time.Now, windows: map[string]RateLimitWindow{}}
}

// rateLimitUpdate is one event's worth of new information. Zero values mean
// "not reported" and leave the previous value in place.
type rateLimitUpdate struct {
	planType string
	status   string
	credits  *RateLimitCredits
	windows  []RateLimitWindow
}

func (t *rateLimitTracker) apply(u rateLimitUpdate) {
	if t == nil {
		return
	}
	if u.planType == "" && u.status == "" && u.credits == nil && len(u.windows) == 0 {
		return
	}
	t.mu.Lock()
	if u.planType != "" {
		t.planType = u.planType
	}
	if u.status != "" {
		t.status = u.status
	}
	if u.credits != nil {
		credits := *u.credits
		t.credits = &credits
	}
	for _, w := range u.windows {
		t.windows[w.Kind] = w
	}
	snap := RateLimitSnapshot{
		PlanType:   t.planType,
		Status:     t.status,
		Windows:    make([]RateLimitWindow, 0, len(t.windows)),
		ObservedAt: t.now().UTC(),
	}
	if t.credits != nil {
		credits := *t.credits
		snap.Credits = &credits
	}
	for _, w := range t.windows {
		snap.Windows = append(snap.Windows, w)
	}
	t.mu.Unlock()

	sort.SliceStable(snap.Windows, func(i, j int) bool {
		a, b := snap.Windows[i], snap.Windows[j]
		if rateLimitWindowOrder[a.Kind] != rateLimitWindowOrder[b.Kind] {
			return rateLimitWindowOrder[a.Kind] < rateLimitWindowOrder[b.Kind]
		}
		return a.WindowMinutes < b.WindowMinutes
	})
	t.emit(snap)
}

func unixSecondsPtr(sec *int64) *time.Time {
	if sec == nil || *sec <= 0 {
		return nil
	}
	t := time.Unix(*sec, 0).UTC()
	return &t
}

func finitePercent(v float64) *float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return nil
	}
	return &v
}

// --- Claude Code -----------------------------------------------------------

// claudeRateLimitInfo is the `rate_limit_info` payload of Claude Code's
// stream-json `rate_limit_event`. The top-level fields describe the window
// currently limiting the account; `unifiedWindows` carries every window on
// every observation in newer CLIs. Utilization is a 0-1 fraction and resetsAt
// is unix seconds. API-key sessions never emit the event.
type claudeRateLimitInfo struct {
	Status         string                               `json:"status"`
	ResetsAt       *int64                               `json:"resetsAt"`
	RateLimitType  string                               `json:"rateLimitType"`
	Utilization    *float64                             `json:"utilization"`
	UnifiedWindows map[string]claudeRateLimitWindowInfo `json:"unifiedWindows"`
}

type claudeRateLimitWindowInfo struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    *int64   `json:"resetsAt"`
	Status      string   `json:"status"`
}

func claudeRateLimitStatus(s string) string {
	switch s {
	case "allowed":
		return RateLimitStatusAllowed
	case "allowed_warning":
		return RateLimitStatusWarning
	case "rejected":
		return RateLimitStatusRejected
	default:
		return ""
	}
}

// claudeRateLimitKind maps Claude's window names onto window kinds. Windows
// whose meaning the UI cannot label (overage buckets) are dropped.
func claudeRateLimitKind(name string) (kind string, minutes int64, ok bool) {
	switch name {
	case "five_hour":
		return RateLimitWindowSession, sessionWindowMinutes, true
	case "seven_day":
		return RateLimitWindowWeekly, weeklyWindowMinutes, true
	case "seven_day_opus":
		return RateLimitWindowWeeklyOpus, weeklyWindowMinutes, true
	case "seven_day_sonnet":
		return RateLimitWindowWeeklySonnet, weeklyWindowMinutes, true
	default:
		return "", 0, false
	}
}

func claudeRateLimitWindow(name string, utilization *float64, resetsAt *int64, status string) (RateLimitWindow, bool) {
	kind, minutes, ok := claudeRateLimitKind(name)
	if !ok {
		return RateLimitWindow{}, false
	}
	w := RateLimitWindow{
		Kind:          kind,
		WindowMinutes: minutes,
		ResetsAt:      unixSecondsPtr(resetsAt),
		Status:        claudeRateLimitStatus(status),
	}
	if utilization != nil {
		w.UsedPercent = finitePercent(*utilization * 100)
	}
	if w.UsedPercent == nil && w.ResetsAt == nil && w.Status == "" {
		return RateLimitWindow{}, false
	}
	return w, true
}

// mergeClaudeRateLimitWindow combines the top-level summary of the limiting
// window with its unifiedWindows entry. Usage and reset come from the
// per-window view when present; the status stays the top-level one, which is
// the CLI's verdict for that window (unifiedWindows often carries no status).
func mergeClaudeRateLimitWindow(top, unified RateLimitWindow) RateLimitWindow {
	merged := unified
	if merged.UsedPercent == nil {
		merged.UsedPercent = top.UsedPercent
	}
	if merged.ResetsAt == nil {
		merged.ResetsAt = top.ResetsAt
	}
	if top.Status != "" {
		merged.Status = top.Status
	}
	return merged
}

// parseClaudeRateLimitEvent converts one rate_limit_info payload into an
// update. The top-level status applies only to the window rateLimitType
// names; other windows keep their own.
func parseClaudeRateLimitEvent(raw json.RawMessage) (rateLimitUpdate, bool) {
	if len(raw) == 0 {
		return rateLimitUpdate{}, false
	}
	var info claudeRateLimitInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		return rateLimitUpdate{}, false
	}
	u := rateLimitUpdate{status: claudeRateLimitStatus(info.Status)}
	byKind := map[string]RateLimitWindow{}
	if w, ok := claudeRateLimitWindow(info.RateLimitType, info.Utilization, info.ResetsAt, info.Status); ok {
		byKind[w.Kind] = w
	}
	for name, win := range info.UnifiedWindows {
		w, ok := claudeRateLimitWindow(name, win.Utilization, win.ResetsAt, win.Status)
		if !ok {
			continue
		}
		if top, seen := byKind[w.Kind]; seen {
			w = mergeClaudeRateLimitWindow(top, w)
		}
		byKind[w.Kind] = w
	}
	for _, w := range byKind {
		u.windows = append(u.windows, w)
	}
	return u, u.status != "" || len(u.windows) > 0
}

// --- Codex -----------------------------------------------------------------

// codexRateLimitSnapshot is the `rateLimits` payload of Codex app-server's
// `account/rateLimits/updated` notification. Updates are sparse: a null field
// keeps the previously observed value. usedPercent is 0-100 and resetsAt is
// unix seconds.
type codexRateLimitSnapshot struct {
	LimitID   *string               `json:"limitId"`
	Primary   *codexRateLimitWindow `json:"primary"`
	Secondary *codexRateLimitWindow `json:"secondary"`
	Credits   *struct {
		HasCredits bool    `json:"hasCredits"`
		Unlimited  bool    `json:"unlimited"`
		Balance    *string `json:"balance"`
	} `json:"credits"`
	PlanType *string `json:"planType"`
}

type codexRateLimitWindow struct {
	UsedPercent        *float64 `json:"usedPercent"`
	WindowDurationMins *int64   `json:"windowDurationMins"`
	ResetsAt           *int64   `json:"resetsAt"`
}

// codexMainRateLimitID is the limit bucket the plan's session/weekly windows
// belong to. Model-specific buckets carry their own ids and are not shown.
const codexMainRateLimitID = "codex"

func codexRateLimitKind(minutes int64, fallback string) string {
	switch {
	case minutes <= 0:
		return fallback
	case minutes <= 24*60:
		return RateLimitWindowSession
	case minutes >= 6*24*60 && minutes <= 8*24*60:
		return RateLimitWindowWeekly
	default:
		return RateLimitWindowOther
	}
}

func codexRateLimitWindowFrom(w *codexRateLimitWindow, fallback string) (RateLimitWindow, bool) {
	if w == nil || w.UsedPercent == nil {
		return RateLimitWindow{}, false
	}
	var minutes int64
	if w.WindowDurationMins != nil {
		minutes = *w.WindowDurationMins
	}
	used := finitePercent(*w.UsedPercent)
	if used == nil {
		return RateLimitWindow{}, false
	}
	return RateLimitWindow{
		Kind:          codexRateLimitKind(minutes, fallback),
		UsedPercent:   used,
		WindowMinutes: minutes,
		ResetsAt:      unixSecondsPtr(w.ResetsAt),
	}, true
}

// parseCodexRateLimitsUpdated converts the notification params into an
// update. Primary is the short window and secondary the weekly one; the
// duration decides when Codex reports it.
func parseCodexRateLimitsUpdated(params map[string]any) (rateLimitUpdate, bool) {
	raw, ok := params["rateLimits"]
	if !ok || raw == nil {
		return rateLimitUpdate{}, false
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return rateLimitUpdate{}, false
	}
	var snap codexRateLimitSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return rateLimitUpdate{}, false
	}
	if snap.LimitID != nil && *snap.LimitID != "" && *snap.LimitID != codexMainRateLimitID {
		return rateLimitUpdate{}, false
	}
	var u rateLimitUpdate
	if snap.PlanType != nil {
		u.planType = *snap.PlanType
	}
	if snap.Credits != nil {
		u.credits = &RateLimitCredits{HasCredits: snap.Credits.HasCredits, Unlimited: snap.Credits.Unlimited}
		if snap.Credits.Balance != nil {
			u.credits.Balance = *snap.Credits.Balance
		}
	}
	if w, ok := codexRateLimitWindowFrom(snap.Primary, RateLimitWindowSession); ok {
		u.windows = append(u.windows, w)
	}
	if w, ok := codexRateLimitWindowFrom(snap.Secondary, RateLimitWindowWeekly); ok {
		u.windows = append(u.windows, w)
	}
	return u, u.planType != "" || u.credits != nil || len(u.windows) > 0
}
