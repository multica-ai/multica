package daemon

import (
	"strings"
	"testing"
)

func TestClassifyPoisonedOutput(t *testing.T) {
	cases := []struct {
		name       string
		output     string
		wantOK     bool
		wantReason string
	}{
		{
			name:       "iteration limit canonical",
			output:     "I reached the iteration limit and couldn't generate a summary.",
			wantOK:     true,
			wantReason: FailureReasonIterationLimit,
		},
		{
			name:       "iteration limit case insensitive",
			output:     "I REACHED THE ITERATION LIMIT and stopped",
			wantOK:     true,
			wantReason: FailureReasonIterationLimit,
		},
		{
			name:       "fallback meta message",
			output:     "Put your final update inside the content string. Keep it concise.",
			wantOK:     true,
			wantReason: FailureReasonAgentFallbackMsg,
		},
		{
			name:   "real conclusion is not poisoned",
			output: "Fixed the bug in auth.go and pushed PR #42.",
			wantOK: false,
		},
		{
			name:   "empty output",
			output: "",
			wantOK: false,
		},
		{
			name:   "mentions iteration but not the marker",
			output: "Each iteration of the loop processes one record.",
			wantOK: false,
		},
		{
			// Regression guard for the GPT-Boy review on MUL-1630:
			// a real review/analysis that quotes both markers must not
			// be misclassified. Without the length cap, this entire
			// PR's review thread would tank as a poisoned failure.
			name: "long review quoting both markers is not poisoned",
			output: `Review for the rerun fix.

Detection markers under consideration:
- "I reached the iteration limit and couldn't generate a summary."
- "Put your final update inside the content string. Keep it concise."

The implementation looks correct: the daemon classifies these as
fallback output, persists a dedicated failure_reason, and the SQL
filter excludes them from the resume lookup. Auto-retry of mid-flight
orphans still keeps the resume contract because CreateRetryTask does
not set force_fresh_session. Approving with a follow-up note about
the matcher being too permissive on long outputs.`,
			wantOK: false,
		},
		{
			name:   "marker buried inside a long agent conclusion",
			output: strings.Repeat("All checks passed and the bug is fixed. ", 10) + "i reached the iteration limit while debugging earlier.",
			wantOK: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reason, ok := classifyPoisonedOutput(tc.output)
			if ok != tc.wantOK {
				t.Fatalf("classifyPoisonedOutput(%q) ok=%v, want %v", tc.output, ok, tc.wantOK)
			}
			if ok && reason != tc.wantReason {
				t.Fatalf("classifyPoisonedOutput(%q) reason=%q, want %q", tc.output, reason, tc.wantReason)
			}
		})
	}
}
