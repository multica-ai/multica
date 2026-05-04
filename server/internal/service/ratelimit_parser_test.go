package service

import (
	"testing"
	"time"
)

func TestParseRateLimitReset(t *testing.T) {
	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name    string
		input   string
		want    time.Time
		wantOK  bool
	}{
		{
			name:   "wall clock pm UTC",
			input:  "You've hit your limit · resets 6:50pm (UTC)",
			want:   time.Date(2026, 5, 4, 18, 50, 0, 0, time.UTC),
			wantOK: true,
		},
		{
			name:   "wall clock 24h with explicit UTC",
			input:  "Rate limit reached. Resets at 18:50 UTC",
			want:   time.Date(2026, 5, 4, 18, 50, 0, 0, time.UTC),
			wantOK: true,
		},
		{
			name:   "wall clock am UTC",
			input:  "available at 9am UTC",
			want:   time.Date(2026, 5, 5, 9, 0, 0, 0, time.UTC),
			wantOK: true,
		},
		{
			name:   "wall clock 12am UTC rolls to next day",
			input:  "resets at 12:00am (UTC)",
			want:   time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC),
			wantOK: true,
		},
		{
			name:   "wall clock past time rolls forward 24h",
			input:  "resets 8:00am (UTC)",
			want:   time.Date(2026, 5, 5, 8, 0, 0, 0, time.UTC),
			wantOK: true,
		},
		{
			name:   "relative seconds",
			input:  "Rate limit. retry after 30s",
			want:   now.Add(30 * time.Second),
			wantOK: true,
		},
		{
			name:   "relative minutes",
			input:  "Try again in 5 minutes",
			want:   now.Add(5 * time.Minute),
			wantOK: true,
		},
		{
			name:   "relative hours",
			input:  "wait 2 hours before retrying",
			want:   now.Add(2 * time.Hour),
			wantOK: true,
		},
		{
			name:   "iso 8601 Z",
			input:  "Reset at 2026-05-04T18:50:00Z",
			want:   time.Date(2026, 5, 4, 18, 50, 0, 0, time.UTC),
			wantOK: true,
		},
		{
			name:   "iso 8601 offset",
			input:  "Resets 2026-05-04T20:50:00+02:00",
			want:   time.Date(2026, 5, 4, 18, 50, 0, 0, time.UTC),
			wantOK: true,
		},
		{
			name:   "epoch seconds in header",
			input:  "X-RateLimit-Reset: 1778256600",
			want:   time.Unix(1778256600, 0).UTC(),
			wantOK: true,
		},
		{
			name:   "epoch seconds out-of-range rejected",
			input:  "Reset: 9999999999",
			want:   time.Time{},
			wantOK: false,
		},
		{
			name:   "wall clock too far rejected",
			input:  "resets at 11am (UTC)",
			want:   time.Date(2026, 5, 5, 11, 0, 0, 0, time.UTC),
			wantOK: true,
		},
		{
			name:   "no hint in error",
			input:  "Internal server error",
			want:   time.Time{},
			wantOK: false,
		},
		{
			name:   "empty input",
			input:  "",
			want:   time.Time{},
			wantOK: false,
		},
		{
			// The Anthropic transient-throttle string. No reset time, but
			// "Rate limited" is unambiguous — fall back to the 5-minute
			// default so the runtime backs off without staying paused
			// forever waiting for a manual unpause.
			name:   "anthropic transient throttle without time hint",
			input:  "API Error: Server is temporarily limiting requests (not your usage limit) · Rate limited",
			want:   now.Add(DefaultRateLimitBackoff),
			wantOK: true,
		},
		{
			name:   "ratelimit single word fallback",
			input:  "ratelimit exceeded",
			want:   now.Add(DefaultRateLimitBackoff),
			wantOK: true,
		},
		{
			name:   "http 429 fallback",
			input:  "Request failed: HTTP 429 Too Many Requests",
			want:   now.Add(DefaultRateLimitBackoff),
			wantOK: true,
		},
		{
			// Regression guard: an explicit reset hint must still win over
			// the fallback. "Rate limited" alone would yield 5min; the
			// "Try again in 30s" hint must give 30s.
			name:   "rate-limited with explicit hint beats fallback",
			input:  "Rate limited. Try again in 30s",
			want:   now.Add(30 * time.Second),
			wantOK: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParseRateLimitReset(tc.input, now)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (input=%q got=%v)", ok, tc.wantOK, tc.input, got)
			}
			if !ok {
				return
			}
			if !got.Equal(tc.want) {
				t.Fatalf("got %s, want %s", got.Format(time.RFC3339), tc.want.Format(time.RFC3339))
			}
		})
	}
}

// Anchors a relative-duration parse against an injected `now` to avoid
// flake from clock drift between Now() calls in test vs production paths.
func TestParseRateLimitReset_RelativeAnchor(t *testing.T) {
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	got, ok := ParseRateLimitReset("retry after 90 seconds", now)
	if !ok {
		t.Fatal("expected parse success")
	}
	want := now.Add(90 * time.Second)
	if !got.Equal(want) {
		t.Fatalf("got %s want %s", got, want)
	}
}
