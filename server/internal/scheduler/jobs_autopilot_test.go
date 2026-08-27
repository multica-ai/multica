package scheduler

import (
	"context"
	"testing"
	"time"
)

func TestAutopilotScheduleOfflineRetryWindow(t *testing.T) {
	job := AutopilotScheduleDispatchJob(nil, nil, nil)

	if job.MaxAttempts != 2 {
		t.Fatalf("scheduled autopilot must have one initial attempt plus one retry; got MaxAttempts=%d", job.MaxAttempts)
	}
	if len(job.RetryBackoff) != 1 || job.RetryBackoff[0] != time.Minute {
		t.Fatalf("scheduled autopilot retry backoff must be exactly [1m]; got %v", job.RetryBackoff)
	}

	// A new occurrence may be admitted up to five minutes late. If that
	// edge attempt fails immediately, the single retry becomes eligible one
	// minute later. This six-minute value is the configured effective
	// lateness under the full chain; planner/tick downtime can delay the
	// durable retry further, as covered by the exemption test below.
	if got, want := maxAutopilotScheduleLateness+job.RetryBackoff[0], 6*time.Minute; got != want {
		t.Fatalf("maximum configured retry lateness=%s want %s", got, want)
	}
}

func TestAutopilotScheduleMinuteCronRetryExemptionAndRecovery(t *testing.T) {
	planTime := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	retryAt := planTime.Add(6 * time.Minute)
	scope := Scope{Kind: ScopeKindAutopilotTrigger, ID: "minute-trigger"}
	cache := newAutopilotScheduleCache()
	cache.replace(map[string]autopilotTriggerConfig{
		scope.ID: {
			TriggerID:      scope.ID,
			CronExpression: "* * * * *",
			Timezone:       "UTC",
			CreatedAt:      planTime.Add(-time.Minute),
		},
	})
	plansForScope := autopilotPlansForScope(cache)
	latest := LatestPlanInfo{
		Found:       true,
		PlanTime:    planTime,
		Status:      "FAILED",
		Attempt:     1,
		MaxAttempts: 2,
		NextRetryAt: retryAt,
	}

	// RetryEligible intentionally wins before the five-minute lateness
	// gate, preserving the one admitted occurrence through its final try.
	plans, err := plansForScope(context.Background(), scope, retryAt, latest)
	if err != nil {
		t.Fatalf("plan retry: %v", err)
	}
	if len(plans) != 1 || !plans[0].Equal(planTime) {
		t.Fatalf("retry must keep original plan_time %s; got %v", planTime, plans)
	}
	if !isAutopilotSchedulePlanStale(retryAt, plans[0]) {
		t.Fatal("test setup must exercise the retry exemption beyond the normal lateness gate")
	}

	// Once the second (final) attempt is exhausted, the old plan no longer
	// pins a high-frequency cursor. Latest-only catch-up collapses all minute
	// occurrences missed during the retry window to the current one.
	latest.Attempt = latest.MaxAttempts
	plans, err = plansForScope(context.Background(), scope, retryAt, latest)
	if err != nil {
		t.Fatalf("plan after retry exhaustion: %v", err)
	}
	if len(plans) != 1 || !plans[0].Equal(retryAt) {
		t.Fatalf("exhausted retry must advance minute cron to latest occurrence %s; got %v", retryAt, plans)
	}
}

// TestAdvancedNextRunStrictlyAfterPlanTime is the regression guard for
// MUL-3749's boundary case: the post-dispatch next_run_at write-back must
// land on the slot AFTER the one that just fired, even when this app
// instance's local clock lags the DB clock that judged the plan due.
// Anchoring naively on time.Now() alone could recompute the just-fired
// slot; advancedNextRun floors the anchor at plan_time to prevent it.
// Uses the reported scenario: hourly cron in America/New_York, fired slot
// 03:00 EDT (07:00 UTC), next slot 04:00 EDT (08:00 UTC).
func TestAdvancedNextRunStrictlyAfterPlanTime(t *testing.T) {
	const cron = "0 * * * *"
	const tz = "America/New_York"
	planTime := time.Date(2026, 6, 26, 7, 0, 0, 0, time.UTC)
	want := time.Date(2026, 6, 26, 8, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		now  time.Time
	}{
		{"app clock lags the fired slot (skew)", planTime.Add(-90 * time.Second)},
		{"app clock exactly on the fired slot", planTime},
		{"app clock just after the fired slot (normal)", planTime.Add(5 * time.Second)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := advancedNextRun(cron, tz, planTime, tc.now)
			if !ok {
				t.Fatal("expected ok=true for a valid cron/timezone")
			}
			if !got.Equal(want) {
				t.Fatalf("got %s, want %s", got.Format(time.RFC3339), want.Format(time.RFC3339))
			}
			if !got.After(planTime) {
				t.Fatalf("next_run_at %s must be strictly after the fired plan_time %s",
					got.Format(time.RFC3339), planTime.Format(time.RFC3339))
			}
		})
	}
}

// TestAdvancedNextRunInvalidInputsSignalFallback verifies the helper
// reports ok=false (so the handler falls back to the last_fired_at-only
// bump) when the cron or timezone cannot be parsed.
func TestAdvancedNextRunInvalidInputsSignalFallback(t *testing.T) {
	planTime := time.Date(2026, 6, 26, 7, 0, 0, 0, time.UTC)
	if _, ok := advancedNextRun("not a cron", "UTC", planTime, planTime); ok {
		t.Fatal("expected ok=false for an invalid cron expression")
	}
	if _, ok := advancedNextRun("0 * * * *", "Mars/Olympus", planTime, planTime); ok {
		t.Fatal("expected ok=false for an invalid timezone")
	}
}
