package runtime

import (
	"testing"

	"github.com/multica-ai/multica/server/pkg/pricing"

	"github.com/jackc/pgx/v5/pgtype"
)

func uuidFromByte(b byte) pgtype.UUID {
	var u pgtype.UUID
	u.Valid = true
	u.Bytes[0] = b
	return u
}

func TestChooseHoldoutPct(t *testing.T) {
	pct := func(v int) *int { return &v }
	cases := []struct {
		name     string
		override *int
		fallback int
		want     int
	}{
		{"no override uses fallback", nil, 20, 20},
		{"override wins over fallback", pct(0), 20, 0},
		{"override 100 (full holdout)", pct(100), 20, 100},
		{"negative override clamps to 0", pct(-5), 20, 0},
		{"over-100 override clamps to 100", pct(150), 20, 100},
		{"negative fallback clamps to 0", nil, -5, 0},
		{"over-100 fallback clamps to 100", nil, 150, 100},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := chooseHoldoutPct(tc.override, tc.fallback); got != tc.want {
				t.Errorf("chooseHoldoutPct(%v, %d) = %d, want %d", tc.override, tc.fallback, got, tc.want)
			}
		})
	}
}

func TestCostSavingHeldOut_BoundsAndDisabled(t *testing.T) {
	id := uuidFromByte(1)
	if costSavingHeldOut(id, savingModelRouting, 0) {
		t.Errorf("pct 0 must never hold out")
	}
	if !costSavingHeldOut(id, savingModelRouting, 100) {
		t.Errorf("pct 100 must always hold out")
	}
	if costSavingHeldOut(pgtype.UUID{}, savingModelRouting, 50) {
		t.Errorf("invalid task id must not hold out")
	}
}

func TestCostSavingHeldOut_Deterministic(t *testing.T) {
	id := uuidFromByte(42)
	first := costSavingHeldOut(id, savingModelRouting, 30)
	for i := 0; i < 100; i++ {
		if costSavingHeldOut(id, savingModelRouting, 30) != first {
			t.Fatalf("holdout decision must be stable for the same task+saving")
		}
	}
}

// TestCostSavingHeldOut_ApproximatesShare checks the random holdout converges to
// the configured percentage across many distinct task IDs.
func TestCostSavingHeldOut_ApproximatesShare(t *testing.T) {
	const pct = 20
	held := 0
	const n = 2000
	for i := 0; i < n; i++ {
		var u pgtype.UUID
		u.Valid = true
		u.Bytes[0] = byte(i)
		u.Bytes[1] = byte(i >> 8)
		if costSavingHeldOut(u, savingModelRouting, pct) {
			held++
		}
	}
	share := float64(held) / float64(n) * 100
	// Generous band — this guards against a broken hash (e.g. always 0 or 100),
	// not statistical precision.
	if share < float64(pct)-7 || share > float64(pct)+7 {
		t.Errorf("holdout share %.1f%% too far from configured %d%%", share, pct)
	}
}

// TestMeasureRun_ModelRoutingHeldOutIsControl verifies an "on" run that was held
// out is recorded as a not-applied control arm that ran on the expensive model.
func TestMeasureRun_ModelRoutingHeldOutIsControl(t *testing.T) {
	usage := pricing.Usage{InputTokens: 100_000, OutputTokens: 20_000}
	expensiveCents := pricing.ComputeCents(testExpensiveModel, usage)
	cheapCents := pricing.ComputeCents(testCheapModel, usage)

	got := measureRun(map[string]string{savingModelRouting: savingModeOn},
		CostSavingRunFacts{
			RequestedModel:  testExpensiveModel,
			EffectiveModel:  testExpensiveModel, // held out → ran on expensive
			Usage:           usage,
			ActualCostCents: expensiveCents,
			RoutingHeldOut:  true,
		},
		testCheapModel)
	m, ok := findMeasurement(got, savingModelRouting)
	if !ok {
		t.Fatalf("model_routing measurement missing: %+v", got)
	}
	if m.Applied {
		t.Errorf("held-out run must not be marked applied")
	}
	if !m.HeldOut {
		t.Errorf("held-out run must set HeldOut")
	}
	if m.ActualCostCents != expensiveCents {
		t.Errorf("ActualCostCents = %d, want expensive %d", m.ActualCostCents, expensiveCents)
	}
	// Control arm ran expensive: baseline = actual(expensive), effective = cheap.
	if m.Baseline != expensiveCents || m.Effective != cheapCents {
		t.Errorf("held-out baseline/effective = %d/%d, want %d/%d", m.Baseline, m.Effective, expensiveCents, cheapCents)
	}
}

