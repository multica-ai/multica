package taskfailure

// CEREBRO-PATCH(taskfailure-session-limit): FIR-3651 regression cover for the
// verbatim provider strings that fell through to ReasonAgentUnknown in
// production between 28 June and 22 July 2026. Unknown is the one bucket
// Workflow never retries, so each of these died on first contact instead of
// pausing until the stated reset time.

import "testing"

func TestClassifySessionAndWeeklyLimits(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		err  string
		want Reason
	}{
		{
			name: "claude per-session cap",
			err:  "You've hit your session limit · resets 2:30pm (Europe/Athens)",
			want: ReasonAgentProviderQuotaLimit,
		},
		{
			name: "claude per-session cap, whole hour",
			err:  "You've hit your session limit · resets 5pm (Europe/Copenhagen)",
			want: ReasonAgentProviderQuotaLimit,
		},
		{
			name: "claude weekly cap",
			err:  "You've hit your weekly limit · resets Jul 24 at 2pm (Europe/Athens)",
			want: ReasonAgentProviderQuotaLimit,
		},
		{
			name: "claude monthly spend cap keeps its FIR-1889 rule",
			err:  "You've hit your monthly spend limit. Run /usage-credits to manage your limit.",
			want: ReasonAgentProviderQuotaLimit,
		},
		{
			name: "model at capacity keeps its FIR-3501 rule",
			err:  "Selected model is at capacity. Please try a different model.",
			want: ReasonAgentProviderCapacityOrRateLimit,
		},
		{
			name: "claude cli mid-stream cut, ported from upstream MUL-4910",
			err:  "API Error: Connection closed mid-response. The response above may be incomplete.",
			want: ReasonAgentProviderNetwork,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := Classify(tc.err); got != tc.want {
				t.Errorf("Classify(%q) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}
