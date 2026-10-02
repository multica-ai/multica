package scheduler

import (
	"context"
	"testing"
	"time"
)

// planAutopilotTrigger runs the autopilot PlansForScope hook for a single
// trigger whose last stored plan_time is lastPlan.
func planAutopilotTrigger(t *testing.T, cron string, lastPlan, now time.Time) []time.Time {
	t.Helper()
	cache := newAutopilotScheduleCache()
	cache.replace(map[string]autopilotTriggerConfig{
		"trigger": {TriggerID: "trigger", CronExpression: cron, Timezone: "UTC"},
	})
	plans, err := autopilotPlansForScope(cache)(
		context.Background(),
		Scope{Kind: ScopeKindAutopilotTrigger, ID: "trigger"},
		now,
		LatestPlanInfo{Found: true, PlanTime: lastPlan, Status: "SUCCESS"},
	)
	if err != nil {
		t.Fatalf("plans hook: %v", err)
	}
	return plans
}

// TestAutopilotPlansForScopeDenseCronAfterLongPause is the regression guard
// for a trigger that resumes after a pause longer than 1024 of its own
// occurrences. NextOccurrencesUTC keeps the FIRST 1024 activations of
// (lastPlan, now], so for "* * * * *" the last kept one sits hours in the
// past, fails the lateness guard, and the hook returns nothing. Nothing is
// written to sys_cron_executions, lastPlan never moves, and the trigger
// stays silent on every later tick while next_run_at keeps looking valid.
func TestAutopilotPlansForScopeDenseCronAfterLongPause(t *testing.T) {
	now := time.Date(2026, 9, 4, 16, 5, 10, 0, time.UTC)

	cases := []struct {
		name     string
		cron     string
		lastPlan time.Time
		want     time.Time
	}{
		{
			name:     "every minute, 18h pause (1080 occurrences)",
			cron:     "* * * * *",
			lastPlan: time.Date(2026, 9, 3, 22, 5, 0, 0, time.UTC),
			want:     time.Date(2026, 9, 4, 16, 5, 0, 0, time.UTC),
		},
		{
			name:     "every minute, 3-day pause",
			cron:     "* * * * *",
			lastPlan: time.Date(2026, 9, 1, 16, 5, 0, 0, time.UTC),
			want:     time.Date(2026, 9, 4, 16, 5, 0, 0, time.UTC),
		},
		{
			name:     "every minute, short pause",
			cron:     "* * * * *",
			lastPlan: time.Date(2026, 9, 4, 15, 55, 0, 0, time.UTC),
			want:     time.Date(2026, 9, 4, 16, 5, 0, 0, time.UTC),
		},
		{
			name:     "every two minutes, 18h pause (540 occurrences)",
			cron:     "*/2 * * * *",
			lastPlan: time.Date(2026, 9, 3, 22, 5, 0, 0, time.UTC),
			want:     time.Date(2026, 9, 4, 16, 4, 0, 0, time.UTC),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plans := planAutopilotTrigger(t, tc.cron, tc.lastPlan, now)
			if len(plans) != 1 || !plans[0].Equal(tc.want) {
				t.Fatalf("plans = %v, want [%s]", plans, tc.want.Format(time.RFC3339))
			}
		})
	}
}

// TestAutopilotPlansForScopeKeepsLatenessGuard pins the stale catch-up rule
// the fix must not loosen: the latest due occurrence fires only while it is
// at most maxAutopilotScheduleLateness old.
func TestAutopilotPlansForScopeKeepsLatenessGuard(t *testing.T) {
	lastPlan := time.Date(2026, 9, 4, 15, 0, 0, 0, time.UTC)
	due := time.Date(2026, 9, 4, 16, 0, 0, 0, time.UTC)

	if plans := planAutopilotTrigger(t, "0 * * * *", lastPlan, due.Add(maxAutopilotScheduleLateness)); len(plans) != 1 || !plans[0].Equal(due) {
		t.Fatalf("exactly at the lateness bound: plans = %v, want [%s]", plans, due.Format(time.RFC3339))
	}
	if plans := planAutopilotTrigger(t, "0 * * * *", lastPlan, due.Add(maxAutopilotScheduleLateness+time.Second)); len(plans) != 0 {
		t.Fatalf("past the lateness bound: plans = %v, want none", plans)
	}
}
