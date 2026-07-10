package runtime

// CEREBRO (FIR-2698): the process-wide price table used by pricing.ComputeCents
// / pricing.Known is no longer a hardcoded map — it is injected by the model
// registry (server/internal/cerebro/modelregistry), which is empty until
// something publishes to it. cost_savings_measure_test.go and
// cost_savings_holdout_test.go rely on testExpensiveModel (claude-opus-4-7)
// and testCheapModel (claude-haiku-4-5) being registered ("known") so their
// real discounted rates apply and differ from each other — without this
// fixture both fall through to the same fail-safe rate (equal costs) or, in
// the measureRun "no fabrication" path, to 0, which was exactly
// TestMeasureRun_ModelRoutingShadowEstimatesCheaperCost and
// TestMeasureRun_PruneToolResultsPricesTokens's failures. init() (not
// TestMain — account_test.go owns that) so it runs before every test here.

import (
	"github.com/multica-ai/multica/server/internal/cerebro/modelregistry"
)

func init() {
	modelregistry.Publish(modelregistry.Snapshot{
		FallbackModel: "claude-opus-4-1",
		Models: map[string]modelregistry.ModelEntry{
			"claude-opus-4-7": {
				ContextWindow:        1_000_000,
				InputUSDPerMtok:      5,
				OutputUSDPerMtok:     25,
				CacheReadUSDPerMtok:  0.5,
				CacheWriteUSDPerMtok: 6.25,
			},
			"claude-haiku-4-5": {
				ContextWindow:        200_000,
				InputUSDPerMtok:      1,
				OutputUSDPerMtok:     5,
				CacheReadUSDPerMtok:  0.1,
				CacheWriteUSDPerMtok: 1.25,
			},
			"claude-opus-4-1": {
				ContextWindow:        200_000,
				InputUSDPerMtok:      15,
				OutputUSDPerMtok:     75,
				CacheReadUSDPerMtok:  1.5,
				CacheWriteUSDPerMtok: 18.75,
			},
		},
	}, "1.0.0")
}
