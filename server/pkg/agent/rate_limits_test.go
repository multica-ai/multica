package agent

import (
	"encoding/json"
	"testing"
	"time"
)

func collectRateLimits(t *testing.T) (*rateLimitTracker, *[]RateLimitSnapshot) {
	t.Helper()
	var got []RateLimitSnapshot
	tr := newRateLimitTracker(func(s RateLimitSnapshot) { got = append(got, s) })
	tr.now = func() time.Time { return time.Date(2026, 11, 16, 3, 46, 0, 0, time.UTC) }
	return tr, &got
}

func windowByKind(t *testing.T, s RateLimitSnapshot, kind string) RateLimitWindow {
	t.Helper()
	for _, w := range s.Windows {
		if w.Kind == kind {
			return w
		}
	}
	t.Fatalf("window %q missing from %+v", kind, s.Windows)
	return RateLimitWindow{}
}

func TestParseClaudeRateLimitEventUnifiedWindows(t *testing.T) {
	// Shape emitted by Claude Code 2.1.x: the top level names the limiting
	// window, unifiedWindows carries every window.
	raw := json.RawMessage(`{
		"status": "allowed_warning",
		"resetsAt": 1795327200,
		"rateLimitType": "five_hour",
		"utilization": 0.72,
		"unifiedWindows": {
			"five_hour": {"utilization": 0.72, "resetsAt": 1795327200, "status": "allowed_warning"},
			"seven_day": {"utilization": 0.41, "resetsAt": 1795568400, "status": "allowed"},
			"seven_day_overage_included": {"utilization": 0.1, "resetsAt": 1795568400}
		}
	}`)
	update, ok := parseClaudeRateLimitEvent(raw)
	if !ok {
		t.Fatal("expected an update")
	}
	tr, got := collectRateLimits(t)
	tr.apply(update)
	if len(*got) != 1 {
		t.Fatalf("emits = %d, want 1", len(*got))
	}
	snap := (*got)[0]
	if snap.Status != RateLimitStatusWarning {
		t.Fatalf("status = %q", snap.Status)
	}
	if len(snap.Windows) != 2 {
		t.Fatalf("windows = %+v, want session and weekly only", snap.Windows)
	}
	if snap.Windows[0].Kind != RateLimitWindowSession || snap.Windows[1].Kind != RateLimitWindowWeekly {
		t.Fatalf("window order = %+v", snap.Windows)
	}
	session := windowByKind(t, snap, RateLimitWindowSession)
	if session.UsedPercent == nil || *session.UsedPercent < 71.99 || *session.UsedPercent > 72.01 {
		t.Fatalf("session used = %v", session.UsedPercent)
	}
	if session.WindowMinutes != 300 || session.Status != RateLimitStatusWarning {
		t.Fatalf("session = %+v", session)
	}
	if session.ResetsAt == nil || !session.ResetsAt.Equal(time.Unix(1795327200, 0)) {
		t.Fatalf("session reset = %v", session.ResetsAt)
	}
	weekly := windowByKind(t, snap, RateLimitWindowWeekly)
	if weekly.WindowMinutes != 7*24*60 || weekly.Status != RateLimitStatusAllowed {
		t.Fatalf("weekly = %+v", weekly)
	}
}

func TestParseClaudeRateLimitEventKeepsTopLevelRejection(t *testing.T) {
	// Claude Code 2.1.300: unifiedWindows carries only utilization/resetsAt and
	// the verdict lives at the top level, so a rejected account can still show
	// a window just under 100%.
	update, ok := parseClaudeRateLimitEvent(json.RawMessage(`{
		"status": "rejected",
		"resetsAt": 1795327200,
		"rateLimitType": "five_hour",
		"unifiedWindows": {
			"five_hour": {"utilization": 0.99, "resetsAt": 1795327200},
			"seven_day": {"utilization": 0.41, "resetsAt": 1795568400}
		}
	}`))
	if !ok {
		t.Fatal("expected an update")
	}
	tr, got := collectRateLimits(t)
	tr.apply(update)
	snap := (*got)[0]
	if snap.Status != RateLimitStatusRejected {
		t.Fatalf("status = %q", snap.Status)
	}
	session := windowByKind(t, snap, RateLimitWindowSession)
	if session.Status != RateLimitStatusRejected {
		t.Fatalf("session status = %q, want rejected", session.Status)
	}
	if session.UsedPercent == nil || *session.UsedPercent < 98.99 || *session.UsedPercent > 99.01 {
		t.Fatalf("session used = %v", session.UsedPercent)
	}
	if session.ResetsAt == nil || !session.ResetsAt.Equal(time.Unix(1795327200, 0)) {
		t.Fatalf("session reset = %v", session.ResetsAt)
	}
	if weekly := windowByKind(t, snap, RateLimitWindowWeekly); weekly.Status != "" {
		t.Fatalf("weekly status = %q, top-level verdict leaked to another window", weekly.Status)
	}
}

