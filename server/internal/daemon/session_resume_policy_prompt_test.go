package daemon

import (
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
)

// TestBuildSessionResumePolicyBlock is the "no silent fallback" half of STOE-29:
// when the server declines a session on purpose, the run is told, with the
// figures that made the decision. A run that is not told assumes the continuity
// it was not given — the failure mode the card is about is an agent reasoning
// from a conversation it no longer has.
func TestBuildSessionResumePolicyBlock(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		task    Task
		want    string
		wantSub []string
	}{
		{
			name: "a resumed run says nothing",
			task: Task{SessionResumeDecision: service.SessionResumeDecisionResumed},
			want: "",
		},
		{
			name: "an old server that sends no decision says nothing",
			task: Task{},
			want: "",
		},
		{
			name: "context limit names both figures",
			task: Task{
				SessionResumeDecision:   service.SessionResumeDecisionFresh,
				SessionResumeReason:     service.SessionResumeReasonContextLimit,
				PriorSessionInputTokens: 154264,
				PriorSessionTurns:       12,
			},
			wantSub: []string{"## Fresh session", "154264 input tokens", "12 turns"},
		},
		{
			name: "turn limit names the depth",
			task: Task{
				SessionResumeDecision: service.SessionResumeDecisionFresh,
				SessionResumeReason:   service.SessionResumeReasonTurnLimit,
				PriorSessionTurns:     41,
			},
			wantSub: []string{"## Fresh session", "41 turns"},
		},
		{
			name: "runtime mismatch names the real cause",
			task: Task{
				SessionResumeDecision: service.SessionResumeDecisionFresh,
				SessionResumeReason:   service.SessionResumeReasonRuntimeMismatch,
			},
			wantSub: []string{"## Fresh session", "another runtime"},
		},
		{
			// A force-fresh retry already carries the MUL-7034 disclosure, and a
			// run with nothing on offer has nothing to explain: rendering here
			// would pay for the same paragraph twice.
			name: "forced fresh is disclosed elsewhere",
			task: Task{
				SessionResumeDecision: service.SessionResumeDecisionFresh,
				SessionResumeReason:   service.SessionResumeReasonForcedFresh,
			},
			want: "",
		},
		{
			name: "no prior session is not a decision",
			task: Task{
				SessionResumeDecision: service.SessionResumeDecisionNone,
				SessionResumeReason:   service.SessionResumeReasonNoPriorSession,
			},
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := buildSessionResumePolicyBlock(tc.task)
			if tc.want != "" {
				if got != tc.want {
					t.Fatalf("buildSessionResumePolicyBlock(%+v) = %q, want %q", tc.task, got, tc.want)
				}
				return
			}
			for _, sub := range tc.wantSub {
				if !strings.Contains(got, sub) {
					t.Errorf("buildSessionResumePolicyBlock(%+v) missing %q in:\n%s", tc.task, sub, got)
				}
			}
			// The copy must not script an apology: the issue record is intact,
			// and the run should raise this only where it matters.
			if strings.Contains(got, "tell the user") {
				t.Errorf("policy notice must not script a user-facing apology:\n%s", got)
			}
		})
	}
}

// TestPerTurnContextBlocksCarriesOneNotice keeps the two disclosure paths from
// double-billing a turn: the continuity notice (the resume was lost) and the
// policy notice (the resume was declined) describe the same absence, and a run
// that got both pays for the same paragraph twice — the exact duplication
// MUL-5722 removed.
func TestPerTurnContextBlocksCarriesOneNotice(t *testing.T) {
	t.Parallel()

	policy := perTurnContextBlocks(Task{
		SessionResumeDecision:   service.SessionResumeDecisionFresh,
		SessionResumeReason:     service.SessionResumeReasonContextLimit,
		PriorSessionInputTokens: 154264,
		PriorSessionTurns:       12,
	}, promptOpts{})
	if !strings.Contains(policy, "## Fresh session") {
		t.Fatalf("a declined resume must be disclosed in the turn's context blocks, got:\n%s", policy)
	}
	if strings.Contains(policy, "Session Continuity Notice") {
		t.Errorf("a declined resume must not also render the continuity notice:\n%s", policy)
	}

	both := perTurnContextBlocks(Task{
		PriorSessionResumeUnavailable: true,
		SessionResumeDecision:         service.SessionResumeDecisionFresh,
		SessionResumeReason:           service.SessionResumeReasonContextLimit,
		PriorSessionInputTokens:       154264,
		PriorSessionTurns:             12,
	}, promptOpts{})
	if strings.Count(both, "## ") != 1 {
		t.Errorf("a run may carry one notice, got %d in:\n%s", strings.Count(both, "## "), both)
	}

	quiet := perTurnContextBlocks(Task{SessionResumeDecision: service.SessionResumeDecisionResumed}, promptOpts{})
	if strings.Contains(quiet, "## Fresh session") {
		t.Errorf("a resumed run must not be told it started fresh:\n%s", quiet)
	}
}
