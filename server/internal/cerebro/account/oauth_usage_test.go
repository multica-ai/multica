package account

import (
	"errors"
	"testing"
	"time"
)

func TestParseOAuthUsageResponse(t *testing.T) {
	body := []byte(`{
		"five_hour": {"utilization": 42.5, "resets_at": "2026-07-12T21:00:00Z"},
		"seven_day": {"utilization": 87, "resets_at": "2026-07-16T09:00:00+02:00"},
		"extra_usage": {"is_enabled": false, "used_credits": 0, "monthly_limit": 0},
		"limits": [{"kind": "weekly_scoped", "percent": 12}]
	}`)
	snap, err := ParseOAuthUsageResponse(body)
	if err != nil {
		t.Fatalf("ParseOAuthUsageResponse: %v", err)
	}
	if snap.FiveHourPct == nil || *snap.FiveHourPct != 42.5 {
		t.Fatalf("FiveHourPct = %v, want 42.5", snap.FiveHourPct)
	}
	if snap.FiveHourResetsAt == nil || !snap.FiveHourResetsAt.Equal(time.Date(2026, 7, 12, 21, 0, 0, 0, time.UTC)) {
		t.Fatalf("FiveHourResetsAt = %v", snap.FiveHourResetsAt)
	}
	if snap.SevenDayPct == nil || *snap.SevenDayPct != 87 {
		t.Fatalf("SevenDayPct = %v, want 87", snap.SevenDayPct)
	}
	if snap.SevenDayResetsAt == nil || !snap.SevenDayResetsAt.Equal(time.Date(2026, 7, 16, 7, 0, 0, 0, time.UTC)) {
		t.Fatalf("SevenDayResetsAt = %v", snap.SevenDayResetsAt)
	}
	if !snap.HasSignal() {
		t.Fatal("HasSignal() = false, want true")
	}
}

func TestParseOAuthUsageResponseMissingWindows(t *testing.T) {
	snap, err := ParseOAuthUsageResponse([]byte(`{}`))
	if err != nil {
		t.Fatalf("ParseOAuthUsageResponse: %v", err)
	}
	if snap.FiveHourPct != nil || snap.SevenDayPct != nil {
		t.Fatalf("expected empty snapshot, got %+v", snap)
	}
	if snap.HasSignal() {
		t.Fatal("HasSignal() = true, want false")
	}
}

func TestParseOAuthUsageResponseClampsAndDropsBadResets(t *testing.T) {
	body := []byte(`{
		"five_hour": {"utilization": 130.2, "resets_at": "not-a-timestamp"},
		"seven_day": {"utilization": -4}
	}`)
	snap, err := ParseOAuthUsageResponse(body)
	if err != nil {
		t.Fatalf("ParseOAuthUsageResponse: %v", err)
	}
	if snap.FiveHourPct == nil || *snap.FiveHourPct != 100 {
		t.Fatalf("FiveHourPct = %v, want clamped 100", snap.FiveHourPct)
	}
	if snap.FiveHourResetsAt != nil {
		t.Fatalf("FiveHourResetsAt = %v, want nil for unparseable timestamp", snap.FiveHourResetsAt)
	}
	if snap.SevenDayPct == nil || *snap.SevenDayPct != 0 {
		t.Fatalf("SevenDayPct = %v, want clamped 0", snap.SevenDayPct)
	}
}

func TestParseOAuthUsageResponseInvalidJSON(t *testing.T) {
	if _, err := ParseOAuthUsageResponse([]byte("not json")); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseClaudeCredentials(t *testing.T) {
	now := time.Date(2026, 7, 12, 20, 0, 0, 0, time.UTC)
	valid := []byte(`{"claudeAiOauth": {"accessToken": "sk-ant-oat01-abc", "refreshToken": "r", "expiresAt": ` +
		"1783929600000" + `}}`) // 2026-07-13 far side of now
	token, err := ParseClaudeCredentials(valid, now)
	if err != nil {
		t.Fatalf("ParseClaudeCredentials: %v", err)
	}
	if token != "sk-ant-oat01-abc" {
		t.Fatalf("token = %q", token)
	}
}

func TestParseClaudeCredentialsExpired(t *testing.T) {
	now := time.Date(2026, 7, 12, 20, 0, 0, 0, time.UTC)
	expired := []byte(`{"claudeAiOauth": {"accessToken": "sk-ant-oat01-abc", "expiresAt": 1000}}`)
	if _, err := ParseClaudeCredentials(expired, now); !errors.Is(err, ErrOAuthTokenUnavailable) {
		t.Fatalf("err = %v, want ErrOAuthTokenUnavailable", err)
	}
}

func TestParseClaudeCredentialsMissingToken(t *testing.T) {
	if _, err := ParseClaudeCredentials([]byte(`{"claudeAiOauth": {}}`), time.Now()); !errors.Is(err, ErrOAuthTokenUnavailable) {
		t.Fatalf("err = %v, want ErrOAuthTokenUnavailable", err)
	}
}

func TestParseClaudeCredentialsNoExpiryIsAccepted(t *testing.T) {
	// Some credential stores omit expiresAt entirely; the token is used as-is.
	token, err := ParseClaudeCredentials([]byte(`{"claudeAiOauth": {"accessToken": "tok"}}`), time.Now())
	if err != nil || token != "tok" {
		t.Fatalf("token = %q, err = %v", token, err)
	}
}

func TestParseDaemonUsageUpdateWindows(t *testing.T) {
	raw := daemonUsageRequest{
		Usage5hPct:      []byte("42.5"),
		Usage5hResetsAt: []byte(`"2026-07-12T21:00:00Z"`),
		Usage7dPct:      []byte("null"),
		Usage7dResetsAt: []byte("null"),
	}
	update, err := parseDaemonUsageUpdate(raw)
	if err != nil {
		t.Fatalf("parseDaemonUsageUpdate: %v", err)
	}
	if update.Usage5hPct == nil || update.Usage5hPct.Value == nil || *update.Usage5hPct.Value != 42.5 {
		t.Fatalf("Usage5hPct = %+v, want 42.5", update.Usage5hPct)
	}
	if update.Usage5hResetsAt == nil || update.Usage5hResetsAt.Value == nil {
		t.Fatalf("Usage5hResetsAt = %+v, want set", update.Usage5hResetsAt)
	}
	// Literal null = explicit clear: outer pointer set, inner nil.
	if update.Usage7dPct == nil || update.Usage7dPct.Value != nil {
		t.Fatalf("Usage7dPct = %+v, want explicit clear", update.Usage7dPct)
	}
	if update.Usage7dResetsAt == nil || update.Usage7dResetsAt.Value != nil {
		t.Fatalf("Usage7dResetsAt = %+v, want explicit clear", update.Usage7dResetsAt)
	}
	// Absent fields stay nil.
	if update.UsageWindowPct != nil || update.ThrottledUntil != nil {
		t.Fatalf("absent fields should stay nil, got %+v / %+v", update.UsageWindowPct, update.ThrottledUntil)
	}
}

func TestParseDaemonUsageUpdateWindowsInvalid(t *testing.T) {
	if _, err := parseDaemonUsageUpdate(daemonUsageRequest{Usage5hPct: []byte(`"nope"`)}); err == nil {
		t.Fatal("expected error for non-numeric usage_5h_pct")
	}
	if _, err := parseDaemonUsageUpdate(daemonUsageRequest{Usage7dResetsAt: []byte(`"yesterday"`)}); err == nil {
		t.Fatal("expected error for non-RFC3339 usage_7d_resets_at")
	}
}
