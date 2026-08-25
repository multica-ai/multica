package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/autopilot"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestDispatch_SlowSchedule_LeaseCoversSlotInterval is the ALL-234 defect 1
// regression: an autopilot whose schedule fires slower than the base lease
// (30m) must have its effective in-flight lease extended to the slot
// interval, so a legitimate long-running run is never reclaimed as stale
// before the next slot could fire. Without the SlotInterval wiring, the
// 35-minute-old run below would be past the 30m lease and wrongly
// terminalized + admitted.
func TestDispatch_SlowSchedule_LeaseCoversSlotInterval(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, taskSvc)
	// Base lease stays at the default 30m; only the 2h slot interval can
	// extend it.
	autopilotSvc.SetLeaseTimeout(30 * time.Minute)

	agentID := loadFixtureAgentID(t, ctx)
	ap := createLeaseGateAutopilot(t, ctx, queries, "ALL-234 slow schedule", agentID, "run_only", "")
	trigger := createTriggerWithCron(t, ctx, queries, ap, "0 */2 * * *") // 2h cadence

	// Slot 1 starts a legitimate run with a linked agent task.
	plannedAt1 := time.Now().UTC().Truncate(time.Second).Add(-2 * time.Minute)
	first, err := autopilotSvc.DispatchAutopilotForPlan(ctx, ap, trigger.ID, "schedule", nil, plannedAt1)
	if err != nil {
		t.Fatalf("slot 1 dispatch: %v", err)
	}
	if first == nil || first.Status != "running" || !first.TaskID.Valid {
		t.Fatalf("slot 1 run = %+v, want running with task", first)
	}

	// Backdate the run past the 30m base lease but keep it well inside the
	// 2h slot interval. With the defect, the lease would be exactly 30m and
	// this run would be reclaimed.
	backdateRunCreatedAt(t, ctx, first.ID, 35*time.Minute)

	// Slot 2 must be skipped (already_active) — the run is still within the
	// extended lease, so the slot must NOT be admitted.
	plannedAt2 := plannedAt1.Add(30 * time.Second)
	second, err := autopilotSvc.DispatchAutopilotForPlan(ctx, ap, trigger.ID, "schedule", nil, plannedAt2)
	if err != nil {
		t.Fatalf("slot 2 dispatch: %v", err)
	}
	if second == nil || second.Status != "skipped" {
		t.Fatalf("slot 2 run status = %v, want skipped (lease must cover the slow schedule)", secondStatus(second))
	}

	// The in-flight run must NOT have been terminalized.
	var firstStatus, firstReason string
	if err := testPool.QueryRow(ctx,
		`SELECT status, COALESCE(reason_code, '') FROM autopilot_run WHERE id = $1`, first.ID,
	).Scan(&firstStatus, &firstReason); err != nil {
		t.Fatalf("read run after slot 2: %v", err)
	}
	if firstStatus != "running" {
		t.Fatalf("slow-schedule in-flight run status = %q (reason %q), want running; lease must cover the slot interval", firstStatus, firstReason)
	}

	// And its linked task must still be active — the run was not treated as
	// stale, so its work was not interrupted.
	var taskStatus string
	if err := testPool.QueryRow(ctx,
		`SELECT status FROM agent_task_queue WHERE id = $1`, first.TaskID,
	).Scan(&taskStatus); err != nil {
		t.Fatalf("read linked task: %v", err)
	}
	if taskStatus == "cancelled" {
		t.Fatalf("slow-schedule run task was cancelled; the run must not be treated as stale within its slot interval")
	}

	assertInFlightCount(t, ctx, ap.ID, 1)
}

