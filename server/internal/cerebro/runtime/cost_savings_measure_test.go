package runtime

import (
	"testing"

	"github.com/multica-ai/multica/server/pkg/pricing"
)

const (
	testExpensiveModel = "claude-opus-4-7"
	testCheapModel     = "claude-haiku-4-5"
)

func findMeasurement(ms []CostSavingMeasurement, key string) (CostSavingMeasurement, bool) {
	for _, m := range ms {
		if m.SavingKey == key {
			return m, true
		}
	}
	return CostSavingMeasurement{}, false
}

func TestMeasureRun_OffSavingsRecordNothing(t *testing.T) {
	// No modes at all (every saving off) → no measurements.
	got := measureRun(map[string]string{}, CostSavingRunFacts{InlinedContextReads: 3, ToolResultChars: 400}, testCheapModel)
	if len(got) != 0 {
		t.Fatalf("expected no measurements for all-off, got %d: %+v", len(got), got)
	}
}

func TestMeasureRun_SnapshotPromptCountsInlinedReads(t *testing.T) {
	got := measureRun(map[string]string{savingSnapshotPrompt: savingModeShadow},
		CostSavingRunFacts{InlinedContextReads: 2}, "")
	m, ok := findMeasurement(got, savingSnapshotPrompt)
	if !ok {
		t.Fatalf("snapshot_prompt measurement missing: %+v", got)
	}
	if m.Applied {
		t.Errorf("shadow mode must not be applied")
	}
	if m.Metric != metricInputTokens || m.Baseline != 1600 || m.Effective != 0 {
		t.Errorf("unexpected snapshot measurement: %+v", m)
	}
}

func TestMeasureRun_SnapshotPromptSkipsWhenNothingInlined(t *testing.T) {
	got := measureRun(map[string]string{savingSnapshotPrompt: savingModeOn},
		CostSavingRunFacts{InlinedContextReads: 0}, "")
	if _, ok := findMeasurement(got, savingSnapshotPrompt); ok {
		t.Errorf("snapshot_prompt should be skipped when no reads were inlined")
	}
}

func TestMeasureRun_BundledReadCollapsesToOneCall(t *testing.T) {
	got := measureRun(map[string]string{savingBundledRead: savingModeOn},
		CostSavingRunFacts{InlinedContextReads: 3}, "")
	m, ok := findMeasurement(got, savingBundledRead)
	if !ok {
		t.Fatalf("bundled_read measurement missing: %+v", got)
	}
	if !m.Applied || m.Metric != metricInputTokens || m.Baseline != 2250 || m.Effective != 2200 {
		t.Errorf("unexpected bundled measurement: %+v", m)
	}
}

func TestMeasureRun_BundledReadSkipsWhenSingleRead(t *testing.T) {
	got := measureRun(map[string]string{savingBundledRead: savingModeShadow},
		CostSavingRunFacts{InlinedContextReads: 1}, "")
	if _, ok := findMeasurement(got, savingBundledRead); ok {
		t.Errorf("bundled_read should be skipped when only one read was inlined")
	}
}

func TestMeasureRun_ModelRoutingShadowEstimatesCheaperCost(t *testing.T) {
	usage := pricing.Usage{InputTokens: 100_000, OutputTokens: 20_000}
	expensiveCents := pricing.ComputeCents(testExpensiveModel, usage)
	cheapCents := pricing.ComputeCents(testCheapModel, usage)
	if expensiveCents <= cheapCents {
		t.Fatalf("test models not ordered as expected: expensive=%d cheap=%d", expensiveCents, cheapCents)
	}

	// Shadow: run executed on the expensive model; effective is the hypothetical
	// cheap cost; saved = expensive(actual) - cheap.
	got := measureRun(map[string]string{savingModelRouting: savingModeShadow},
		CostSavingRunFacts{RequestedModel: testExpensiveModel, EffectiveModel: testExpensiveModel, Usage: usage, ActualCostCents: expensiveCents},
		testCheapModel)
	m, ok := findMeasurement(got, savingModelRouting)
	if !ok {
		t.Fatalf("model_routing measurement missing: %+v", got)
	}
	if m.Applied {
		t.Errorf("shadow must not be applied")
	}
	if m.Metric != metricModelCost || m.Baseline != expensiveCents || m.Effective != cheapCents {
		t.Errorf("unexpected shadow routing measurement: %+v (want baseline=%d effective=%d)", m, expensiveCents, cheapCents)
	}
	if m.SavedCents != expensiveCents-cheapCents {
		t.Errorf("saved_cents = %d, want %d", m.SavedCents, expensiveCents-cheapCents)
	}
}

