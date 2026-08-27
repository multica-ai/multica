package service

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestPricingBreachStickyDowngrade verifies that a price breach marks the model
// unhealthy in a way that does NOT auto-recover via the 10m model_health TTL:
// last_failure_at is pushed far into the future, so isModelHealthyWithQueries
// keeps returning false (resolver keeps using the fallback) until the price
// drops and the watcher explicitly marks the model healthy again.
func TestPricingBreachStickyDowngrade(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)

	if _, err := q.UpsertGlobalModelTier(ctx, db.UpsertGlobalModelTierParams{
		Tier:             "balanced",
		Concrete:         "primary-model",
		FallbackConcrete: []string{"fallback-a"},
	}); err != nil {
		t.Fatalf("upsert tier: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM model_tier_map WHERE tier='balanced' AND workspace_id IS NULL`)
		pool.Exec(context.Background(), `DELETE FROM model_pricing WHERE concrete='primary-model'`)
		pool.Exec(context.Background(), `DELETE FROM model_health WHERE concrete='primary-model'`)
	})

	var thr, pr pgtype.Numeric
	if err := thr.Scan("0.005"); err != nil {
		t.Fatalf("scan thr: %v", err)
	}
	if err := pr.Scan("0.01"); err != nil {
		t.Fatalf("scan price: %v", err)
	}
	if _, err := q.UpsertModelPricing(ctx, db.UpsertModelPricingParams{
		Concrete:              "primary-model",
		InputUsdPerMtok:       pr,
		ThresholdInputUsdPerMtok: thr,
	}); err != nil {
		t.Fatalf("upsert pricing: %v", err)
	}

	watcher := &ModelPricingWatcher{Queries: q}
	if err := watcher.CheckOnce(ctx); err != nil {
		t.Fatalf("watcher check: %v", err)
	}

	ws := pgtype.UUID{}
	h, err := q.GetModelHealth(ctx, db.GetModelHealthParams{WorkspaceID: ws, Concrete: "primary-model"})
	if err != nil {
		t.Fatalf("get health: %v", err)
	}
	if h.Status != "unhealthy" || h.Reason.String != "pricing" {
		t.Fatalf("expected pricing unhealthy, got status=%q reason=%q", h.Status, h.Reason.String)
	}
	// Sticky: last_failure_at must be far in the future so the 10m TTL never flips
	// this row back to healthy on its own.
	if !h.LastFailureAt.Valid {
		t.Fatalf("expected LastFailureAt to be set")
	}
	if !h.LastFailureAt.Time.After(time.Now().Add(100 * 24 * time.Hour)) {
		t.Fatalf("expected LastFailureAt far in the future (>100d), got %v", h.LastFailureAt.Time)
	}

	svc := &TaskService{Queries: q}
	if svc.isModelHealthyWithQueries(ctx, q, ws, "primary-model") {
		t.Fatalf("breached model should be unhealthy (sticky), but isModelHealthyWithQueries returned true")
	}
	got := svc.resolveConcreteModel(ctx, ws, "balanced")
	if got != "fallback-a" {
		t.Fatalf("breach resolver should use fallback-a, got %q", got)
	}

	// Recovery: drop price below threshold.
	var low pgtype.Numeric
	if err := low.Scan("0.001"); err != nil {
		t.Fatalf("scan low: %v", err)
	}
	if _, err := q.UpsertModelPricing(ctx, db.UpsertModelPricingParams{
		Concrete:                "primary-model",
		InputUsdPerMtok:         low,
		ThresholdInputUsdPerMtok: thr,
	}); err != nil {
		t.Fatalf("upsert low: %v", err)
	}
	if err := watcher.CheckOnce(ctx); err != nil {
		t.Fatalf("watcher recovery check: %v", err)
	}

	h, err = q.GetModelHealth(ctx, db.GetModelHealthParams{WorkspaceID: ws, Concrete: "primary-model"})
	if err != nil {
		t.Fatalf("get health after recovery: %v", err)
	}
	if h.Status != "healthy" {
		t.Fatalf("expected healthy after recovery, got %q", h.Status)
	}
	if !svc.isModelHealthyWithQueries(ctx, q, ws, "primary-model") {
		t.Fatalf("recovered model should be healthy, but isModelHealthyWithQueries returned false")
	}
	got = svc.resolveConcreteModel(ctx, ws, "balanced")
	if got != "primary-model" {
		t.Fatalf("after recovery resolver should return primary, got %q", got)
	}
}
