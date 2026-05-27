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
	if d.EstimatedSavedCents != 4400 {
		t.Errorf("estimated saved cents = %d, want 4400", d.EstimatedSavedCents)
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

// TestAggregateDashboard_NoControlNoMeasured confirms a saving with only a
// treatment arm (no holdout yet) reports the estimate but no A/B.
func TestAggregateDashboard_NoControlNoMeasured(t *testing.T) {
	rows := []cerebrodb.DashboardCerebroCostOptimizationRow{
		{SavingKey: "model_routing", Mode: "on", HeldOut: false, Metric: "model_cost",
			RunCount: 5, TotalSavedCents: 500, TotalActualCostCents: 500},
	}
	d, _ := findSaving(aggregateDashboard(rows), "model_routing")
	if d.Measured != nil {
		t.Errorf("no control arm → measured must be nil, got %+v", d.Measured)
	}
	if d.EstimatedSavedCents != 500 {
		t.Errorf("estimated saved cents = %d, want 500", d.EstimatedSavedCents)
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
	if d.EstimatedSavedUnits != 21 {
		t.Errorf("estimated saved units = %d, want 21", d.EstimatedSavedUnits)
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
