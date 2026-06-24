package scheduler

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestJobSpecValidatePlansForScopeRelaxesCadence covers the relaxed
// JobSpec.validate() rule: when PlansForScope is set, Cadence is
// optional because the hook owns plan_time selection (used by the
// Autopilot schedule job, where each trigger's cron expression is
// arbitrary and does not fit a single Cadence grid).
func TestJobSpecValidatePlansForScopeRelaxesCadence(t *testing.T) {
	base := JobSpec{
		Name:              "hook_validate",
		RunTimeout:        time.Minute,
		StaleTimeout:      2 * time.Minute,
		HeartbeatInterval: 30 * time.Second,
		MaxAttempts:       1,
		Scopes:            StaticScopes(ScopeGlobal),
		Handler: func(ctx context.Context, in HandlerInput) (HandlerResult, error) {
			return HandlerResult{}, nil
		},
	}

	t.Run("cadence still required without hook", func(t *testing.T) {
		j := base
		err := j.validate()
		if err == nil {
			t.Fatalf("expected validate error when both Cadence and PlansForScope are unset")
		}
		if !strings.Contains(err.Error(), "cadence must be > 0") {
			t.Fatalf("expected cadence error, got %v", err)
		}
	})

	t.Run("cadence optional when hook is set", func(t *testing.T) {
		j := base
		j.PlansForScope = func(ctx context.Context, scope Scope, now time.Time, latest LatestPlanInfo) ([]time.Time, error) {
			return nil, nil
		}
		if err := j.validate(); err != nil {
			t.Fatalf("expected validate to pass with PlansForScope and no Cadence; got %v", err)
		}
	})

	t.Run("every_plan max_plans_per_tick still required without hook", func(t *testing.T) {
		j := base
		j.Cadence = 5 * time.Minute
		j.CatchUpMode = CatchUpEveryPlan
		err := j.validate()
		if err == nil || !strings.Contains(err.Error(), "max_plans_per_tick") {
			t.Fatalf("expected max_plans_per_tick error for every_plan without hook, got %v", err)
		}
	})

	t.Run("every_plan max_plans_per_tick optional with hook", func(t *testing.T) {
		j := base
		j.CatchUpMode = CatchUpEveryPlan // legal but ignored when hook is set
		j.PlansForScope = func(ctx context.Context, scope Scope, now time.Time, latest LatestPlanInfo) ([]time.Time, error) {
			return nil, nil
		}
		if err := j.validate(); err != nil {
			t.Fatalf("hook-driven jobs may leave max_plans_per_tick=0; got %v", err)
		}
	})

	t.Run("other invariants still fire", func(t *testing.T) {
		// RunTimeout > 0 must still be enforced even when the hook is set.
		j := base
		j.PlansForScope = func(ctx context.Context, scope Scope, now time.Time, latest LatestPlanInfo) ([]time.Time, error) {
			return nil, nil
		}
		j.RunTimeout = 0
		err := j.validate()
		if err == nil || !strings.Contains(err.Error(), "run_timeout") {
			t.Fatalf("expected run_timeout invariant to still fire with hook, got %v", err)
		}
	})
}

