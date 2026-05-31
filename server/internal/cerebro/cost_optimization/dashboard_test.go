package cost_optimization

import (
	"testing"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

func findSaving(s []dashboardSaving, key string) (dashboardSaving, bool) {
	for _, d := range s {
		if d.SavingKey == key {
			return d, true
		}
	}
	return dashboardSaving{}, false
}

// TestAggregateDashboard_HoldoutABComputesMeasuredSaving builds a model_routing
// saving with a treatment arm (routed, cheap) and a control arm (held out,
// expensive) and checks the measured A/B is control_avg - treatment_avg.
func TestAggregateDashboard_HoldoutABComputesMeasuredSaving(t *testing.T) {
	rows := []cerebrodb.DashboardCerebroCostOptimizationRow{
		// Treatment: 8 routed runs, total actual 800 cents → avg 100.
		{SavingKey: "model_routing", Mode: "on", HeldOut: false, Metric: "model_cost",
			RunCount: 8, TotalSavedUnits: 4000, TotalSavedCents: 4000, TotalActualCostCents: 800},
		// Control: 2 held-out runs, total actual 600 cents → avg 300.
		{SavingKey: "model_routing", Mode: "on", HeldOut: true, Metric: "model_cost",
			RunCount: 2, TotalSavedUnits: 400, TotalSavedCents: 400, TotalActualCostCents: 600},
	}

	got := aggregateDashboard(rows)
	d, ok := findSaving(got, "model_routing")
	if !ok {
		t.Fatalf("model_routing missing: %+v", got)
	}
	if d.TreatmentRunCount != 8 || d.ControlRunCount != 2 {
		t.Errorf("run counts treatment/control = %d/%d, want 8/2", d.TreatmentRunCount, d.ControlRunCount)
	}
	// Estimated is now the hypothetical from runs where the saving was NOT applied
	// — here only the held-out control arm (400 cents).
	if d.EstimatedSavedCents != 400 {
		t.Errorf("estimated saved cents = %d, want 400 (held-out control only)", d.EstimatedSavedCents)
	}
	// Applied is the real saving accrued on the treatment arm (4000 cents).
	if d.Applied == nil {
		t.Fatalf("expected an applied (treatment) saving")
	}
	if d.Applied.SavedCents != 4000 || d.Applied.RunCount != 8 {
		t.Errorf("applied saved cents/runs = %d/%d, want 4000/8", d.Applied.SavedCents, d.Applied.RunCount)
	}
	if d.Measured == nil {
		t.Fatalf("expected a measured A/B result")
	}
	if d.Measured.TreatmentAvgCostCents != 100 || d.Measured.ControlAvgCostCents != 300 {
		t.Errorf("avg cost treatment/control = %d/%d, want 100/300", d.Measured.TreatmentAvgCostCents, d.Measured.ControlAvgCostCents)
	}
	if d.Measured.SavedPerRunCents != 200 {
		t.Errorf("saved per run = %d, want 200", d.Measured.SavedPerRunCents)
	}
	if d.Measured.TotalSavedCents != 1600 { // 200 * 8 treatment runs
		t.Errorf("total measured saved = %d, want 1600", d.Measured.TotalSavedCents)
	}
}

// TestAggregateDashboard_ContextTokensHoldoutAB builds a prune_tool_results
// saving (metric context_tokens) with a pruned treatment arm and a held-out
// control arm, and checks the measured A/B is control_avg - treatment_avg, the
// same metric-agnostic actual_cost_cents math used for model_cost.
func TestAggregateDashboard_ContextTokensHoldoutAB(t *testing.T) {
	rows := []cerebrodb.DashboardCerebroCostOptimizationRow{
		// Treatment: 10 pruned runs, total actual 1200 cents → avg 120.
		{SavingKey: "prune_tool_results", Mode: "on", HeldOut: false, Metric: "context_tokens",
			RunCount: 10, TotalSavedUnits: 5000, TotalSavedCents: 500, TotalActualCostCents: 1200},
		// Control: 4 held-out runs (pruning withheld), total actual 800 cents → avg 200.
		{SavingKey: "prune_tool_results", Mode: "on", HeldOut: true, Metric: "context_tokens",
			RunCount: 4, TotalSavedUnits: 2000, TotalSavedCents: 200, TotalActualCostCents: 800},
	}

	d, ok := findSaving(aggregateDashboard(rows), "prune_tool_results")
	if !ok {
		t.Fatalf("prune_tool_results missing")
	}
	if d.Measured == nil {
		t.Fatalf("expected a measured A/B result for context_tokens")
	}
	// control avg = 800/4 = 200; treatment avg = 1200/10 = 120; saved = 80.
	if d.Measured.SavedPerRunCents != 80 {
		t.Errorf("saved per run = %d, want 80", d.Measured.SavedPerRunCents)
	}
	if d.Measured.TreatmentAvgCostCents != 120 || d.Measured.ControlAvgCostCents != 200 {
		t.Errorf("avg cost treatment/control = %d/%d, want 120/200", d.Measured.TreatmentAvgCostCents, d.Measured.ControlAvgCostCents)
	}
}

// TestAggregateDashboard_NoControlNoMeasured confirms a saving with only a
// treatment arm (no holdout yet) reports the applied saving but no A/B, and
// nothing under estimated (there were no non-applied runs to estimate from).
func TestAggregateDashboard_NoControlNoMeasured(t *testing.T) {
	rows := []cerebrodb.DashboardCerebroCostOptimizationRow{
		{SavingKey: "model_routing", Mode: "on", HeldOut: false, Metric: "model_cost",
			RunCount: 5, TotalSavedCents: 500, TotalActualCostCents: 500},
	}
	d, _ := findSaving(aggregateDashboard(rows), "model_routing")
	if d.Measured != nil {
		t.Errorf("no control arm → measured must be nil, got %+v", d.Measured)
	}
	if d.EstimatedSavedCents != 0 {
		t.Errorf("estimated saved cents = %d, want 0 (no non-applied runs)", d.EstimatedSavedCents)
	}
	if d.Applied == nil || d.Applied.SavedCents != 500 || d.Applied.RunCount != 5 {
		t.Errorf("applied = %+v, want saved 500 over 5 runs", d.Applied)
	}
}

// TestAggregateDashboard_PruneAt100PercentHasAppliedMeasurement is the FIR-2572
// case: prune_tool_results turned on for everyone (holdout 0, so no control arm).
// There is no holdout A/B, but the dashboard must still expose a real measured
// saving via the applied (treatment) figure — otherwise the page shows only "—"
// and looks broken even though tokens are being saved.
func TestAggregateDashboard_PruneAt100PercentHasAppliedMeasurement(t *testing.T) {
	rows := []cerebrodb.DashboardCerebroCostOptimizationRow{
		{SavingKey: "prune_tool_results", Mode: "on", HeldOut: false, Metric: "context_tokens",
			RunCount: 20, TotalSavedUnits: 9000, TotalSavedCents: 450, TotalActualCostCents: 2400},
	}
	d, ok := findSaving(aggregateDashboard(rows), "prune_tool_results")
	if !ok {
		t.Fatalf("prune_tool_results missing")
	}
	if d.Measured != nil {
		t.Errorf("no control arm at 100%% → holdout A/B must be nil, got %+v", d.Measured)
	}
	if d.Applied == nil {
		t.Fatalf("100%% rollout must still expose an applied (measured) saving")
	}
	if d.Applied.SavedUnits != 9000 || d.Applied.SavedCents != 450 || d.Applied.RunCount != 20 {
		t.Errorf("applied = %+v, want 9000 units / 450 cents / 20 runs", d.Applied)
	}
	if d.EstimatedSavedCents != 0 || d.EstimatedSavedUnits != 0 {
		t.Errorf("estimated should be empty at 100%% (no non-applied runs); got units=%d cents=%d",
			d.EstimatedSavedUnits, d.EstimatedSavedCents)
	}
}

// TestAggregateDashboard_NonCostMetricHasNoMeasured confirms a platform_calls
// saving never produces an A/B (only model_cost actual-cost reflects the saving).
func TestAggregateDashboard_NonCostMetricHasNoMeasured(t *testing.T) {
	rows := []cerebrodb.DashboardCerebroCostOptimizationRow{
		{SavingKey: "bundled_read", Mode: "on", HeldOut: false, Metric: "platform_calls",
			RunCount: 4, TotalSavedUnits: 12},
		{SavingKey: "bundled_read", Mode: "shadow", HeldOut: false, Metric: "platform_calls",
			RunCount: 3, TotalSavedUnits: 9},
	}
	d, _ := findSaving(aggregateDashboard(rows), "bundled_read")
	if d.Measured != nil {
		t.Errorf("platform_calls saving must not produce A/B, got %+v", d.Measured)
	}
	// Estimated is now the shadow (measure-only) arm; the applied arm carries the
	// treatment units.
	if d.EstimatedSavedUnits != 9 {
		t.Errorf("estimated saved units = %d, want 9 (shadow only)", d.EstimatedSavedUnits)
	}
	if d.Applied == nil || d.Applied.SavedUnits != 12 {
		t.Errorf("applied = %+v, want 12 units (treatment)", d.Applied)
	}
	if d.ShadowRunCount != 3 || d.TreatmentRunCount != 4 {
		t.Errorf("shadow/treatment runs = %d/%d, want 3/4", d.ShadowRunCount, d.TreatmentRunCount)
	}
}

// TestAggregateDashboard_StableOrder confirms savings come back sorted by key so
// the dashboard does not reshuffle between polls.
func TestAggregateDashboard_StableOrder(t *testing.T) {
	rows := []cerebrodb.DashboardCerebroCostOptimizationRow{
		{SavingKey: "snapshot_prompt", Mode: "shadow", Metric: "platform_calls", RunCount: 1},
		{SavingKey: "bundled_read", Mode: "shadow", Metric: "platform_calls", RunCount: 1},
		{SavingKey: "model_routing", Mode: "shadow", Metric: "model_cost", RunCount: 1},
	}
	got := aggregateDashboard(rows)
	want := []string{"bundled_read", "model_routing", "snapshot_prompt"}
	for i, w := range want {
		if got[i].SavingKey != w {
			t.Errorf("order[%d] = %q, want %q", i, got[i].SavingKey, w)
		}
	}
}
