package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ModelPricingWatcher is the minimal pricing reaction stub.
// It polls every pricingPollInterval (default 15m) using a time.Ticker
// and a best-effort DB lease (ticker alone for single-node; distributed
// lease TODO via advisory lock / channel lease table).
// On breach (price > threshold) it flips model_health to unhealthy/pricing + logs alert.
// When price drops below threshold it clears that pricing unhealthiness.
// Webhook alternative (provider price-change POST -> same upsert path) is TODO.
type ModelPricingWatcher struct {
	Queries  *db.Queries
	Interval time.Duration
	Logger   *slog.Logger
}

const pricingPollInterval = 15 * time.Minute

func (w *ModelPricingWatcher) logger() *slog.Logger {
	if w.Logger != nil {
		return w.Logger
	}
	return slog.Default()
}

func (w *ModelPricingWatcher) Run(ctx context.Context) {
	interval := w.Interval
	if interval <= 0 {
		interval = pricingPollInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.CheckOnce(ctx); err != nil {
				w.logger().Warn("model pricing watcher check failed", "error", err)
			}
		}
	}
}

// CheckOnce is the testable single poll iteration: for each model_pricing row
// where threshold is set, compare price vs threshold and flip health.
func (w *ModelPricingWatcher) CheckOnce(ctx context.Context) error {
	if w.Queries == nil {
		return nil
	}
	pricings, err := w.Queries.ListModelPricing(ctx)
	if err != nil {
		return err
	}
	for _, p := range pricings {
		if !p.ThresholdInputUsdPerMtok.Valid || !p.InputUsdPerMtok.Valid {
			continue
		}
		breach := numericGreater(p.InputUsdPerMtok, p.ThresholdInputUsdPerMtok)
		// Pricing health is global (workspace NULL) – concrete is global concept.
		ws := pgtype.UUID{}
		if breach {
			if _, err := w.Queries.UpsertModelHealthUnhealthy(ctx, db.UpsertModelHealthUnhealthyParams{
				WorkspaceID: ws,
				Concrete:    p.Concrete,
				Reason:      pgtype.Text{String: "pricing", Valid: true},
			}); err != nil {
				w.logger().Warn("pricing breach: mark unhealthy failed", "concrete", p.Concrete, "error", err)
				continue
			}
			// Use Float64 for logging brevity; pgtype.Numeric doesn't have String() in this version.
			af, _ := p.InputUsdPerMtok.Float64Value()
			bf, _ := p.ThresholdInputUsdPerMtok.Float64Value()
			w.logger().Warn("model pricing breach", "concrete", p.Concrete, "price", af.Float64, "threshold", bf.Float64)
			// Alert placeholder: log already; webhook TODO
		} else {
			// Clear pricing unhealthiness if currently unhealthy due to pricing.
			// We check existing health first to avoid flipping non-pricing unhealthiness.
			h, err := w.Queries.GetModelHealth(ctx, db.GetModelHealthParams{WorkspaceID: ws, Concrete: p.Concrete})
			if err == nil && h.Status == "unhealthy" && h.Reason.String == "pricing" {
				if err := w.Queries.MarkModelHealthy(ctx, db.MarkModelHealthyParams{WorkspaceID: ws, Concrete: p.Concrete}); err != nil {
					w.logger().Warn("pricing recovery: mark healthy failed", "concrete", p.Concrete, "error", err)
				} else {
					w.logger().Info("model pricing recovered", "concrete", p.Concrete)
				}
			}
		}
	}
	return nil
}

func numericGreater(a, b pgtype.Numeric) bool {
	if !a.Valid || !b.Valid {
		return false
	}
	af, err1 := a.Float64Value()
	bf, err2 := b.Float64Value()
	if err1 != nil || err2 != nil || !af.Valid || !bf.Valid {
		return false
	}
	return af.Float64 > bf.Float64
}
