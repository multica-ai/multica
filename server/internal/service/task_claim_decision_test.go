package service

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
)

// This runs without a database: claimReasonFor, normalizeClaimPath, and the
// metric funnel are pure functions over their arguments.

// TestClaimDecisionReasonFor pins the outcome -> reason mapping, especially
// the #8551 split inside no_tasks: candidates > 0 means work was visible and
// nothing was claimed on this attempt (exact cause unknown), zero candidates
// on a list-bearing path means the candidate query saw nothing claimable, and
// the list-less agent path can only report "unknown".
func TestClaimDecisionReasonFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		outcome string
		scope   claimScope
		want    string
	}{
		{name: "claimed stays claimed", outcome: claimOutcomeClaimed, scope: claimScope{claimPath: claimPathAgent}, want: claimReasonClaimed},
		{name: "claimed via runtime poll", outcome: claimOutcomeClaimed, scope: claimScope{claimPath: claimPathRuntime, candidates: 3}, want: claimReasonClaimed},
		{name: "no capacity maps to concurrency cap", outcome: claimOutcomeNoCapacity, scope: claimScope{claimPath: claimPathRuntime}, want: claimReasonAgentConcurrencyCap},
		{name: "no runtime maps to unbound agent", outcome: claimOutcomeNoRuntime, scope: claimScope{claimPath: claimPathAgent}, want: claimReasonAgentUnbound},
		{name: "runtime mismatch maps to rebound agent", outcome: claimOutcomeRuntimeMismatch, scope: claimScope{claimPath: claimPathRuntime}, want: claimReasonAgentRebound},

		{name: "no tasks with candidates is unclaimed", outcome: claimOutcomeNoTasks, scope: claimScope{claimPath: claimPathRuntime, candidates: 1}, want: claimReasonCandidatesUnclaimed},
		{name: "no tasks unclaimed on batch path", outcome: claimOutcomeNoTasks, scope: claimScope{claimPath: claimPathBatch, candidates: 2}, want: claimReasonCandidatesUnclaimed},
		{name: "no tasks without candidates is nothing visible", outcome: claimOutcomeNoTasks, scope: claimScope{claimPath: claimPathRuntime}, want: claimReasonNoVisibleCandidates},
		{name: "no tasks without candidates on batch", outcome: claimOutcomeNoTasks, scope: claimScope{claimPath: claimPathBatch}, want: claimReasonNoVisibleCandidates},
		{name: "no tasks on agent path is unknown", outcome: claimOutcomeNoTasks, scope: claimScope{claimPath: claimPathAgent}, want: claimReasonUnknownCandidates},
		{name: "agent path with candidates is still unclaimed", outcome: claimOutcomeNoTasks, scope: claimScope{claimPath: claimPathAgent, candidates: 1}, want: claimReasonCandidatesUnclaimed},

		{name: "empty cache hit", outcome: claimOutcomeEmptyCacheHit, scope: claimScope{claimPath: claimPathRuntime}, want: claimReasonEmptyCache},
		{name: "empty db means nothing visible", outcome: claimOutcomeEmptyDB, scope: claimScope{claimPath: claimPathRuntime}, want: claimReasonNoVisibleCandidates},
		{name: "empty db on batch means nothing visible", outcome: claimOutcomeEmptyDB, scope: claimScope{claimPath: claimPathBatch}, want: claimReasonNoVisibleCandidates},
		{name: "reclaimed dispatch", outcome: claimOutcomeReclaimedDispatched, scope: claimScope{claimPath: claimPathBatch}, want: claimReasonStaleDispatchRecover},

		{name: "error get agent", outcome: claimOutcomeErrorGetAgent, scope: claimScope{claimPath: claimPathAgent}, want: claimReasonError},
		{name: "error count running", outcome: claimOutcomeErrorCountRunning, scope: claimScope{claimPath: claimPathRuntime}, want: claimReasonError},
		{name: "error claim", outcome: claimOutcomeErrorClaim, scope: claimScope{claimPath: claimPathRuntime}, want: claimReasonError},
		{name: "error reanchor chat input", outcome: claimOutcomeErrorReanchorChatInput, scope: claimScope{claimPath: claimPathRuntime}, want: claimReasonError},
		{name: "error transaction", outcome: claimOutcomeErrorTransaction, scope: claimScope{claimPath: claimPathBatch}, want: claimReasonError},
		{name: "error promote deferred", outcome: claimOutcomeErrorPromoteDeferred, scope: claimScope{claimPath: claimPathBatch}, want: claimReasonError},
		{name: "error reclaim dispatched", outcome: claimOutcomeErrorReclaimDispatched, scope: claimScope{claimPath: claimPathRuntime}, want: claimReasonError},
		{name: "error list", outcome: claimOutcomeErrorList, scope: claimScope{claimPath: claimPathBatch}, want: claimReasonError},

		// Unknown inputs degrade to "error" instead of minting a series.
		{name: "unrecognized outcome", outcome: "no_task", scope: claimScope{claimPath: claimPathRuntime}, want: claimReasonError},
		{name: "bogus outcome", outcome: "surprise", scope: claimScope{claimPath: claimPathAgent}, want: claimReasonError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := claimReasonFor(tt.outcome, tt.scope)
			if got != tt.want {
				t.Fatalf("claimReasonFor(%q, %+v) = %q, want %q", tt.outcome, tt.scope, got, tt.want)
			}
		})
	}
}