// TestDispatch_StaleRun_CancelsLinkedTask is the ALL-234 defect 2 regression
// on the dispatch-gate path: when the lease expires and the stale run is
// reclaimed, the agent task linked via autopilot_run.task_id MUST be
// cancelled in lockstep, so the newly admitted slot never runs concurrently
// with the stale run's still-executing task. The assertion is the
// no-orphan-running-task invariant, counted across the autopilot's runs.
func TestDispatch_StaleRun_CancelsLinkedTask(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, taskSvc)

	const leaseTimeout = 6 * time.Minute // above autopilot.MinLeaseDuration so the floor clamp does not apply
	autopilotSvc.SetLeaseTimeout(leaseTimeout)

	agentID := loadFixtureAgentID(t, ctx)
	ap := createLeaseGateAutopilot(t, ctx, queries, "ALL-234 cancel linked task", agentID, "run_only", "")
	trigger := createTriggerForMutexTest(t, ctx, queries, ap)

	// Slot 1 starts a legitimate run with a linked agent task.
	plannedAt1 := time.Now().UTC().Truncate(time.Second).Add(-2 * time.Minute)
	first, err := autopilotSvc.DispatchAutopilotForPlan(ctx, ap, trigger.ID, "schedule", nil, plannedAt1)
	if err != nil {
		t.Fatalf("slot 1 dispatch: %v", err)
	}
	if first == nil || first.Status != "running" || !first.TaskID.Valid {
		t.Fatalf("slot 1 run = %+v, want running with task", first)
	}
	task1 := first.TaskID

	// Simulate real execution: the task is running, and the run has aged past
	// its lease without reaching a terminal state.
	if _, err := testPool.Exec(ctx,
		`UPDATE agent_task_queue SET status = 'running' WHERE id = $1`, task1); err != nil {
		t.Fatalf("mark task running: %v", err)
	}
	backdateRunCreatedAt(t, ctx, first.ID, leaseTimeout+5*time.Second)

	// Slot 2 fires past the lease: the stale run is reclaimed and the slot is
	// admitted — the concurrent-execution scenario defect 2 exists to close.
	plannedAt2 := plannedAt1.Add(30 * time.Second)
	second, err := autopilotSvc.DispatchAutopilotForPlan(ctx, ap, trigger.ID, "schedule", nil, plannedAt2)
	if err != nil {
		t.Fatalf("slot 2 dispatch: %v", err)
	}
	if second == nil || second.Status != "running" || !second.TaskID.Valid {
		t.Fatalf("slot 2 run = %+v, want running with task", second)
	}
	if second.ID == first.ID {
		t.Fatalf("slot 2 must create a fresh run, not reuse the stale one")
	}

	// The stale run is terminalized as failed with reason lease_expired.
	var staleStatus, staleReason string
	if err := testPool.QueryRow(ctx,
		`SELECT status, reason_code FROM autopilot_run WHERE id = $1`, first.ID,
	).Scan(&staleStatus, &staleReason); err != nil {
		t.Fatalf("read stale run: %v", err)
	}
	if staleStatus != "failed" || staleReason != "lease_expired" {
		t.Fatalf("stale run = status %q, reason %q; want failed/lease_expired", staleStatus, staleReason)
	}

	// The linked agent task must be cancelled — the core no-orphan invariant.
	var taskStatus string
	if err := testPool.QueryRow(ctx,
		`SELECT status FROM agent_task_queue WHERE id = $1`, task1,
	).Scan(&taskStatus); err != nil {
		t.Fatalf("read linked task: %v", err)
	}
	if taskStatus != "cancelled" {
		t.Fatalf("linked task status = %q, want cancelled; stale run must not leave an orphan running task", taskStatus)
	}

	// No orphan ACTIVE task remains across the autopilot's runs: only the new
	// run's task may be non-terminal.
	assertActiveTaskCount(t, ctx, ap.ID, 1)
	assertInFlightCount(t, ctx, ap.ID, 1)
}

