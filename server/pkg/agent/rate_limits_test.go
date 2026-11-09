package agent

import (
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func timePointer(t *testing.T, value string) *time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		t.Fatal(err)
	}
	parsed = parsed.UTC()
	return &parsed
}

func TestParseClaudeRateLimitEvent(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		info string
		want []RateLimitWindow
	}{
		{
			// Captured from Claude Code 2.1.319 on a weekly-only plan.
			name: "weekly_only_unified_windows",
			info: `{"status":"allowed","resetsAt":1794808800,"rateLimitType":"seven_day","overageStatus":"rejected","overageDisabledReason":"org_level_disabled","isUsingOverage":false,"unifiedWindows":{"seven_day":{"utilization":0,"resetsAt":1794808800,"status":"allowed"}}}`,
			want: []RateLimitWindow{{ID: "seven_day", WindowMinutes: 10080, UsedPercent: 0, ResetsAt: timePointer(t, "2026-11-16T06:00:00Z")}},
		},
		{
			name: "session_and_weekly_fractions_become_percent",
			info: `{"status":"allowed_warning","rateLimitType":"five_hour","utilization":0.82,"resetsAt":1794700800,"unifiedWindows":{"seven_day":{"utilization":0.11,"resetsAt":1794808800},"five_hour":{"utilization":0.82,"resetsAt":1794700800}}}`,
			want: []RateLimitWindow{
				{ID: "five_hour", WindowMinutes: 300, UsedPercent: 82, ResetsAt: timePointer(t, "2026-11-15T00:00:00Z")},
				{ID: "seven_day", WindowMinutes: 10080, UsedPercent: 11, ResetsAt: timePointer(t, "2026-11-16T06:00:00Z")},
			},
		},
		{
			name: "older_cli_without_unified_windows",
			info: `{"status":"allowed","rateLimitType":"seven_day_opus","utilization":0.5,"resetsAt":1794808800}`,
			want: []RateLimitWindow{{ID: "seven_day_opus", Label: "Opus", WindowMinutes: 10080, UsedPercent: 50, ResetsAt: timePointer(t, "2026-11-16T06:00:00Z")}},
		},
		{
			name: "status_without_utilization_is_ignored",
			info: `{"status":"allowed","rateLimitType":"five_hour","resetsAt":1794700800}`,
		},
		{name: "malformed", info: `{"unifiedWindows":[]}`},
		{name: "missing", info: ``},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ParseClaudeRateLimitEvent(json.RawMessage(tt.info))
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("windows = %#v, want %#v", got, tt.want)
			}
		})
	}
}

// claudeGetUsageFixture is a trimmed get_usage answer from Claude Code
// 2.1.319, with a session window added.
const claudeGetUsageFixture = `{
  "subscription_type": "max",
  "rate_limits_available": true,
  "rate_limits": {
    "five_hour": {"utilization": 15, "resets_at": "2026-11-09T18:00:00.5+00:00"},
    "seven_day": {"utilization": 12, "resets_at": "2026-11-16T06:00:00.271415+00:00", "limit_dollars": null},
    "seven_day_opus": null,
    "seven_day_sonnet": null,
    "extra_usage": {"is_enabled": false},
    "model_scoped": [{"display_name": "Fable", "utilization": 3, "resets_at": "2026-11-16T06:00:00+00:00"}]
  }
}`

func TestParseClaudeUsageResponse(t *testing.T) {
	t.Parallel()
	usage, err := ParseClaudeUsageResponse(json.RawMessage(claudeGetUsageFixture))
	if err != nil {
		t.Fatal(err)
	}
	want := ClaudeUsage{
		Plan:      "max",
		Available: true,
		Windows: []RateLimitWindow{
			{ID: "five_hour", WindowMinutes: 300, UsedPercent: 15, ResetsAt: timePointer(t, "2026-11-09T18:00:00.5Z")},
			{ID: "seven_day", WindowMinutes: 10080, UsedPercent: 12, ResetsAt: timePointer(t, "2026-11-16T06:00:00.271415Z")},
			{ID: "seven_day_fable", Label: "Fable", WindowMinutes: 10080, UsedPercent: 3, ResetsAt: timePointer(t, "2026-11-16T06:00:00Z")},
		},
	}
	if !reflect.DeepEqual(usage, want) {
		t.Fatalf("usage = %#v, want %#v", usage, want)
	}
}

