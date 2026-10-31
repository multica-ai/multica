package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
)

// This runs without a database: the normalizers, the canary policy, and the
// funnel are pure functions over their arguments.

// stubClaimConstraintPolicy is an inline policy stub (same style as
// stubOverlayBuilder in resolve_originator_test.go): it records every call and
// the request it saw, then returns a canned decision.
type stubClaimConstraintPolicy struct {
	calls int
	last  ClaimConstraintRequest
	resp  ClaimConstraintDecision
}

func (s *stubClaimConstraintPolicy) EvaluateClaimConstraint(_ context.Context, req ClaimConstraintRequest) ClaimConstraintDecision {
	s.calls++
	s.last = req
	return s.resp
}

// TestClaimConstraintNormalizeAction keeps the decision label a closed enum.
// An unrecognized action folds to "allow" — fail-open, consistent with the
// interface contract — rather than to "deny" or "other".
func TestClaimConstraintNormalizeAction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		action string
		want   string
	}{
		{claimConstraintAllow, claimConstraintAllow},
		{claimConstraintWait, claimConstraintWait},
		{claimConstraintDeny, claimConstraintDeny},
		{"", claimConstraintAllow},
		{"block", claimConstraintAllow},
		{"ALLOW", claimConstraintAllow},
		{"other", claimConstraintAllow},
	}
	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			t.Parallel()
			if got := normalizeClaimConstraintAction(tt.action); got != tt.want {
				t.Fatalf("normalizeClaimConstraintAction(%q) = %q, want %q", tt.action, got, tt.want)
			}
		})
	}
}

// TestClaimConstraintNormalizeReason keeps the reason label a closed enum:
// values outside the four known reasons land on "other" instead of minting a
// new series.
func TestClaimConstraintNormalizeReason(t *testing.T) {
	t.Parallel()

	tests := []struct {
		reason string
		want   string
	}{
		{claimConstraintReasonAllow, claimConstraintReasonAllow},
		{claimConstraintReasonCanary, claimConstraintReasonCanary},
		{claimConstraintReasonError, claimConstraintReasonError},
		{claimConstraintReasonOther, claimConstraintReasonOther},
		{"", claimConstraintReasonOther},
		{"quota_exceeded", claimConstraintReasonOther},
		{"Allowed", claimConstraintReasonOther},
	}
	for _, tt := range tests {
		t.Run(tt.reason, func(t *testing.T) {
			t.Parallel()
			if got := normalizeClaimConstraintReason(tt.reason); got != tt.want {
				t.Fatalf("normalizeClaimConstraintReason(%q) = %q, want %q", tt.reason, got, tt.want)
			}
		})
	}
}

// TestClaimConstraintCanaryPolicy pins the built-in canary: one fixed decision,
// no scheduling semantics.
func TestClaimConstraintCanaryPolicy(t *testing.T) {
	t.Parallel()

	var p ClaimConstraintPolicy = ClaimConstraintCanaryPolicy{}
	got := p.EvaluateClaimConstraint(context.Background(), ClaimConstraintRequest{Candidates: 3})
	want := ClaimConstraintDecision{Action: claimConstraintAllow, Reason: claimConstraintReasonCanary}
	if got != want {
		t.Fatalf("canary policy = %+v, want %+v", got, want)
	}
}

// TestClaimConstraintNilPolicyRecordsNothing is the nil-default regression:
// with ClaimConstraints nil (every deployment that has not opted in), the
// evaluation helper returns before touching the policy or the metric, so every
// prewarmed claim_constraint series stays at zero.
func TestClaimConstraintNilPolicyRecordsNothing(t *testing.T) {
	t.Parallel()

	m := obsmetrics.NewBusinessMetrics()
	svc := &TaskService{Metrics: m} // ClaimConstraints nil

	svc.evaluateClaimConstraint(context.Background(), pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, claimPathRuntime, 1)
	svc.evaluateClaimConstraint(context.Background(), pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, claimPathBatch, 2)

	for _, s := range claimConstraintZeroSeries(t, m) {
		if s.value != 0 {
			t.Fatalf("claim_constraint series {%s,%s,%s} = %v with nil policy, want 0", s.claimPath, s.decision, s.reason, s.value)
		}
	}
}

// TestClaimConstraintNilMetricsIsSafe covers self-hosted deployments and tests
// that run without the metrics listener: the funnel must not panic when a
// policy is wired but Metrics is nil.
func TestClaimConstraintNilMetricsIsSafe(t *testing.T) {
	t.Parallel()

	svc := &TaskService{ClaimConstraints: ClaimConstraintCanaryPolicy{}}
	svc.evaluateClaimConstraint(context.Background(), pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, claimPathRuntime, 1)
}