// TestClaimDecisionNormalizePath keeps the claim_path label a closed enum.
func TestClaimDecisionNormalizePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		claimPath string
		want      string
	}{
		{claimPath: claimPathAgent, want: claimPathAgent},
		{claimPath: claimPathRuntime, want: claimPathRuntime},
		{claimPath: claimPathBatch, want: claimPathBatch},
		{claimPath: "", want: "other"},
		{claimPath: "daemon", want: "other"},
		{claimPath: "AGENT", want: "other"},
	}

	for _, tt := range tests {
		t.Run(tt.claimPath, func(t *testing.T) {
			t.Parallel()
			if got := normalizeClaimPath(tt.claimPath); got != tt.want {
				t.Fatalf("normalizeClaimPath(%q) = %q, want %q", tt.claimPath, got, tt.want)
			}
		})
	}
}

// TestClaimDecisionNilMetricsIsSafe covers self-hosted deployments and tests
// that run without the metrics listener: the funnel must not panic and must
// still skip the (suppressed) hot-path log branches.
func TestClaimDecisionNilMetricsIsSafe(t *testing.T) {
	t.Parallel()

	svc := &TaskService{}
	for _, outcome := range []string{
		claimOutcomeClaimed,
		claimOutcomeNoTasks,
		claimOutcomeNoCapacity,
		claimOutcomeNoRuntime,
		claimOutcomeRuntimeMismatch,
		claimOutcomeEmptyCacheHit,
		claimOutcomeEmptyDB,
		claimOutcomeErrorClaim,
	} {
		svc.recordClaimDecision(pgtype.UUID{}, pgtype.UUID{}, outcome, claimScope{claimPath: claimPathRuntime, candidates: 1}, claimDecisionDetail{running: 2, maxConcurrent: 2})
	}
}

