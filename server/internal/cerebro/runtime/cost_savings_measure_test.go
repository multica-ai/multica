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
	if m.Metric != metricPlatformCalls || m.Baseline != 2 || m.Effective != 0 {
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
	if !m.Applied || m.Metric != metricPlatformCalls || m.Baseline != 3 || m.Effective != 1 {
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