func TestParseClaudeRateLimitEventTopLevelFillsMissingUnifiedFields(t *testing.T) {
	update, _ := parseClaudeRateLimitEvent(json.RawMessage(`{
		"status": "allowed_warning",
		"resetsAt": 1795327200,
		"rateLimitType": "five_hour",
		"utilization": 0.85,
		"unifiedWindows": {"five_hour": {"status": "allowed"}}
	}`))
	if len(update.windows) != 1 {
		t.Fatalf("windows = %+v", update.windows)
	}
	w := update.windows[0]
	if w.Status != RateLimitStatusWarning || w.UsedPercent == nil || *w.UsedPercent < 84.99 || w.ResetsAt == nil {
		t.Fatalf("window = %+v", w)
	}
}

func TestParseClaudeRateLimitEventTopLevelOnly(t *testing.T) {
	// Older CLIs send only the limiting window, often without utilization.
	update, ok := parseClaudeRateLimitEvent(json.RawMessage(`{"status":"rejected","resetsAt":1795327200,"rateLimitType":"seven_day_opus"}`))
	if !ok {
		t.Fatal("expected an update")
	}
	if len(update.windows) != 1 {
		t.Fatalf("windows = %+v", update.windows)
	}
	w := update.windows[0]
	if w.Kind != RateLimitWindowWeeklyOpus || w.UsedPercent != nil || w.Status != RateLimitStatusRejected || w.ResetsAt == nil {
		t.Fatalf("window = %+v", w)
	}
	if update.status != RateLimitStatusRejected {
		t.Fatalf("status = %q", update.status)
	}
}

func TestParseClaudeRateLimitEventRejectsGarbage(t *testing.T) {
	for _, raw := range []string{``, `null`, `"x"`, `{}`, `{"rateLimitType":"overage","utilization":0.5}`} {
		if _, ok := parseClaudeRateLimitEvent(json.RawMessage(raw)); ok {
			t.Fatalf("%q produced an update", raw)
		}
	}
}

func TestRateLimitTrackerMergesWindowsAcrossEvents(t *testing.T) {
	tr, got := collectRateLimits(t)
	first, _ := parseClaudeRateLimitEvent(json.RawMessage(`{"status":"allowed","unifiedWindows":{"five_hour":{"utilization":0.2,"resetsAt":1795327200},"seven_day":{"utilization":0.4,"resetsAt":1795568400}}}`))
	tr.apply(first)
	second, _ := parseClaudeRateLimitEvent(json.RawMessage(`{"status":"allowed_warning","rateLimitType":"five_hour","utilization":0.8,"resetsAt":1795327200}`))
	tr.apply(second)
	if len(*got) != 2 {
		t.Fatalf("emits = %d", len(*got))
	}
	last := (*got)[1]
	if last.Status != RateLimitStatusWarning {
		t.Fatalf("status = %q", last.Status)
	}
	if used := windowByKind(t, last, RateLimitWindowSession).UsedPercent; used == nil || *used < 79.99 {
		t.Fatalf("session not updated: %v", used)
	}
	if used := windowByKind(t, last, RateLimitWindowWeekly).UsedPercent; used == nil || *used < 39.99 || *used > 40.01 {
		t.Fatalf("weekly lost by a partial event: %v", used)
	}
	// Each emit is an independent copy.
	if used := windowByKind(t, (*got)[0], RateLimitWindowSession).UsedPercent; *used > 20.01 {
		t.Fatalf("earlier snapshot mutated: %v", *used)
	}
}

