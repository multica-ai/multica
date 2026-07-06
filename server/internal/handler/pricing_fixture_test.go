package handler

// CEREBRO-PATCH(handler-pricing-fixture): FIR-2698 the process-wide price
// table used by pricing.ComputeCents is no longer a hardcoded map — it is
// injected by the model registry
// (server/internal/cerebro/modelregistry), which is empty until something
// publishes to it. budget_test.go's TestReportTaskUsage_RollsUpBudgetState
// prices real usage rows against claude-sonnet-4-6 and expects the same list
// price the registry seed carries (300 cents/Mtok input + 1500 cents/Mtok
// output = 1800 cents for 1M+1M tokens); without this fixture the lookup
// falls through to the fail-safe worst-case rate and the budget-rollup math
// is off. init() (not TestMain — handler_test.go owns that) so it runs
// before every test in this package. Deliberately does NOT register
// "model-not-yet-priced" — TestReportTaskUsage_UnknownModelUsesFallback
// relies on that model staying unknown so the fail-safe Opus rate applies.
import (
	"github.com/multica-ai/multica/server/internal/cerebro/modelregistry"
)

func init() {
	modelregistry.Publish(modelregistry.Snapshot{
		FallbackModel: "claude-opus-4-1",
		Models: map[string]modelregistry.ModelEntry{
			"claude-sonnet-4-6": {
				ContextWindow:        1_000_000,
				InputUSDPerMtok:      3,
				OutputUSDPerMtok:     15,
				CacheReadUSDPerMtok:  0.3,
				CacheWriteUSDPerMtok: 3.75,
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
