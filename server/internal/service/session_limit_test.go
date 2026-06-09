package service

import (
	"testing"
	"time"
)

func TestParseSessionLimitResetTime(t *testing.T) {
	tests := []struct {
		name    string
		message string
		wantOK  bool
	}{
		{
			name:    "standard claude message pm",
			message: "You've hit your session limit · resets 5:10pm (UTC)",
			wantOK:  true,
		},
		{
			name:    "standard claude message am",
			message: "You've hit your session limit · resets 10:10am (UTC)",
			wantOK:  true,
		},
		{
			name:    "midnight 12am",
			message: "You've hit your session limit · resets 12:00am (UTC)",
			wantOK:  true,
		},
		{
			name:    "noon 12pm",
			message: "You've hit your session limit · resets 12:00pm (UTC)",
			wantOK:  true,
		},
		{
			name:    "no parentheses",
			message: "You've hit your session limit · resets 5:10pm UTC",
			wantOK:  true,
		},
		{
			name:    "unrelated error",
			message: "connection refused",
			wantOK:  false,
		},
		{
			name:    "empty",
			message: "",
			wantOK:  false,
		},
		{
			name:    "no reset time",
			message: "You've hit your session limit",
			wantOK:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParseSessionLimitResetTime(tc.message)
			if ok != tc.wantOK {
				t.Fatalf("ParseSessionLimitResetTime(%q) ok = %v, want %v", tc.message, ok, tc.wantOK)
			}
			if ok {
				if got.IsZero() {
					t.Fatal("expected non-zero time")
				}
				if got.Before(time.Now().UTC().Add(-time.Minute)) {
					t.Fatalf("reset time %v is in the past", got)
				}
			}
		})
	}
}

func TestParseSessionLimitResetTimeValues(t *testing.T) {
	msg := "You've hit your session limit · resets 11:30pm (UTC)"
	got, ok := ParseSessionLimitResetTime(msg)
	if !ok {
		t.Fatal("expected parse success")
	}
	if got.Hour() != 23 || got.Minute() != 30 {
		t.Fatalf("expected 23:30, got %02d:%02d", got.Hour(), got.Minute())
	}
}
