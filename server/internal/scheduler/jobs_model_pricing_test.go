package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/pricing"
)

func TestModelPricingJobRetriesWithinDailyPlan(t *testing.T) {
	prices := pricing.New(nil, nil)
	job := ModelPricingJob(prices)
	if err := job.validate(); err != nil {
		t.Fatal(err)
	}
	if job.MaxAttempts != 4 {
		t.Fatalf("max attempts = %d, want 4", job.MaxAttempts)
	}
	for i, want := range []time.Duration{2 * time.Minute, 10 * time.Minute, 30 * time.Minute} {
		if got := job.retryDelay(i + 1); got != want {
			t.Fatalf("retry %d delay = %s, want %s", i+1, got, want)
		}
	}
	if job.RunTimeout <= 2*prices.Client.Timeout {
		t.Fatal("job timeout must cover both feeds plus database work")
	}
	now := time.Date(2026, 9, 2, 13, 0, 0, 0, time.UTC)
	plan := pricing.Midnight(now, prices.Location)
	latest := LatestPlanInfo{Found: true, PlanTime: plan, Status: "FAILED", Attempt: 1, MaxAttempts: job.MaxAttempts, NextRetryAt: now.Add(-time.Second)}
	plans, err := job.PlansForScope(context.Background(), ScopeGlobal, now, latest)
	if err != nil || len(plans) != 1 || !plans[0].Equal(plan) {
		t.Fatalf("retry replaced its daily plan: %v, %v", plans, err)
	}
}
