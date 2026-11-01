package service

import (
	"os"
	"testing"
)

// TestDecideSessionResume is the acceptance case for STOE-29: a comment wakeup
// after a large session starts fresh, and a comment wakeup after a normal one
// does not. The threshold-crossed and not-crossed sides are both asserted
// because the not-crossed side is the regression this card could cause — a gate
// that trips early throws away an agent's working memory on ordinary follow-ups.
func TestDecideSessionResume(t *testing.T) {
	t.Parallel()

	defaults := SessionResumeLimits{
		MaxInputTokens: DefaultResumeSessionInputTokenLimit,
		MaxTurns:       DefaultResumeSessionTurnLimit,
	}

	cases := []struct {
		name         string
		metrics      SessionResumeMetrics
		limits       SessionResumeLimits
		wantDecision string
		wantReason   string
	}{
		{
			// The observed pathology from the card: a session whose last turn
			// already carried 154k input-side tokens, resumed into a 131k
			// window at 170s per model call.
			name:         "the 154k-token session that motivated the card",
			metrics:      SessionResumeMetrics{InputTokens: 154_264, Turns: 12},
			limits:       defaults,
			wantDecision: SessionResumeDecisionFresh,
			wantReason:   SessionResumeReasonContextLimit,
		},
		{
			name:         "just over the token limit",
			metrics:      SessionResumeMetrics{InputTokens: DefaultResumeSessionInputTokenLimit + 1, Turns: 3},
			limits:       defaults,
			wantDecision: SessionResumeDecisionFresh,
			wantReason:   SessionResumeReasonContextLimit,
		},
		{
			// Exactly on the limit is not over it: the limit is a ceiling, not
			// a tripwire, and a session that lands on it still fits.
			name:         "exactly on the token limit",
			metrics:      SessionResumeMetrics{InputTokens: DefaultResumeSessionInputTokenLimit, Turns: 3},
			limits:       defaults,
			wantDecision: SessionResumeDecisionResumed,
			wantReason:   "",
		},
		{
			// The ordinary follow-up: the fresh-run measurement in the card
			// sits at ~21k input tokens with 95-99% cache hits.
			name:         "an ordinary follow-up keeps its session",
			metrics:      SessionResumeMetrics{InputTokens: 21_400, Turns: 4},
			limits:       defaults,
			wantDecision: SessionResumeDecisionResumed,
			wantReason:   "",
		},
		{
			name:         "depth trips when no single turn looks large",
			metrics:      SessionResumeMetrics{InputTokens: 9_000, Turns: DefaultResumeSessionTurnLimit + 1},
			limits:       defaults,
			wantDecision: SessionResumeDecisionFresh,
			wantReason:   SessionResumeReasonTurnLimit,
		},
		{
			name:         "exactly on the turn limit",
			metrics:      SessionResumeMetrics{InputTokens: 9_000, Turns: DefaultResumeSessionTurnLimit},
			limits:       defaults,
			wantDecision: SessionResumeDecisionResumed,
			wantReason:   "",
		},
		{
			// Tokens are reported first: it is the figure that explains a slow
			// run, so a row that tripped both axes should read as the one a
			// reader can act on.
			name:         "both axes tripped reports the token axis",
			metrics:      SessionResumeMetrics{InputTokens: 200_000, Turns: 90},
			limits:       defaults,
			wantDecision: SessionResumeDecisionFresh,
			wantReason:   SessionResumeReasonContextLimit,
		},
		{
			// A session with no usage rows measures zero, not unknown: it is
			// small as far as the server can tell, and resumes.
			name:         "a session that recorded no usage",
			metrics:      SessionResumeMetrics{InputTokens: 0, Turns: 0},
			limits:       defaults,
			wantDecision: SessionResumeDecisionResumed,
			wantReason:   "",
		},
		{
			name:         "a tight deployment limit",
			metrics:      SessionResumeMetrics{InputTokens: 50_000, Turns: 2},
			limits:       SessionResumeLimits{MaxInputTokens: 32_000, MaxTurns: 5},
			wantDecision: SessionResumeDecisionFresh,
			wantReason:   SessionResumeReasonContextLimit,
		},
		{
			name:         "a zero limit disables the token axis",
			metrics:      SessionResumeMetrics{InputTokens: 900_000, Turns: 2},
			limits:       SessionResumeLimits{MaxInputTokens: 0, MaxTurns: 5},
			wantDecision: SessionResumeDecisionResumed,
			wantReason:   "",
		},
		{
			name:         "a zero limit disables the turn axis",
			metrics:      SessionResumeMetrics{InputTokens: 1_000, Turns: 5_000},
			limits:       SessionResumeLimits{MaxInputTokens: 32_000, MaxTurns: 0},
			wantDecision: SessionResumeDecisionResumed,
			wantReason:   "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			decision, reason := DecideSessionResume(tc.metrics, tc.limits)
			if decision != tc.wantDecision || reason != tc.wantReason {
				t.Fatalf("DecideSessionResume(%+d tokens, %d turns, limits %+v) = (%q, %q), want (%q, %q)",
					tc.metrics.InputTokens, tc.metrics.Turns, tc.limits, decision, reason, tc.wantDecision, tc.wantReason)
			}
		})
	}
}

// TestResumeSessionLimits covers the deployment override: a self-hosted
// operator has to be able to move or disable the gate without a code change,
// and a typo in one must not silently switch the gate off.
func TestResumeSessionLimits(t *testing.T) {
	t.Setenv("MULTICA_SESSION_RESUME_MAX_INPUT_TOKENS", "250000")
	t.Setenv("MULTICA_SESSION_RESUME_MAX_TURNS", "10")

	limits := ResumeSessionLimits()
	if limits.MaxInputTokens != 250_000 {
		t.Errorf("MaxInputTokens = %d, want 250000", limits.MaxInputTokens)
	}
	if limits.MaxTurns != 10 {
		t.Errorf("MaxTurns = %d, want 10", limits.MaxTurns)
	}

	t.Setenv("MULTICA_SESSION_RESUME_MAX_INPUT_TOKENS", "0")
	t.Setenv("MULTICA_SESSION_RESUME_MAX_TURNS", "-5")
	limits = ResumeSessionLimits()
	if limits.MaxInputTokens != 0 || limits.MaxTurns != 0 {
		t.Errorf("zero/negative limits = %+v, want both axes disabled (0)", limits)
	}

	t.Setenv("MULTICA_SESSION_RESUME_MAX_INPUT_TOKENS", "120k")
	t.Setenv("MULTICA_SESSION_RESUME_MAX_TURNS", "")
	limits = ResumeSessionLimits()
	if limits.MaxInputTokens != DefaultResumeSessionInputTokenLimit {
		t.Errorf("unparseable limit = %d, want the default %d", limits.MaxInputTokens, DefaultResumeSessionInputTokenLimit)
	}
	if limits.MaxTurns != DefaultResumeSessionTurnLimit {
		t.Errorf("unset limit = %d, want the default %d", limits.MaxTurns, DefaultResumeSessionTurnLimit)
	}

	// The defaults have to be readable with no environment at all: an unset
	// var and an empty one mean the same thing.
	os.Unsetenv("MULTICA_SESSION_RESUME_MAX_INPUT_TOKENS")
	os.Unsetenv("MULTICA_SESSION_RESUME_MAX_TURNS")
	limits = ResumeSessionLimits()
	if limits.MaxInputTokens != DefaultResumeSessionInputTokenLimit || limits.MaxTurns != DefaultResumeSessionTurnLimit {
		t.Errorf("unset limits = %+v, want the defaults", limits)
	}
}
