package scheduler

import (
	"context"
	"testing"
	"time"
)

func TestAutopilotScheduleSingleFlightWhileRunning(t *testing.T) {
	cache := newAutopilotScheduleCache()
	created := time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC)
	scope := Scope{Kind: ScopeKindAutopilotTrigger, ID: "trigger-single-flight"}
	cache.replace(map[string]autopilotTriggerConfig{
		scope.ID: {
			TriggerID:      scope.ID,
			CronExpression: "*/5 * * * *",
			Timezone:       "UTC",
			CreatedAt:      created,
		},
	})
	plans := autopilotPlansForScope(cache)
	now := created.Add(16 * time.Minute)

	got, err := plans(context.Background(), scope, now, LatestPlanInfo{
		Found:    true,
		PlanTime: created.Add(15 * time.Minute),
		Status:   "RUNNING",
	})
	if err != nil {
		t.Fatalf("plan while running: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("a running trigger must not overlap a newer cron occurrence, got %v", got)
	}

	// A failed occurrence with retry budget is serial too. Before its
	// backoff expires, the scheduler must not skip ahead to a newer
	// occurrence and strand the retry.
	failed := LatestPlanInfo{
		Found:       true,
		PlanTime:    created.Add(10 * time.Minute),
		Status:      "FAILED",
		Attempt:     1,
		MaxAttempts: 3,
		NextRetryAt: now.Add(time.Minute),
	}
	got, err = plans(context.Background(), scope, now, failed)
	if err != nil {
		t.Fatalf("plan during retry backoff: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("a failed trigger with retry budget must wait for backoff, got %v", got)
	}

	failed.NextRetryAt = now.Add(-time.Second)
	got, err = plans(context.Background(), scope, now, failed)
	if err != nil {
		t.Fatalf("plan when retry is due: %v", err)
	}
	if len(got) != 1 || !got[0].Equal(failed.PlanTime) {
		t.Fatalf("retry must reuse the failed plan_time %s, got %v", failed.PlanTime, got)
	}

	// Terminal rows release the single-flight gate. The next occurrence
	// remains due and is returned normally.
	got, err = plans(context.Background(), scope, now, LatestPlanInfo{
		Found:    true,
		PlanTime: created.Add(10 * time.Minute),
		Status:   "SUCCESS",
	})
	if err != nil {
		t.Fatalf("plan after terminal run: %v", err)
	}
	if len(got) != 1 || !got[0].Equal(created.Add(15*time.Minute)) {
		t.Fatalf("terminal trigger should return the next occurrence, got %v", got)
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
