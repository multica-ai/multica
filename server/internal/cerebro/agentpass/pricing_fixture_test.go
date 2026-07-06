package agentpass

// CEREBRO (FIR-2698): the process-wide price table used by pricing.ComputeCents
// is no longer a hardcoded map — it is injected by the model registry
// (server/internal/cerebro/modelregistry), which is empty until something
// publishes to it. This package's spend-ceiling tests (agentpass_test.go)
// price real usage rows against claude-sonnet-4-6 and expect the same list
// price the registry seed carries; without this fixture every lookup falls
// through to the fail-safe worst-case rate and the spend-ceiling math is off
// by 5x, which was exactly TestEvaluateForEnqueue_SpendAtWarn/Degrade's
// failure (expected "allow", got "deny_exhausted"). init() (not TestMain —
// handler_integration_test.go owns that) so it runs before every test here.

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
