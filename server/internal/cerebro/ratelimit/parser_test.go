package ratelimit

import (
	"testing"
	"time"
)

// anchor is a fixed "now" so the relative/wall-clock branches are deterministic.
var anchor = time.Date(2026, time.May, 31, 12, 0, 0, 0, time.UTC)

// TestClassifyReset_AuthCapacityGap covers the FIR-2611 classifier gap: expired
// or revoked logins, bad API keys, "not logged in", and capacity exhaustion
// must all be pause-worthy so the runtime auto-pauses instead of bounce-looping.
// None of these carry a reset time, so hasReset must be false (caller applies
// the growing backoff).
func TestClassifyReset_AuthCapacityGap(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{"refresh token revoked", "OAuth error: refresh token was already used or revoked"},
		{"refresh token expired", "the refresh token expired, please re-authenticate"},
		{"invalid api key", "Error: Invalid API key provided"},
		{"incorrect api key", "Incorrect API key provided: sk-***"},
		{"invalid x-api-key", "401 invalid x-api-key"},
		{"not logged in", "You are not logged in. Please run /login to continue."},
		{"please run codex login", "Authentication required: please run codex login"},
		{"capacity exhausted", "You've exhausted your current capacity for this model"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, hasReset, pauseWorthy := ClassifyReset(tc.text, anchor)
			if !pauseWorthy {
				t.Fatalf("expected pauseWorthy=true for %q", tc.text)
			}
			if hasReset {
				t.Fatalf("expected hasReset=false (no reset time) for %q", tc.text)
			}
		})
	}
}

// TestClassifyReset_NotPauseWorthy guards against the broadened regex pausing on
// ordinary failures — a plain compile error or a generic 500 must stay a normal
// agent_error, not trip auto-pause.
func TestClassifyReset_NotPauseWorthy(t *testing.T) {
	cases := []string{
		"panic: nil pointer dereference",
		"syntax error: unexpected token at line 3",
		"500 internal server error",
		"connection refused",
		"",
	}
	for _, text := range cases {
		if _, _, pauseWorthy := ClassifyReset(text, anchor); pauseWorthy {
			t.Fatalf("expected pauseWorthy=false for %q", text)
		}
	}
}

// TestClassifyReset_ProviderResetWins verifies the back-off requirement: when
// the provider message carries an explicit reset time we pause until that time
// (hasReset=true), not the fixed fallback.
func TestClassifyReset_ProviderResetWins(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{"wall clock pm", "Rate limited. Resets 3:40PM UTC"},
		{"codex full date", "You've hit your usage limit. Try again at June 2nd, 2026 10:12 PM"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetAt, hasReset, pauseWorthy := ClassifyReset(tc.text, anchor)
			if !pauseWorthy {
				t.Fatalf("expected pauseWorthy=true for %q", tc.text)
			}
			if !hasReset {
				t.Fatalf("expected a parsed reset time for %q", tc.text)
			}
			if !resetAt.After(anchor) {
				t.Fatalf("reset time %v should be after now %v for %q", resetAt, anchor, tc.text)
			}
		})
	}
}