// TestClaimConstraintIncrementsLabeledSeries verifies the funnel lands each
// decision on exactly its (claim_path, decision, reason) series and that
// unknown action/reason values normalize independently (a bogus action folds
// to allow while a valid reason survives; an unknown reason folds to other
// while a valid action survives).
func TestClaimConstraintIncrementsLabeledSeries(t *testing.T) {
	t.Parallel()

	m := obsmetrics.NewBusinessMetrics()
	stub := &stubClaimConstraintPolicy{}
	svc := &TaskService{Metrics: m, ClaimConstraints: stub}

	stub.resp = ClaimConstraintDecision{Action: claimConstraintAllow, Reason: claimConstraintReasonCanary}
	svc.evaluateClaimConstraint(context.Background(), pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, claimPathRuntime, 1)
	stub.resp = ClaimConstraintDecision{Action: claimConstraintWait, Reason: "later"}
	svc.evaluateClaimConstraint(context.Background(), pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, claimPathBatch, 2)
	stub.resp = ClaimConstraintDecision{Action: claimConstraintDeny, Reason: "x"}
	svc.evaluateClaimConstraint(context.Background(), pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, claimPathRuntime, 1)
	stub.resp = ClaimConstraintDecision{Action: "bogus", Reason: claimConstraintReasonError}
	svc.evaluateClaimConstraint(context.Background(), pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, claimPathRuntime, 1)

	if stub.calls != 4 {
		t.Fatalf("policy evaluated %d times, want 4", stub.calls)
	}
	if stub.last.ClaimPath != claimPathRuntime || stub.last.Candidates != 1 {
		t.Fatalf("last request = %+v, want claim_path=runtime candidates=1", stub.last)
	}

	tests := []struct {
		claimPath, decision, reason string
		want                        float64
	}{
		{claimPathRuntime, claimConstraintAllow, claimConstraintReasonCanary, 1},
		{claimPathBatch, claimConstraintWait, claimConstraintReasonOther, 1},
		{claimPathRuntime, claimConstraintDeny, claimConstraintReasonOther, 1},
		// A bogus action folds to allow (fail-open); the valid reason survives.
		{claimPathRuntime, claimConstraintAllow, claimConstraintReasonError, 1},
		// Prewarmed series that have not fired read as zero.
		{claimPathBatch, claimConstraintAllow, claimConstraintReasonCanary, 0},
		{claimPathBatch, claimConstraintDeny, claimConstraintReasonError, 0},
		{claimPathRuntime, claimConstraintWait, claimConstraintReasonOther, 0},
	}
	for _, tt := range tests {
		t.Run(tt.claimPath+"/"+tt.decision+"/"+tt.reason, func(t *testing.T) {
			t.Parallel()
			if got := claimConstraintCount(t, m, tt.claimPath, tt.decision, tt.reason); got != tt.want {
				t.Fatalf("claim_constraint_decision_total{%s,%s,%s} = %v, want %v", tt.claimPath, tt.decision, tt.reason, got, tt.want)
			}
		})
	}
}

// ---- helpers ---------------------------------------------------------------

// claimConstraintCount reads one (claim_path, decision, reason) series of the
// claim constraint decision counter out of a freshly gathered registry.
func claimConstraintCount(t *testing.T, m *obsmetrics.BusinessMetrics, claimPath, decision, reason string) float64 {
	t.Helper()

	fam := obsmetrics.GatherForTest(t, m)["multica_agent_task_claim_constraint_decision_total"]
	if fam == nil {
		t.Fatalf("multica_agent_task_claim_constraint_decision_total not registered")
	}
	for _, mtr := range fam.GetMetric() {
		if labelValue(mtr, "claim_path") == claimPath && labelValue(mtr, "decision") == decision && labelValue(mtr, "reason") == reason {
			return mtr.GetCounter().GetValue()
		}
	}
	t.Fatalf("no sample for %s/%s/%s", claimPath, decision, reason)
	return 0
}

// claimConstraintSample is one gathered series of the claim constraint counter.
type claimConstraintSample struct {
	claimPath, decision, reason string
	value                       float64
}

// claimConstraintZeroSeries returns every series of the claim constraint
// counter, for assertions that must hold over the whole family (e.g. nothing
// fired at all).
func claimConstraintZeroSeries(t *testing.T, m *obsmetrics.BusinessMetrics) []claimConstraintSample {
	t.Helper()

	fam := obsmetrics.GatherForTest(t, m)["multica_agent_task_claim_constraint_decision_total"]
	if fam == nil {
		t.Fatalf("multica_agent_task_claim_constraint_decision_total not registered")
	}
	var out []claimConstraintSample
	for _, mtr := range fam.GetMetric() {
		out = append(out, claimConstraintSample{
			claimPath: labelValue(mtr, "claim_path"),
			decision:  labelValue(mtr, "decision"),
			reason:    labelValue(mtr, "reason"),
			value:     mtr.GetCounter().GetValue(),
		})
	}
	if len(out) == 0 {
		t.Fatal("claim_constraint family has no series")
	}
	return out
}
