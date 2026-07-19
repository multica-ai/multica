package evals

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func countRunsForEval(t *testing.T, store *Store, workspaceID, evalID uuid.UUID) int {
	t.Helper()
	var n int
	if err := store.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM cerebro_eval_run WHERE workspace_id=$1 AND eval_id=$2`,
		workspaceID, evalID).Scan(&n); err != nil {
		t.Fatalf("count runs: %v", err)
	}
	return n
}

func enableEvalFeature(t *testing.T, workspaceID uuid.UUID) {
	t.Helper()
	if _, err := evalTestPool.Exec(context.Background(), `INSERT INTO cerebro_feature_flags (workspace_id,user_id,flag_key,enabled) VALUES ($1,'00000000-0000-0000-0000-000000000000','cerebro_evals',true) ON CONFLICT (workspace_id,user_id,flag_key) DO UPDATE SET enabled=true`, workspaceID); err != nil {
		t.Fatalf("enable cerebro_evals: %v", err)
	}
}

func TestScheduleSweeperRunsDueScheduleOnceThenAdvances(t *testing.T) {
	if evalTestPool == nil {
		t.Skip("no test DB")
	}
	f := seedEvalFixture(t)
	enableEvalFeature(t, f.workspaceID)
	store := NewStore(evalTestPool)
	ctx := context.Background()

	evalID := seedActiveEval(t, f, "scheduled-sweeper", 1)
	sched, err := store.UpsertSchedule(ctx, f.workspaceID, evalID, f.actorID, "@hourly", "", true)
	if err != nil {
		t.Fatalf("UpsertSchedule: %v", err)
	}

	exec := &fakeRunExecutor{execution: RunExecution{TargetVersion: "1.0.0", Status: "passed", Results: json.RawMessage(`{}`)}}
	sweeper := NewScheduleSweeper(store, exec)
	// Anchor "now" past the schedule's first fire so the row is due.
	fixedNow := sched.NextRunAt.Add(time.Minute)
	sweeper.now = func() time.Time { return fixedNow }

	if err := sweeper.SweepOnce(ctx); err != nil {
		t.Fatalf("SweepOnce: %v", err)
	}
	if exec.calls != 1 {
		t.Fatalf("expected the executor to run once, got %d", exec.calls)
	}
	if got := countRunsForEval(t, store, f.workspaceID, evalID); got != 1 {
		t.Fatalf("expected 1 recorded run, got %d", got)
	}

	// A second sweep at the same instant sees next_run_at advanced beyond now,
	// so it claims nothing and records no new run.
	if err := sweeper.SweepOnce(ctx); err != nil {
		t.Fatalf("second SweepOnce: %v", err)
	}
	if exec.calls != 1 {
		t.Fatalf("executor should not run again, got %d calls", exec.calls)
	}
	if got := countRunsForEval(t, store, f.workspaceID, evalID); got != 1 {
		t.Fatalf("expected still 1 recorded run after second sweep, got %d", got)
	}
}

func TestScheduleSweeperSkipsWorkspaceWithoutExecutor(t *testing.T) {
	if evalTestPool == nil {
		t.Skip("no test DB")
	}
	f := seedEvalFixture(t)
	enableEvalFeature(t, f.workspaceID)
	store := NewStore(evalTestPool)
	ctx := context.Background()

	evalID := seedActiveEval(t, f, "scheduled-eval-no-gw", 1)
	sched, err := store.UpsertSchedule(ctx, f.workspaceID, evalID, f.actorID, "@hourly", "", true)
	if err != nil {
		t.Fatalf("UpsertSchedule: %v", err)
	}

	sweeper := NewScheduleSweeper(store, nil)
	fixedNow := sched.NextRunAt.Add(time.Minute)
	sweeper.now = func() time.Time { return fixedNow }

	if err := sweeper.SweepOnce(ctx); err != nil {
		t.Fatalf("SweepOnce: %v", err)
	}
	if got := countRunsForEval(t, store, f.workspaceID, evalID); got != 0 {
		t.Fatalf("no run should be recorded without a gateway, got %d", got)
	}
	// The schedule was advanced so a second sweep does not re-claim it.
	after, err := store.ClaimDueSchedules(ctx, fixedNow, 10)
	if err != nil {
		t.Fatalf("ClaimDueSchedules: %v", err)
	}
	if scheduleInSet(after, sched.ID) {
		t.Fatal("schedule without a gateway should have been advanced, not left due")
	}
}

func TestScheduleSweeperSkipsWorkspaceWithoutEvalFeature(t *testing.T) {
	if evalTestPool == nil {
		t.Skip("no test DB")
	}
	f := seedEvalFixture(t)
	store := NewStore(evalTestPool)
	ctx := context.Background()
	evalID := seedActiveEval(t, f, "scheduled-feature-off", 1)
	sched, err := store.UpsertSchedule(ctx, f.workspaceID, evalID, f.actorID, "@hourly", "", true)
	if err != nil {
		t.Fatal(err)
	}
	due, err := store.ClaimDueSchedules(ctx, sched.NextRunAt.Add(time.Minute), 10)
	if err != nil {
		t.Fatal(err)
	}
	if scheduleInSet(due, sched.ID) {
		t.Fatal("feature-off workspace schedule was claimed")
	}
}
