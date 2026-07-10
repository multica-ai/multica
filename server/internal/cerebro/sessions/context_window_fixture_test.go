package sessions

// CEREBRO (FIR-2698): context windows come from the model registry; this
// fixture publishes the seed rows the window tests assert on. init() (not
// TestMain — handler_db_test.go owns that) so it runs before every test in
// the package.
//
// modelregistry.Publish pushes BOTH context windows and prices into the
// process-wide pricing table (server/pkg/pricing) — it is one document, not
// two. A fixture that sets only ContextWindow zeroes out every price behind
// it, which silently breaks the cost fallback that other tests in this
// package (e.g. usage_breakdown_db_test.go) rely on. Prices below mirror the
// seed values in server/migrations/9120_cerebro_model_registry.up.sql so this
// fixture cannot drift into a false "cost = 0" reading again.

import (
	"github.com/multica-ai/multica/server/internal/cerebro/modelregistry"
)

func init() {
	entry := func(w int64, inputUSD, outputUSD, cacheReadUSD, cacheWriteUSD float64) modelregistry.ModelEntry {
		return modelregistry.ModelEntry{
			ContextWindow:        w,
			InputUSDPerMtok:      inputUSD,
			OutputUSDPerMtok:     outputUSD,
			CacheReadUSDPerMtok:  cacheReadUSD,
			CacheWriteUSDPerMtok: cacheWriteUSD,
		}
	}
	modelregistry.Publish(modelregistry.Snapshot{
		FallbackModel: "claude-opus-4-1",
		Models: map[string]modelregistry.ModelEntry{
			"claude-fable-5":    entry(1_000_000, 10, 50, 1, 12.5),
			"claude-opus-4-8":   entry(1_000_000, 5, 25, 0.5, 6.25),
			"claude-opus-4-7":   entry(1_000_000, 5, 25, 0.5, 6.25),
			"claude-opus-4-6":   entry(1_000_000, 5, 25, 0.5, 6.25),
			"claude-opus-4-5":   entry(200_000, 5, 25, 0.5, 6.25),
			"claude-opus-4-1":   entry(200_000, 15, 75, 1.5, 18.75),
			"claude-opus-4":     entry(200_000, 15, 75, 1.5, 18.75),
			"claude-sonnet-5":   entry(1_000_000, 3, 15, 0.3, 3.75),
			"claude-sonnet-4-6": entry(1_000_000, 3, 15, 0.3, 3.75),
			"claude-sonnet-4-5": entry(1_000_000, 3, 15, 0.3, 3.75),
			"claude-sonnet-4":   entry(1_000_000, 3, 15, 0.3, 3.75),
			"claude-haiku-4-5":  entry(200_000, 1, 5, 0.1, 1.25),
			"claude-haiku-3-5":  entry(200_000, 0.8, 4, 0.08, 1),
			"gpt-5.5":           entry(272_000, 5, 30, 0.5, 5),
			"gpt-5":             entry(272_000, 1.25, 10, 0.125, 0),
			"gpt-5-mini":        entry(272_000, 0.25, 2, 0.025, 0),
			"gemini-2.5-pro":    entry(1_000_000, 1.25, 10, 0.3125, 0),
			"gemini-2.5-flash":  entry(1_000_000, 0.075, 0.3, 0.01875, 0),
		},
	}, "1.0.0")
}
