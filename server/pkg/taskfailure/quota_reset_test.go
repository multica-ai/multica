package taskfailure

import (
	"testing"
	"time"
)

// These messages are verbatim from the dev7 deployment quota refusals the
// windowed-quota classifier and reset parser were built against.
const (
	sessionLimitMessage = "You've hit your session limit · resets 12:50pm (UTC)"
	weeklyLimitMessage  = "You've hit your weekly limit · resets 2pm (UTC)"
)

func TestParseQuotaResetAt(t *testing.T) {
	now := time.Date(2026, 8, 28, 11, 41, 49, 0, time.UTC)

	cases := []struct {
		name    string
		message string
		now     time.Time
		want    time.Time
		ok      bool
	}{
		{
			name:    "session limit later the same day",
			message: sessionLimitMessage,
			now:     now,
			want:    time.Date(2026, 8, 28, 12, 50, 0, 0, time.UTC),
			ok:      true,
		},
		{
			name:    "hour without minutes",
			message: weeklyLimitMessage,
			now:     now,
			want:    time.Date(2026, 8, 28, 14, 0, 0, 0, time.UTC),
			ok:      true,
		},
		{
			name:    "already past today rolls to tomorrow",
			message: sessionLimitMessage,
			now:     time.Date(2026, 8, 28, 13, 0, 0, 0, time.UTC),
			want:    time.Date(2026, 8, 29, 12, 50, 0, 0, time.UTC),
			ok:      true,
		},
		{
			name:    "midnight is 12am, not noon",
			message: "resets 12am (UTC)",
			now:     time.Date(2026, 8, 28, 23, 0, 0, 0, time.UTC),
			want:    time.Date(2026, 8, 29, 0, 0, 0, 0, time.UTC),
			ok:      true,
		},
		{
			name:    "24-hour wording",
			message: "resets 14:05 (UTC)",
			now:     now,
			want:    time.Date(2026, 8, 28, 14, 5, 0, 0, time.UTC),
			ok:      true,
		},
		{
			name:    "no zone is read as UTC",
			message: "resets 12:50pm",
			now:     now,
			want:    time.Date(2026, 8, 28, 12, 50, 0, 0, time.UTC),
			ok:      true,
		},
		{
			name:    "a zone we cannot resolve is refused",
			message: "resets 12:50pm (PDT)",
			now:     now,
		},
		{
			name:    "no reset clause at all",
			message: "You've hit your session limit",
			now:     now,
		},
		{
			name:    "impossible wall clock",
			message: "resets 61:99 (UTC)",
			now:     now,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParseQuotaResetAt(tc.message, tc.now)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v (got %v)", ok, tc.ok, got)
			}
			if !tc.ok {
				return
			}
			if !got.Equal(tc.want) {
				t.Fatalf("reset = %s, want %s", got, tc.want)
			}
			if !got.After(tc.now) {
				t.Fatalf("reset %s must be strictly after now %s", got, tc.now)
			}
		})
	}
}

// A parsed reset is always inside the horizon, so a caller can park a task on
// it without a second sanity check.
func TestParseQuotaResetAtStaysInsideHorizon(t *testing.T) {
	now := time.Date(2026, 8, 28, 11, 41, 49, 0, time.UTC)
	got, ok := ParseQuotaResetAt(sessionLimitMessage, now)
	if !ok {
		t.Fatal("expected a parse")
	}
	if got.Sub(now) > QuotaResetHorizon {
		t.Fatalf("reset %s is beyond the horizon", got)
	}
}