func TestMeasureRun_ModelRoutingOnMeasuresActualAgainstExpensive(t *testing.T) {
	usage := pricing.Usage{InputTokens: 100_000, OutputTokens: 20_000}
	expensiveCents := pricing.ComputeCents(testExpensiveModel, usage)
	actualCheap := pricing.ComputeCents(testCheapModel, usage)

	// On: run already executed on the cheap model; baseline is the expensive
	// hypothetical; effective is the measured actual cost.
	got := measureRun(map[string]string{savingModelRouting: savingModeOn},
		CostSavingRunFacts{RequestedModel: testExpensiveModel, EffectiveModel: testCheapModel, Usage: usage, ActualCostCents: actualCheap},
		testCheapModel)
	m, _ := findMeasurement(got, savingModelRouting)
	if !m.Applied || m.Baseline != expensiveCents || m.Effective != actualCheap || m.SavedCents != expensiveCents-actualCheap {
		t.Errorf("unexpected on routing measurement: %+v (want baseline=%d effective=%d)", m, expensiveCents, actualCheap)
	}
}

func TestMeasureRun_ModelRoutingSkippedWhenNoCheapModel(t *testing.T) {
	got := measureRun(map[string]string{savingModelRouting: savingModeOn},
		CostSavingRunFacts{RequestedModel: testExpensiveModel, Usage: pricing.Usage{InputTokens: 1000}, ActualCostCents: 5}, "")
	if _, ok := findMeasurement(got, savingModelRouting); ok {
		t.Errorf("model_routing must be skipped when no cheap model is configured")
	}
}

func TestMeasureRun_PruneToolResultsApproximatesTokens(t *testing.T) {
	got := measureRun(map[string]string{savingPruneToolResults: savingModeShadow},
		CostSavingRunFacts{ToolResultChars: 400}, "")
	m, ok := findMeasurement(got, savingPruneToolResults)
	if !ok {
		t.Fatalf("prune_tool_results measurement missing: %+v", got)
	}
	if m.Metric != metricContextTokens || m.Baseline != 100 || m.Effective != 0 {
		t.Errorf("unexpected prune measurement: %+v (want baseline=100)", m)
	}
}

// FIR-2572: when the run reports both its total prompt characters and the
// provider's prompt tokens, the prune saving's baseline tokens must reflect the
// REAL calibrated chars-per-token ratio for the run — not the chars/4 estimate.
// This is what makes the 100% (holdout 0) measurement real with no control arm.
func TestMeasureRun_PruneToolResultsCalibratesFromRealUsage(t *testing.T) {
	// 20000 prompt chars / 4000 prompt tokens => 5 chars/token for THIS run.
	// 1000 pruned chars => 1000 * 4000 / 20000 = 200 saved tokens.
	// The chars/4 fallback would (wrongly) give 250, so 200 proves calibration.
	facts := CostSavingRunFacts{
		EffectiveModel:               testCheapModel,
		ToolResultChars:              4000,
		PrunedContextCharsCompounded: 1000,
		PromptInputChars:             20_000,
		Usage:                        pricing.Usage{InputTokens: 4000},
	}
	got := measureRun(map[string]string{savingPruneToolResults: savingModeOn}, facts, "")
	m, ok := findMeasurement(got, savingPruneToolResults)
	if !ok {
		t.Fatalf("prune_tool_results measurement missing: %+v", got)
	}
	if !m.Applied {
		t.Errorf("on (not held out) run must be applied")
	}
	const wantTokens = int64(200)
	if m.Baseline != wantTokens {
		t.Errorf("calibrated baseline tokens = %d, want %d (chars/4 would be 250)", m.Baseline, wantTokens)
	}
	wantCents := pricing.ComputeCents(testCheapModel, pricing.Usage{InputTokens: wantTokens})
	if m.SavedCents != wantCents {
		t.Errorf("calibrated saved_cents = %d, want %d", m.SavedCents, wantCents)
	}
}