func TestRateLimitTrackerNilIsInert(t *testing.T) {
	var tr *rateLimitTracker
	tr.apply(rateLimitUpdate{status: RateLimitStatusAllowed})
	if newRateLimitTracker(nil) != nil {
		t.Fatal("tracker without a callback should be nil")
	}
}

func codexParams(t *testing.T, payload string) map[string]any {
	t.Helper()
	var params map[string]any
	if err := json.Unmarshal([]byte(payload), &params); err != nil {
		t.Fatal(err)
	}
	return params
}

func TestParseCodexRateLimitsUpdated(t *testing.T) {
	params := codexParams(t, `{"rateLimits":{
		"limitId":"codex",
		"primary":{"usedPercent":18,"windowDurationMins":300,"resetsAt":1795331520},
		"secondary":{"usedPercent":93,"windowDurationMins":10080,"resetsAt":1795478400},
		"credits":{"hasCredits":true,"unlimited":false,"balance":"240"},
		"planType":"pro"
	}}`)
	update, ok := parseCodexRateLimitsUpdated(params)
	if !ok {
		t.Fatal("expected an update")
	}
	tr, got := collectRateLimits(t)
	tr.apply(update)
	snap := (*got)[0]
	if snap.PlanType != "pro" {
		t.Fatalf("plan = %q", snap.PlanType)
	}
	if snap.Credits == nil || !snap.Credits.HasCredits || snap.Credits.Balance != "240" {
		t.Fatalf("credits = %+v", snap.Credits)
	}
	if w := windowByKind(t, snap, RateLimitWindowSession); *w.UsedPercent != 18 || w.WindowMinutes != 300 {
		t.Fatalf("session = %+v", w)
	}
	if w := windowByKind(t, snap, RateLimitWindowWeekly); *w.UsedPercent != 93 || w.ResetsAt == nil {
		t.Fatalf("weekly = %+v", w)
	}
}

func TestParseCodexRateLimitsUpdatedSparse(t *testing.T) {
	tr, got := collectRateLimits(t)
	full, _ := parseCodexRateLimitsUpdated(codexParams(t, `{"rateLimits":{"primary":{"usedPercent":10,"windowDurationMins":300},"secondary":{"usedPercent":50,"windowDurationMins":10080},"planType":"plus"}}`))
	tr.apply(full)
	// A rolling update without plan metadata or the weekly window keeps both.
	sparse, ok := parseCodexRateLimitsUpdated(codexParams(t, `{"rateLimits":{"primary":{"usedPercent":12,"windowDurationMins":300},"secondary":null,"planType":null}}`))
	if !ok {
		t.Fatal("expected an update")
	}
	tr.apply(sparse)
	last := (*got)[len(*got)-1]
	if last.PlanType != "plus" {
		t.Fatalf("plan cleared by a sparse update: %q", last.PlanType)
	}
	if *windowByKind(t, last, RateLimitWindowSession).UsedPercent != 12 || *windowByKind(t, last, RateLimitWindowWeekly).UsedPercent != 50 {
		t.Fatalf("windows = %+v", last.Windows)
	}
}

func TestParseCodexRateLimitsUpdatedIgnoresOtherBuckets(t *testing.T) {
	if _, ok := parseCodexRateLimitsUpdated(codexParams(t, `{"rateLimits":{"limitId":"codex_spark","primary":{"usedPercent":90,"windowDurationMins":300}}}`)); ok {
		t.Fatal("a model-specific bucket must not overwrite the plan's windows")
	}
	if _, ok := parseCodexRateLimitsUpdated(codexParams(t, `{"other":1}`)); ok {
		t.Fatal("missing rateLimits produced an update")
	}
	// Without a duration, primary/secondary fall back to session/weekly.
	update, ok := parseCodexRateLimitsUpdated(codexParams(t, `{"rateLimits":{"primary":{"usedPercent":5},"secondary":{"usedPercent":6}}}`))
	if !ok || len(update.windows) != 2 || update.windows[0].Kind != RateLimitWindowSession || update.windows[1].Kind != RateLimitWindowWeekly {
		t.Fatalf("fallback kinds = %+v", update.windows)
	}
}
