package account

import (
	"errors"
	"testing"
	"time"
)

func TestParseCodexUsageResponse(t *testing.T) {
	now := time.Date(2026, 7, 12, 20, 0, 0, 0, time.UTC)
	body := []byte(`{
		"rate_limit": {
			"primary_window": {"used_percent": 33.5, "reset_at": 1783932000, "limit_window_seconds": 18000},
			"secondary_window": {"used_percent": 71, "reset_after_seconds": 3600, "limit_window_seconds": 604800}
		},
		"credits": {"balance": 0}
	}`)
	snap, err := ParseCodexUsageResponse(body, now)
	if err != nil {
		t.Fatalf("ParseCodexUsageResponse: %v", err)
	}
	if snap.FiveHourPct == nil || *snap.FiveHourPct != 33.5 {
		t.Fatalf("FiveHourPct = %v, want 33.5", snap.FiveHourPct)
	}
	if snap.FiveHourResetsAt == nil || !snap.FiveHourResetsAt.Equal(time.Unix(1783932000, 0).UTC()) {
		t.Fatalf("FiveHourResetsAt = %v", snap.FiveHourResetsAt)
	}
	if snap.SevenDayPct == nil || *snap.SevenDayPct != 71 {
		t.Fatalf("SevenDayPct = %v, want 71", snap.SevenDayPct)
	}
	if snap.SevenDayResetsAt == nil || !snap.SevenDayResetsAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("SevenDayResetsAt = %v, want now+1h", snap.SevenDayResetsAt)
	}
	if !snap.HasSignal() {
		t.Fatal("HasSignal() = false, want true")
	}
}

func TestParseCodexUsageResponseMissingRateLimit(t *testing.T) {
	snap, err := ParseCodexUsageResponse([]byte(`{}`), time.Now())
	if err != nil {
		t.Fatalf("ParseCodexUsageResponse: %v", err)
	}
	if snap.HasSignal() {
		t.Fatalf("expected empty snapshot, got %+v", snap)
	}
}

func TestParseCodexUsageResponseClamps(t *testing.T) {
	body := []byte(`{"rate_limit": {"primary_window": {"used_percent": 140}, "secondary_window": {"used_percent": -3}}}`)
	snap, err := ParseCodexUsageResponse(body, time.Now())
	if err != nil {
		t.Fatalf("ParseCodexUsageResponse: %v", err)
	}
	if snap.FiveHourPct == nil || *snap.FiveHourPct != 100 {
		t.Fatalf("FiveHourPct = %v, want clamped 100", snap.FiveHourPct)
	}
	if snap.FiveHourResetsAt != nil {
		t.Fatalf("FiveHourResetsAt = %v, want nil without reset fields", snap.FiveHourResetsAt)
	}
	if snap.SevenDayPct == nil || *snap.SevenDayPct != 0 {
		t.Fatalf("SevenDayPct = %v, want clamped 0", snap.SevenDayPct)
	}
}

func TestParseCodexUsageResponseInvalidJSON(t *testing.T) {
	if _, err := ParseCodexUsageResponse([]byte("not json"), time.Now()); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseCodexCredentials(t *testing.T) {
	data := []byte(`{"tokens": {"access_token": "eyJ-abc", "account_id": "acct-1", "id_token": "x"}}`)
	token, accountID, err := ParseCodexCredentials(data)
	if err != nil {
		t.Fatalf("ParseCodexCredentials: %v", err)
	}
	if token != "eyJ-abc" || accountID != "acct-1" {
		t.Fatalf("token = %q, accountID = %q", token, accountID)
	}
}

func TestParseCodexCredentialsMissingToken(t *testing.T) {
	if _, _, err := ParseCodexCredentials([]byte(`{"tokens": {"account_id": "acct-1"}}`)); !errors.Is(err, ErrOAuthTokenUnavailable) {
		t.Fatalf("err = %v, want ErrOAuthTokenUnavailable", err)
	}
}