// Cache-read/-write tokens are prompt tokens and must count in the calibration
// denominator, else a cache-heavy run reports too few tokens per character.
func TestMeasureRun_PruneCalibrationIncludesCacheTokens(t *testing.T) {
	// Same effective ratio as above (4000 prompt tokens), but split across cache.
	facts := CostSavingRunFacts{
		EffectiveModel:               testCheapModel,
		ToolResultChars:              4000,
		PrunedContextCharsCompounded: 1000,
		PromptInputChars:             20_000,
		Usage:                        pricing.Usage{InputTokens: 2000, CacheReadTokens: 1000, CacheWriteTokens: 1000},
	}
	got := measureRun(map[string]string{savingPruneToolResults: savingModeOn}, facts, "")
	m, _ := findMeasurement(got, savingPruneToolResults)
	if m.Baseline != 200 {
		t.Errorf("calibrated baseline with cache tokens = %d, want 200", m.Baseline)
	}
}

// With no calibration data (no prompt chars / no prompt tokens) the saving must
// fall back to the documented chars/4 estimate.
func TestMeasureRun_PruneFallsBackToCharsPerTokenWhenUncalibrated(t *testing.T) {
	facts := CostSavingRunFacts{
		EffectiveModel:               testCheapModel,
		ToolResultChars:              4000,
		PrunedContextCharsCompounded: 1000,
		// PromptInputChars and Usage left zero => no calibration possible.
	}
	got := measureRun(map[string]string{savingPruneToolResults: savingModeOn}, facts, "")
	m, _ := findMeasurement(got, savingPruneToolResults)
	if m.Baseline != int64(1000)/approxCharsPerToken {
		t.Errorf("uncalibrated baseline = %d, want chars/4 = %d", m.Baseline, int64(1000)/approxCharsPerToken)
	}
}

// prune_tool_results held out: pruning was deliberately withheld for this run,
// so the measurement must be the control arm (Applied=false, HeldOut=true) and
// fall back to the full tool-result surface as the would-save, mirroring the
// model-routing held-out control.
func TestMeasureRun_PruneToolResultsHeldOutIsControl(t *testing.T) {
	got := measureRun(map[string]string{savingPruneToolResults: savingModeOn},
		CostSavingRunFacts{ToolResultChars: 400, PruneHeldOut: true}, "")
	m, ok := findMeasurement(got, savingPruneToolResults)
	if !ok {
		t.Fatalf("prune_tool_results measurement missing: %+v", got)
	}
	if m.Applied {
		t.Errorf("held-out control must not be applied")
	}
	if !m.HeldOut {
		t.Errorf("held-out control must have HeldOut=true")
	}
	if m.Metric != metricContextTokens || m.Baseline != 100 || m.Effective != 0 {
		t.Errorf("unexpected held-out prune measurement: %+v (want baseline=100)", m)
	}
}

func TestEffectiveModelForRun(t *testing.T) {
	cases := []struct {
		name      string
		modes     map[string]string
		cheap     string
		requested string
		want      string
	}{
		{"off keeps requested", map[string]string{}, testCheapModel, testExpensiveModel, testExpensiveModel},
		{"shadow keeps requested", map[string]string{savingModelRouting: savingModeShadow}, testCheapModel, testExpensiveModel, testExpensiveModel},
		{"on routes to cheap", map[string]string{savingModelRouting: savingModeOn}, testCheapModel, testExpensiveModel, testCheapModel},
		{"on without cheap keeps requested", map[string]string{savingModelRouting: savingModeOn}, "", testExpensiveModel, testExpensiveModel},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := effectiveModelForRun(tc.modes, tc.requested, tc.cheap); got != tc.want {
				t.Errorf("effectiveModelForRun = %q, want %q", got, tc.want)
			}
		})
	}
}
