package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

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

// TestSyncRunFromTask_DoesNotOverwriteTerminalizedRun is the ALL-234
// defect 4 regression: a run terminalized as failed (reason_code=lease_expired)
// by the lease gate or the sweeper may still have a SURVIVING agent task that
// finishes later — terminalizeStaleRun deliberately does not cancel it (see
// its comment; the runtime sweeper reclaims it). The surviving task's late
// terminal event must NOT rewrite the same run back to completed/failed,
// which would erase the lease audit trail and make the failure monitor count
// a reclaimed run as a success.
func TestSyncRunFromTask_DoesNotOverwriteTerminalizedRun(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, taskSvc)

	const leaseTimeout = 6 * time.Minute // above autopilot.MinLeaseDuration so the floor clamp does not apply
	autopilotSvc.SetLeaseTimeout(leaseTimeout)

	agentID := loadFixtureAgentID(t, ctx)
	ap := createLeaseGateAutopilot(t, ctx, queries, "ALL-234 terminal guard", agentID, "run_only", "")
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

	// The run's lease expires without the run reaching a terminal state. The
	// next slot reclaims it exactly as the dispatch gate does: terminalize
	// the stale run (failed + lease_expired) and admit the new slot. The
	// surviving task from slot 1 is NOT cancelled by design.
	backdateRunCreatedAt(t, ctx, first.ID, leaseTimeout+5*time.Second)
	plannedAt2 := plannedAt1.Add(30 * time.Second)
	if _, err := autopilotSvc.DispatchAutopilotForPlan(ctx, ap, trigger.ID, "schedule", nil, plannedAt2); err != nil {
		t.Fatalf("slot 2 dispatch (lease-reclaimed): %v", err)
	}
	var staleStatus, staleReason string
	if err := testPool.QueryRow(ctx,
		`SELECT status, reason_code FROM autopilot_run WHERE id = $1`, first.ID,
	).Scan(&staleStatus, &staleReason); err != nil {
		t.Fatalf("read reclaimed run: %v", err)
	}
	if staleStatus != "failed" || staleReason != "lease_expired" {
		t.Fatalf("reclaimed run = status %q, reason %q; want failed/lease_expired", staleStatus, staleReason)
	}

	// The surviving task finishes LATER. Its terminal event must be ignored
	// by the terminal-state guard — the run must stay failed/lease_expired.
	task, err := queries.GetAgentTask(ctx, task1)
	if err != nil {
		t.Fatalf("load surviving task: %v", err)
	}
	task.Status = "completed"
	autopilotSvc.SyncRunFromTask(ctx, task)

	var afterStatus, afterReason string
	if err := testPool.QueryRow(ctx,
		`SELECT status, COALESCE(reason_code, '') FROM autopilot_run WHERE id = $1`, first.ID,
	).Scan(&afterStatus, &afterReason); err != nil {
		t.Fatalf("read reclaimed run after surviving task event: %v", err)
	}
	if afterStatus != "failed" || afterReason != "lease_expired" {
		t.Fatalf("reclaimed run after surviving task completion = status %q, reason %q; want failed/lease_expired (terminal guard must hold)", afterStatus, afterReason)
	}

	// The run that was reclaimed must not have been resurrected by the task
	// event: it stays terminal, and the new slot's run is the only in-flight one.
	assertInFlightCount(t, ctx, ap.ID, 1)
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
