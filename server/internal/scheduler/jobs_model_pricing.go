package scheduler

import (
	"context"
	"time"

	"github.com/multica-ai/multica/server/internal/pricing"
)

func ModelPricingJob(prices *pricing.Service) JobSpec {
	return JobSpec{
		Name: "model_pricing_sync", Scopes: StaticScopes(ScopeGlobal), MaxPlansPerTick: 1,
		RunTimeout: 90 * time.Second, StaleTimeout: 3 * time.Minute, HeartbeatInterval: 20 * time.Second,
		AllowStaleReentry: true, MaxAttempts: 4,
		RetryBackoff: []time.Duration{2 * time.Minute, 10 * time.Minute, 30 * time.Minute},
		PlansForScope: func(_ context.Context, _ Scope, now time.Time, _ LatestPlanInfo) ([]time.Time, error) {
			return []time.Time{pricing.Midnight(now, prices.Location)}, nil
		},
		Handler: func(ctx context.Context, _ HandlerInput) (HandlerResult, error) {
			return HandlerResult{}, prices.Refresh(ctx, false)
		},
	}
}
