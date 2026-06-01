package runtime

import (
	"context"
	"log/slog"
	"testing"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

// TestResolveHoldoutPct_PrefersWorkspaceOverride is the FIR-2640 integration
// proof that a real run respects the holdout chosen in the UI, not just the env
// var: resolveHoldoutPct reads the per-workspace cerebro_cost_optimization_holdout
// row when present and falls back to the env/default config otherwise. It reuses
// the package's shared DB fixture (TestMain in account_test.go) and skips when no
// database is reachable, matching the other integration tests in this package.
func TestResolveHoldoutPct_PrefersWorkspaceOverride(t *testing.T) {
	if runtimeAccountTestPool == nil {
		t.Skip("DATABASE_URL not configured; skipping holdout integration test")
	}
	ctx := context.Background()
	queries := cerebrodb.New(runtimeAccountTestPool)
	wsID := runtimeAccountTestWSID

	const routing = "model_routing"
	const prune = "prune_tool_results"

	t.Cleanup(func() {
		for _, key := range []string{routing, prune} {
			_ = queries.DeleteCerebroCostOptimizationHoldout(context.Background(), cerebrodb.DeleteCerebroCostOptimizationHoldoutParams{
				WorkspaceID: wsID, SavingKey: key,
			})
		}
	})

	envFallback := 25
	e := &FirtalGatewayExecutor{
		cerebro: queries,
		cfg:     FirtalGatewayRuntimeConfig{CostSavingHoldoutPct: &envFallback},
		logger:  slog.Default(),
	}

	upsert := func(key string, pct int32) {
		if err := queries.UpsertCerebroCostOptimizationHoldout(ctx, cerebrodb.UpsertCerebroCostOptimizationHoldoutParams{
			WorkspaceID: wsID, SavingKey: key, HoldoutPct: pct,
		}); err != nil {
			t.Fatalf("upsert holdout %s=%d: %v", key, pct, err)
		}
	}

	// No override row -> env/default fallback wins for every saving.
	for _, key := range []string{routing, prune} {
		_ = queries.DeleteCerebroCostOptimizationHoldout(ctx, cerebrodb.DeleteCerebroCostOptimizationHoldoutParams{WorkspaceID: wsID, SavingKey: key})
	}
	if got := e.resolveHoldoutPct(ctx, wsID, routing); got != 25 {
		t.Fatalf("no override: got %d, want 25 (env fallback)", got)
	}

	// Each saving is independent: routing at full rollout (0), prune at 60.
	upsert(routing, 0)
	upsert(prune, 60)
	if got := e.resolveHoldoutPct(ctx, wsID, routing); got != 0 {
		t.Fatalf("routing full rollout override: got %d, want 0", got)
	}
	if got := e.resolveHoldoutPct(ctx, wsID, prune); got != 60 {
		t.Fatalf("prune override: got %d, want 60 (UI value)", got)
	}

	// Clearing one saving reverts only that saving to the env fallback; the
	// other keeps its override.
	if err := queries.DeleteCerebroCostOptimizationHoldout(ctx, cerebrodb.DeleteCerebroCostOptimizationHoldoutParams{WorkspaceID: wsID, SavingKey: prune}); err != nil {
		t.Fatalf("clear prune: %v", err)
	}
	if got := e.resolveHoldoutPct(ctx, wsID, prune); got != 25 {
		t.Fatalf("prune after reset: got %d, want 25 (env fallback)", got)
	}
	if got := e.resolveHoldoutPct(ctx, wsID, routing); got != 0 {
		t.Fatalf("routing still overridden: got %d, want 0", got)
	}
}