// TestStaleRunSweeper_CancelsLinkedTask is the ALL-234 defect 2 regression
// on the sweeper path: a stale in-flight run reclaimed by the background
// sweeper must cancel its linked agent task exactly like the dispatch gate,
// so a run terminalized by the sweeper never leaves its task executing.
func TestStaleRunSweeper_CancelsLinkedTask(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, taskSvc)

	agentID := loadFixtureAgentID(t, ctx)
	ap := createLeaseGateAutopilot(t, ctx, queries, "ALL-234 sweeper cancel", agentID, "run_only", "")
	trigger := createTriggerForMutexTest(t, ctx, queries, ap)

	// Slot 1 starts a legitimate run with a linked agent task.
	plannedAt1 := time.Now().UTC().Truncate(time.Second).Add(-2 * time.Minute)
	first, err := autopilotSvc.DispatchAutopilotForPlan(ctx, ap, trigger.ID, "schedule", nil, plannedAt1)
	if err != nil {
		t.Fatalf("slot 1 dispatch: %v", err)
	}
	if first == nil || first.Status != "running" || !first.TaskID.Valid {
		t.Fatalf("slot 1 run = %+v, want running with task", first)
	}
	task1 := first.TaskID

	// Simulate real execution: task running, run aged beyond the sweeper's
	// hard timeout (the run the dispatch lease gate would never see, because
	// no new slot ever arrives).
	const hardTimeout = time.Hour
	if _, err := testPool.Exec(ctx,
		`UPDATE agent_task_queue SET status = 'running' WHERE id = $1`, task1); err != nil {
		t.Fatalf("mark task running: %v", err)
	}
	backdateRunCreatedAt(t, ctx, first.ID, hardTimeout+time.Minute)

	sweeper := autopilot.NewStaleRunSweeper(testPool, &autopilot.SweeperConfig{
		Interval:    5 * time.Minute,
		HardTimeout: hardTimeout,
		Enabled:     true,
		Logger:      testLogger(t),
		CancelTask: func(ctx context.Context, taskID pgtype.UUID, reason string) error {
			_, err := taskSvc.CancelTaskWithReason(ctx, taskID, reason, "lease_expired")
			return err
		},
	})
	sweeper.SweepOnce(ctx)

	// The swept run is terminalized as failed with reason lease_expired.
	var staleStatus, staleReason string
	if err := testPool.QueryRow(ctx,
		`SELECT status, reason_code FROM autopilot_run WHERE id = $1`, first.ID,
	).Scan(&staleStatus, &staleReason); err != nil {
		t.Fatalf("read swept run: %v", err)
	}
	if staleStatus != "failed" || staleReason != "lease_expired" {
		t.Fatalf("swept run = status %q, reason %q; want failed/lease_expired", staleStatus, staleReason)
	}

	// The linked agent task must be cancelled too — no orphan running task.
	var taskStatus string
	if err := testPool.QueryRow(ctx,
		`SELECT status FROM agent_task_queue WHERE id = $1`, task1,
	).Scan(&taskStatus); err != nil {
		t.Fatalf("read linked task: %v", err)
	}
	if taskStatus != "cancelled" {
		t.Fatalf("linked task status = %q, want cancelled; swept run must not leave an orphan running task", taskStatus)
	}

	assertActiveTaskCount(t, ctx, ap.ID, 0)
}

// --- helpers ---------------------------------------------------------------

// createTriggerWithCron seeds a schedule trigger with an explicit cron
// expression for lease-gate tests.
func createTriggerWithCron(t *testing.T, ctx context.Context, queries *db.Queries, ap db.Autopilot, cronExpr string) db.AutopilotTrigger {
	t.Helper()
	trigger, err := queries.CreateAutopilotTrigger(ctx, db.CreateAutopilotTriggerParams{
		AutopilotID:    ap.ID,
		Kind:           "schedule",
		Enabled:        true,
		CronExpression: pgtype.Text{String: cronExpr, Valid: true},
		Timezone:       pgtype.Text{String: "UTC", Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateAutopilotTrigger: %v", err)
	}
	return trigger
}

// backdateRunCreatedAt rewinds an existing autopilot_run's created_at so the
// run appears to be `age` old.
func backdateRunCreatedAt(t *testing.T, ctx context.Context, runID pgtype.UUID, age time.Duration) {
	t.Helper()
	if _, err := testPool.Exec(ctx, `
		UPDATE autopilot_run SET created_at = now() - $2::interval WHERE id = $1
	`, runID, fmt.Sprintf("%d seconds", int(age.Seconds()))); err != nil {
		t.Fatalf("backdate run created_at: %v", err)
	}
}

// assertActiveTaskCount asserts how many non-terminal agent tasks remain
// across an autopilot's runs. The no-orphan invariant: after a stale-run
// reclaim, exactly the newly admitted run's task may be active.
func assertActiveTaskCount(t *testing.T, ctx context.Context, autopilotID pgtype.UUID, want int) {
	t.Helper()
	var count int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue t
		JOIN autopilot_run r ON r.id = t.autopilot_run_id
		WHERE r.autopilot_id = $1
		  AND t.status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred')
	`, autopilotID).Scan(&count); err != nil {
		t.Fatalf("count active tasks: %v", err)
	}
	if count != want {
		t.Fatalf("active task count = %d, want %d", count, want)
	}
}
