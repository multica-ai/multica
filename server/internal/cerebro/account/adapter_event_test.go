package account

import (
	"testing"
	"time"
)

func TestParseAdapterOutput_RetryAfter(t *testing.T) {
	now := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		input    string
		wantSecs int
	}{
		{
			name:     "relative duration",
			input:    `API Error: 429 — please try again in 30 seconds`,
			wantSecs: 30,
		},
		{
			name:     "wall clock UTC",
			input:    `Rate limited. Resets at 10:05 (UTC)`,
			wantSecs: 5 * 60,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ev := ParseAdapterOutput(tc.input, now)
			if ev.ThrottledUntil == nil {
				t.Fatalf("expected ThrottledUntil to be set, got nil")
			}
			diff := ev.ThrottledUntil.Sub(now)
			want := time.Duration(tc.wantSecs) * time.Second
			if diff < want-time.Second || diff > want+time.Second {
				t.Fatalf("expected ~%v from now, got %v", want, diff)
			}
		})
	}
}

func TestParseAdapterOutput_UsagePct(t *testing.T) {
	now := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		input string
		want  float32
	}{
		{"header-style", `x-ratelimit-input-tokens-pct: 87`, 87},
		{"json-style", `{"limits": {"input_tokens_pct": 92.5}}`, 92.5},
		{"prose", `You have used 42% of your weekly usage window`, 42},
		{"window-pct", `usage_window_pct=15`, 15},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ev := ParseAdapterOutput(tc.input, now)
			if ev.UsageWindowPct == nil {
				t.Fatalf("expected UsageWindowPct to be set, got nil")
			}
			got := *ev.UsageWindowPct
			if got < tc.want-0.1 || got > tc.want+0.1 {
				t.Fatalf("UsageWindowPct: got %v want %v", got, tc.want)
			}
		})
	}
}

func TestParseAdapterOutput_NoSignal(t *testing.T) {
	now := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
	cases := []string{
		"",
		"just an agent reply with nothing of interest",
		"the agent answered 100% of the questions",
		"87% reduction in latency",
	}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			ev := ParseAdapterOutput(c, now)
			if ev.HasSignal() {
				t.Fatalf("expected no signal for %q, got %+v", c, ev)
			}
		})
	}
}

func TestParseAdapterOutput_BothSignals(t *testing.T) {
	now := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
	input := `API Error: 429 rate limit; try again in 60 seconds.
x-ratelimit-input-tokens-pct: 99`
	ev := ParseAdapterOutput(input, now)
	if ev.ThrottledUntil == nil {
		t.Fatalf("expected ThrottledUntil to be set")
	}
	if ev.UsageWindowPct == nil || *ev.UsageWindowPct != 99 {
		t.Fatalf("expected UsageWindowPct=99, got %+v", ev.UsageWindowPct)
	}
}
