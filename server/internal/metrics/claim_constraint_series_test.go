package metrics

import (
	"testing"
)

// This runs without a database: prewarm is a pure registry operation.

// TestClaimConstraintDecisionSeriesArePrewarmed pins the prewarm grid: every
// (claim_path, decision, reason) combination the service-side normalizers can
// emit is present exactly once with value zero on a fresh BusinessMetrics, so
// an unfired decision reads as zero rather than as a missing series. The
// pedantic registry in GatherForTest fails the gather on duplicate label
// sets, so the exact count also guards against an accidental double seed.
// Keep in sync with claimConstraintSeries when a value is added.
func TestClaimConstraintDecisionSeriesArePrewarmed(t *testing.T) {
	m := NewBusinessMetrics()
	fam := GatherForTest(t, m)["multica_agent_task_claim_constraint_decision_total"]
	if fam == nil {
		t.Fatal("multica_agent_task_claim_constraint_decision_total not registered")
	}
	if got := len(fam.GetMetric()); got != 24 {
		t.Fatalf("prewarmed series = %d, want 24 (2 claim paths x 3 decisions x 4 reasons)", got)
	}
	for _, mtr := range fam.GetMetric() {
		if v := mtr.GetCounter().GetValue(); v != 0 {
			t.Fatalf("prewarmed series %v = %v, want 0", mtr.Label, v)
		}
	}
}