// TestMeasureRun_AppliedRunRecordsActualCost confirms a treatment run carries the
// run's real billed cost so the dashboard can average it.
func TestMeasureRun_AppliedRunRecordsActualCost(t *testing.T) {
	usage := pricing.Usage{InputTokens: 100_000, OutputTokens: 20_000}
	actualCheap := pricing.ComputeCents(testCheapModel, usage)
	got := measureRun(map[string]string{savingModelRouting: savingModeOn},
		CostSavingRunFacts{RequestedModel: testExpensiveModel, EffectiveModel: testCheapModel, Usage: usage, ActualCostCents: actualCheap},
		testCheapModel)
	m, _ := findMeasurement(got, savingModelRouting)
	if !m.Applied || m.HeldOut {
		t.Errorf("treatment run should be applied and not held out: %+v", m)
	}
	if m.ActualCostCents != actualCheap {
		t.Errorf("ActualCostCents = %d, want %d", m.ActualCostCents, actualCheap)
	}
}

// TestMeasureRun_PruneToolResultsPricesTokens confirms saved context tokens are
// priced in cents on the run's model (no fabrication: 0 for unknown models).
func TestMeasureRun_PruneToolResultsPricesTokens(t *testing.T) {
	got := measureRun(map[string]string{savingPruneToolResults: savingModeShadow},
		CostSavingRunFacts{EffectiveModel: testCheapModel, ToolResultChars: 40_000}, "")
	m, _ := findMeasurement(got, savingPruneToolResults)
	wantTokens := int64(40_000) / approxCharsPerToken
	wantCents := pricing.ComputeCents(testCheapModel, pricing.Usage{InputTokens: wantTokens})
	if m.Baseline != wantTokens {
		t.Errorf("prune baseline tokens = %d, want %d", m.Baseline, wantTokens)
	}
	if m.SavedCents != wantCents {
		t.Errorf("prune saved_cents = %d, want %d", m.SavedCents, wantCents)
	}

	unknown := measureRun(map[string]string{savingPruneToolResults: savingModeShadow},
		CostSavingRunFacts{EffectiveModel: "made-up-model", ToolResultChars: 40_000}, "")
	mu, _ := findMeasurement(unknown, savingPruneToolResults)
	if mu.SavedCents != 0 {
		t.Errorf("unknown model must not fabricate cents, got %d", mu.SavedCents)
	}
}

func TestCostSavingHoldoutPct_DefaultAndClamp(t *testing.T) {
	if got := (FirtalGatewayRuntimeConfig{}).costSavingHoldoutPct(); got != defaultCostSavingHoldoutPct {
		t.Errorf("unset holdout pct = %d, want default %d", got, defaultCostSavingHoldoutPct)
	}
	neg, hi, mid := -5, 250, 35
	if got := (FirtalGatewayRuntimeConfig{CostSavingHoldoutPct: &neg}).costSavingHoldoutPct(); got != 0 {
		t.Errorf("negative pct should clamp to 0, got %d", got)
	}
	if got := (FirtalGatewayRuntimeConfig{CostSavingHoldoutPct: &hi}).costSavingHoldoutPct(); got != 100 {
		t.Errorf("over-100 pct should clamp to 100, got %d", got)
	}
	if got := (FirtalGatewayRuntimeConfig{CostSavingHoldoutPct: &mid}).costSavingHoldoutPct(); got != 35 {
		t.Errorf("in-range pct should pass through, got %d", got)
	}
}