func TestParseClaudeUsageResponseWithoutPlanLimits(t *testing.T) {
	t.Parallel()
	usage, err := ParseClaudeUsageResponse(json.RawMessage(`{"subscription_type":null,"rate_limits_available":false,"rate_limits":null}`))
	if err != nil {
		t.Fatal(err)
	}
	if usage.Available || len(usage.Windows) != 0 {
		t.Fatalf("usage = %#v, want unavailable with no windows", usage)
	}
}

func TestParseClaudeUsageProbeOutput(t *testing.T) {
	t.Parallel()
	initLine := `{"type":"control_response","response":{"subtype":"success","request_id":"multica-usage-init","response":{"account":{"email":"Person@Example.com","subscriptionType":"Claude Max"}}}}`
	usageLine := `{"type":"control_response","response":{"subtype":"success","request_id":"multica-usage","response":` + strings.Join(strings.Fields(claudeGetUsageFixture), "") + `}}`

	snapshot, err := parseClaudeUsageProbeOutput([]byte("not json\n" + initLine + "\n" + `{"type":"system"}` + "\n" + usageLine + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Provider != "claude" || snapshot.Plan != "max" || snapshot.Source != RateLimitSourceProbe || len(snapshot.Windows) != 3 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if snapshot.AccountID != RateLimitAccountID("claude", "person@example.com") || snapshot.AccountID == "" {
		t.Fatalf("account id = %q, want digest of the login email", snapshot.AccountID)
	}
	if strings.Contains(snapshot.AccountID, "example") {
		t.Fatalf("account id %q must not expose the email", snapshot.AccountID)
	}

	failed := `{"type":"control_response","response":{"subtype":"error","request_id":"multica-usage","error":"Unknown subtype: get_usage"}}`
	if _, err := parseClaudeUsageProbeOutput([]byte(initLine + "\n" + failed + "\n")); err == nil || !strings.Contains(err.Error(), "Unknown subtype") {
		t.Fatalf("error = %v, want the CLI's get_usage error", err)
	}
	if _, err := parseClaudeUsageProbeOutput([]byte(initLine + "\n")); err == nil {
		t.Fatal("missing get_usage response must fail")
	}
}

// codexRateLimitsReadFixture is a trimmed account/rateLimits/read result
// from codex-cli 0.167.0.
const codexRateLimitsReadFixture = `{
  "ordinaryUsageAllowed": true,
  "rateLimits": {
    "limitId": "codex",
    "primary": {"usedPercent": 51, "windowDurationMins": 10080, "resetsAt": 1794393146},
    "secondary": null,
    "credits": {"hasCredits": false, "unlimited": false, "balance": null},
    "planType": "self_serve_business_prolite"
  },
  "accountId": "5829f4ef-3a9c-438c-962f-b8fcbde3b4ed"
}`

func TestParseCodexRateLimits(t *testing.T) {
	t.Parallel()
	limits, ok := ParseCodexRateLimits(json.RawMessage(codexRateLimitsReadFixture))
	if !ok {
		t.Fatal("expected rate limits")
	}
	want := CodexRateLimits{
		AccountID: RateLimitAccountID("codex", "5829f4ef-3a9c-438c-962f-b8fcbde3b4ed"),
		Plan:      "self_serve_business_prolite",
		Windows:   []RateLimitWindow{{ID: "primary", WindowMinutes: 10080, UsedPercent: 51, ResetsAt: timePointer(t, "2026-11-11T10:32:26Z")}},
	}
	if !reflect.DeepEqual(limits, want) {
		t.Fatalf("limits = %#v, want %#v", limits, want)
	}

	notification := `{"rateLimits":{"primary":{"usedPercent":12.5,"windowDurationMins":300,"resetsAt":1794700800},"secondary":{"usedPercent":140,"windowDurationMins":10080,"resetsAt":1794808800}}}`
	limits, ok = ParseCodexRateLimits(json.RawMessage(notification))
	if !ok || limits.AccountID != "" || len(limits.Windows) != 2 {
		t.Fatalf("notification limits = %#v, ok=%v", limits, ok)
	}
	if limits.Windows[0].UsedPercent != 12.5 || limits.Windows[1].UsedPercent != 100 {
		t.Fatalf("used percent = %v / %v, want 12.5 / clamped 100", limits.Windows[0].UsedPercent, limits.Windows[1].UsedPercent)
	}

	for _, malformed := range []string{``, `{}`, `{"rateLimits":{"primary":null,"secondary":null}}`, `{"rateLimits":"x"}`} {
		if _, ok := ParseCodexRateLimits(json.RawMessage(malformed)); ok {
			t.Fatalf("%q must not parse as rate limits", malformed)
		}
	}
}

func TestCodexProbeOutputFindsResponseAcrossChunks(t *testing.T) {
	t.Parallel()
	output := newCodexProbeOutput(codexRateLimitsRequestID)
	stream := `{"id":1,"result":{"userAgent":"codex"}}` + "\n" +
		`{"method":"remoteControl/status/changed","params":{}}` + "\n" +
		`{"id":2,"result":` + strings.Join(strings.Fields(codexRateLimitsReadFixture), "") + "}\n"
	for start := 0; start < len(stream); start += 7 {
		end := min(start+7, len(stream))
		if _, err := output.Write([]byte(stream[start:end])); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-output.done:
	default:
		t.Fatal("done must close once the response arrives")
	}
	result, rpcError, answered := output.answer()
	if !answered || rpcError != "" {
		t.Fatalf("answered=%v rpcError=%q", answered, rpcError)
	}
	if _, ok := ParseCodexRateLimits(result); !ok {
		t.Fatalf("result does not parse: %s", result)
	}

	failed := newCodexProbeOutput(codexRateLimitsRequestID)
	_, _ = failed.Write([]byte(`{"id":2,"error":{"code":-32600,"message":"not logged in"}}` + "\n"))
	if _, rpcError, answered := failed.answer(); !answered || rpcError != "not logged in" {
		t.Fatalf("answered=%v rpcError=%q", answered, rpcError)
	}
}

func TestMergeRateLimitWindows(t *testing.T) {
	t.Parallel()
	base := []RateLimitWindow{{ID: "five_hour", UsedPercent: 10}, {ID: "seven_day", UsedPercent: 20}}
	got := MergeRateLimitWindows(base, []RateLimitWindow{{ID: "seven_day", UsedPercent: 25}, {ID: "seven_day_opus", UsedPercent: 5}})
	want := []RateLimitWindow{{ID: "five_hour", UsedPercent: 10}, {ID: "seven_day", UsedPercent: 25}, {ID: "seven_day_opus", UsedPercent: 5}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merged = %#v, want %#v", got, want)
	}
	if base[1].UsedPercent != 20 {
		t.Fatal("merge must not modify its input")
	}
}

func TestClaudeExecuteReportsRateLimitEvents(t *testing.T) {
	t.Parallel()
	event := json.RawMessage(`{"type":"rate_limit_event","rate_limit_info":{"status":"allowed","unifiedWindows":{"five_hour":{"utilization":0.4,"resetsAt":1794700800}}},"uuid":"u","session_id":"s"}`)
	result := json.RawMessage(`{"type":"result","subtype":"success","result":"done"}`)
	backend := claudeUsageFixtureBackend(t, []json.RawMessage{event}, result, false)

	var mu sync.Mutex
	var reports []RateLimitSnapshot
	executeClaudeUsageFixtureWithOptions(t, backend, 0, ExecOptions{
		Timeout: 5 * time.Second,
		OnRateLimits: func(snapshot RateLimitSnapshot) {
			mu.Lock()
			reports = append(reports, snapshot)
			mu.Unlock()
		},
	})
	mu.Lock()
	defer mu.Unlock()
	if len(reports) != 1 {
		t.Fatalf("reports = %#v, want one", reports)
	}
	got := reports[0]
	if got.Provider != "claude" || got.Source != RateLimitSourceRun || len(got.Windows) != 1 || got.Windows[0].ID != "five_hour" || got.Windows[0].UsedPercent != 40 {
		t.Fatalf("report = %#v", got)
	}
}

func TestCodexRateLimitsNotificationBypassesTurnGate(t *testing.T) {
	t.Parallel()
	c, _, _ := newTestCodexClient(t)
	c.acceptNotification = func(string, map[string]any) bool { return false }
	var reports []RateLimitSnapshot
	c.onRateLimits = func(snapshot RateLimitSnapshot) { reports = append(reports, snapshot) }

	c.handleLine(`{"jsonrpc":"2.0","method":"account/rateLimits/updated","params":{"rateLimits":{"primary":{"usedPercent":61,"windowDurationMins":10080,"resetsAt":1794393146},"planType":"pro"}}}`)

	if len(reports) != 1 {
		t.Fatalf("reports = %#v, want one", reports)
	}
	if got := reports[0]; got.Provider != "codex" || got.Plan != "pro" || got.Source != RateLimitSourceRun || got.Windows[0].UsedPercent != 61 {
		t.Fatalf("report = %#v", got)
	}
}