// TestClaimDecisionIncrementsLabeledSeries verifies the funnel lands each
// decision on exactly its (claim_path, outcome, reason) series, that an
// unclassified path is normalized to "other", and that a never-fired
// prewarmed series still reads as zero rather than being absent.
func TestClaimDecisionIncrementsLabeledSeries(t *testing.T) {
	t.Parallel()

	m := obsmetrics.NewBusinessMetrics()
	svc := &TaskService{Metrics: m}

	svc.recordClaimDecision(pgtype.UUID{}, pgtype.UUID{}, claimOutcomeNoTasks, claimScope{claimPath: claimPathRuntime, candidates: 2}, claimDecisionDetail{})
	svc.recordClaimDecision(pgtype.UUID{}, pgtype.UUID{}, claimOutcomeNoTasks, claimScope{claimPath: claimPathAgent}, claimDecisionDetail{})
	svc.recordClaimDecision(pgtype.UUID{}, pgtype.UUID{}, claimOutcomeNoCapacity, claimScope{claimPath: claimPathAgent}, claimDecisionDetail{running: 3, maxConcurrent: 3})
	svc.recordClaimDecision(pgtype.UUID{}, pgtype.UUID{}, claimOutcomeEmptyDB, claimScope{claimPath: claimPathBatch}, claimDecisionDetail{})
	svc.recordClaimDecision(pgtype.UUID{}, pgtype.UUID{}, claimOutcomeErrorList, claimScope{claimPath: "daemon"}, claimDecisionDetail{})

	tests := []struct {
		claimPath, outcome, reason string
		want                       float64
	}{
		{claimPathRuntime, claimOutcomeNoTasks, claimReasonCandidatesUnclaimed, 1},
		{claimPathRuntime, claimOutcomeNoTasks, claimReasonNoVisibleCandidates, 0},
		{claimPathAgent, claimOutcomeNoTasks, claimReasonUnknownCandidates, 1},
		{claimPathAgent, claimOutcomeNoCapacity, claimReasonAgentConcurrencyCap, 1},
		{claimPathBatch, claimOutcomeEmptyDB, claimReasonNoVisibleCandidates, 1},
		// An unknown path is normalized to "other" rather than minting a
		// per-call-site series.
		{"other", claimOutcomeErrorList, claimReasonError, 1},
		{claimPathRuntime, claimOutcomeErrorList, claimReasonError, 0},
		// Prewarmed series that have not fired read as zero, including the
		// batch poll exits.
		{claimPathBatch, claimOutcomeNoTasks, claimReasonCandidatesUnclaimed, 0},
		{claimPathBatch, claimOutcomeReclaimedDispatched, claimReasonStaleDispatchRecover, 0},
		{claimPathBatch, claimOutcomeEmptyCacheHit, claimReasonEmptyCache, 0},
		{claimPathBatch, claimOutcomeErrorPromoteDeferred, claimReasonError, 0},
		{claimPathRuntime, claimOutcomeReclaimedDispatched, claimReasonStaleDispatchRecover, 0},
	}
	for _, tt := range tests {
		t.Run(tt.claimPath+"/"+tt.outcome+"/"+tt.reason, func(t *testing.T) {
			t.Parallel()
			if got := claimDecisionCount(t, m, tt.claimPath, tt.outcome, tt.reason); got != tt.want {
				t.Fatalf("claim_decision_total{%s,%s,%s} = %v, want %v", tt.claimPath, tt.outcome, tt.reason, got, tt.want)
			}
		})
	}
}

// ---- helpers ---------------------------------------------------------------

// claimDecisionCount reads one (claim_path, outcome, reason) series of the
// claim decision counter out of a freshly gathered registry.
func claimDecisionCount(t *testing.T, m *obsmetrics.BusinessMetrics, claimPath, outcome, reason string) float64 {
	t.Helper()

	fam := obsmetrics.GatherForTest(t, m)["multica_agent_task_claim_decision_total"]
	if fam == nil {
		t.Fatalf("multica_agent_task_claim_decision_total not registered")
	}
	for _, mtr := range fam.GetMetric() {
		if labelValue(mtr, "claim_path") == claimPath && labelValue(mtr, "outcome") == outcome && labelValue(mtr, "reason") == reason {
			return mtr.GetCounter().GetValue()
		}
	}
	t.Fatalf("no sample for %s/%s/%s", claimPath, outcome, reason)
	return 0
}