// TestManagerPlansForScopeHookDrivesPlans verifies end-to-end that a
// JobSpec.PlansForScope hook fully replaces the Cadence planner:
//
//   - The hook is invoked with a fresh LatestPlanInfo{Found:false} on
//     first tick, then with the prior plan on the second tick.
//   - Every plan_time the hook returns is claimed and finalised
//     SUCCESS by the manager — proof the hook is wired through the
//     same tryClaim/runClaimed lease path as Cadence-driven jobs.
//   - MaxPlansPerTick caps an over-eager hook without erroring.
//   - Cadence=0 is legal when PlansForScope is set.
//
// Lives in the integration tier (requires DATABASE_URL) because it
// exercises the manager's actual SQL primitives, not a stub.
func TestManagerPlansForScopeHookDrivesPlans(t *testing.T) {
	pool := integrationPool(t)
	job := newTestJobSpec(uniqueJobName(t, "hook_planner"))
	t.Cleanup(func() { cleanupExecutions(t, pool, job.Name) })

	ctx := context.Background()
	now, err := dbNow(ctx, pool)
	if err != nil {
		t.Fatalf("dbNow: %v", err)
	}

	// Hook-driven job: arbitrary plan_times, no Cadence.
	job.Cadence = 0
	job.CatchUpMode = CatchUpLatestOnly // ignored when hook is set, but exercise the path
	job.MaxPlansPerTick = 2             // safety cap; we'll return 3 from the hook below

	// Plan_times the hook will offer in tick 1. Deliberately NOT on a
	// uniform Cadence grid so the Cadence planner would never produce
	// them — proving the hook bypasses FloorPlan entirely.
	t0 := now.Add(-3 * time.Minute).Truncate(time.Second)
	t1 := now.Add(-2*time.Minute - 17*time.Second).Truncate(time.Second)
	t2 := now.Add(-1 * time.Minute).Truncate(time.Second)
	t3 := now.Add(-15 * time.Second).Truncate(time.Second) // beyond MaxPlansPerTick; should be dropped

	var hookCalls atomic.Int32
	var lastSeenFound atomic.Bool
	var lastSeenPlan atomic.Pointer[time.Time]

	job.PlansForScope = func(ctx context.Context, scope Scope, ts time.Time, latest LatestPlanInfo) ([]time.Time, error) {
		hookCalls.Add(1)
		lastSeenFound.Store(latest.Found)
		if latest.Found {
			pt := latest.PlanTime
			lastSeenPlan.Store(&pt)
		} else {
			lastSeenPlan.Store(nil)
		}
		// Realistic hook contract: return only plan_times strictly
		// after the most recent stored one. This is exactly what the
		// Autopilot scheduler will do — compute cron occurrences in
		// (latest.PlanTime, now]. We always *could* return everything
		// and rely on tryClaim's conflict-no-op for idempotency, but
		// returning fewer plans is cheaper and exercises the
		// LatestPlanInfo plumbing.
		all := []time.Time{t0, t1, t2, t3}
		var out []time.Time
		for _, p := range all {
			if !latest.Found || p.After(latest.PlanTime) {
				out = append(out, p)
			}
		}
		return out, nil
	}

	mgr := NewManager(pool, Options{RunnerID: "hook-planner-runner"})
	if err := mgr.Register(*job); err != nil {
		t.Fatalf("register hook-driven job: %v", err)
	}

	// Tick 1: no prior plan, hook returns all 4 plans, MaxPlansPerTick
	// truncates to {t0, t1}.
	if err := mgr.RunOnce(ctx); err != nil {
		t.Fatalf("runOnce tick 1: %v", err)
	}
	if hookCalls.Load() == 0 {
		t.Fatalf("hook was never called on tick 1")
	}
	if lastSeenFound.Load() {
		t.Fatalf("first tick should see LatestPlanInfo{Found:false}")
	}

	rows := dumpJobRows(t, pool, job.Name)
	if len(rows) != 2 {
		t.Fatalf("expected 2 plans claimed (MaxPlansPerTick=2), got %d: %+v", len(rows), rows)
	}
	for _, r := range rows {
		if r.Status != "SUCCESS" {
			t.Fatalf("hook-claimed plan should finish SUCCESS, got %q at %s", r.Status, r.PlanTime)
		}
	}
	wantPlans := []time.Time{t0.UTC(), t1.UTC()}
	for i, r := range rows {
		if !r.PlanTime.Equal(wantPlans[i]) {
			t.Fatalf("plan[%d] = %s; want %s (MaxPlansPerTick should preserve hook order)",
				i, r.PlanTime.Format(time.RFC3339Nano), wantPlans[i].Format(time.RFC3339Nano))
		}
	}

	// Tick 2: latest stored plan is t1. The hook now returns only the
	// plans strictly after t1 — i.e. {t2, t3}. MaxPlansPerTick=2 lets
	// both through this tick. The hook must see Found=true with the
	// latest plan_time (t1, picked by latestPlan ORDER BY plan_time
	// DESC).
	hookCalls.Store(0)
	if err := mgr.RunOnce(ctx); err != nil {
		t.Fatalf("runOnce tick 2: %v", err)
	}
	if hookCalls.Load() == 0 {
		t.Fatalf("hook was never called on tick 2")
	}
	if !lastSeenFound.Load() {
		t.Fatalf("second tick should see LatestPlanInfo{Found:true}")
	}
	if got := lastSeenPlan.Load(); got == nil || !got.Equal(t1.UTC()) {
		var gotStr string
		if got != nil {
			gotStr = got.Format(time.RFC3339Nano)
		}
		t.Fatalf("second tick's LatestPlanInfo.PlanTime should be the most recent stored plan; want %s got %s",
			t1.UTC().Format(time.RFC3339Nano), gotStr)
	}

	rows = dumpJobRows(t, pool, job.Name)
	if len(rows) != 4 {
		t.Fatalf("expected 4 plans after tick 2 (tick 1 left {t0,t1}, tick 2 adds {t2,t3}), got %d: %+v",
			len(rows), rows)
	}
	for _, r := range rows {
		if r.Status != "SUCCESS" {
			t.Fatalf("all plans should be SUCCESS after tick 2, got %q at %s", r.Status, r.PlanTime)
		}
	}
}

// TestManagerPlansForScopeHookEmptyIsNoOp covers the "nothing due"
// path: an empty plan slice must not error, must not block subsequent
// scopes, and must not produce any sys_cron_executions row.
func TestManagerPlansForScopeHookEmptyIsNoOp(t *testing.T) {
	pool := integrationPool(t)
	job := newTestJobSpec(uniqueJobName(t, "hook_empty"))
	t.Cleanup(func() { cleanupExecutions(t, pool, job.Name) })

	job.Cadence = 0
	var hookCalls atomic.Int32
	job.PlansForScope = func(ctx context.Context, scope Scope, now time.Time, latest LatestPlanInfo) ([]time.Time, error) {
		hookCalls.Add(1)
		return nil, nil
	}

	mgr := NewManager(pool, Options{RunnerID: "hook-empty-runner"})
	if err := mgr.Register(*job); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := mgr.RunOnce(context.Background()); err != nil {
		t.Fatalf("runOnce: %v", err)
	}
	if hookCalls.Load() == 0 {
		t.Fatalf("hook was never called for the registered job")
	}
	rows := dumpJobRows(t, pool, job.Name)
	if len(rows) != 0 {
		t.Fatalf("empty hook output must not create rows; got %d", len(rows))
	}
}
