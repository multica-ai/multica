package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/dispatch"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestAutopilotMutexGateSkipsOverlappingSlot is the ALL-206 regression for the
// scheduler mutex/lease gate: while an autopilot still has an in-flight run
// (autopilot_run.status IN ('pending','issue_created','running') — the exact set
// of the partial index idx_autopilot_run_status from migration 042), the next
// scheduled slot MUST NOT start a second concurrent run. It is recorded as a
// `skipped` run with a clear failure_reason instead.
func TestAutopilotMutexGateSkipsOverlappingSlot(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, taskSvc)

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id::text FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load fixture agent: %v", err)
	}

	ap := createAutopilotForMutexTest(t, ctx, queries, "ALL-206 mutex in-flight", agentID)
	trigger := createTriggerForMutexTest(t, ctx, queries, ap)

	// Slot 1 dispatches normally and leaves a run in flight (running + task).
	plannedAt1 := time.Now().UTC().Truncate(time.Second).Add(-2 * time.Minute)
	first, err := autopilotSvc.DispatchAutopilotForPlan(ctx, ap, trigger.ID, "schedule", nil, plannedAt1)
	if err != nil {
		t.Fatalf("slot 1 DispatchAutopilotForPlan: %v", err)
	}
	if first == nil {
		t.Fatalf("slot 1 returned nil run")
	}
	if first.Status != "running" || !first.TaskID.Valid {
		t.Fatalf("slot 1 run = status %q, task_id valid %v; want running with task", first.Status, first.TaskID.Valid)
	}

	// Slot 2 is the very next occurrence while slot 1 is still in flight: the
	// mutex gate must skip it instead of starting a concurrent run.
	plannedAt2 := plannedAt1.Add(30 * time.Second)
	second, err := autopilotSvc.DispatchAutopilotForPlan(ctx, ap, trigger.ID, "schedule", nil, plannedAt2)
	if err != nil {
		t.Fatalf("slot 2 DispatchAutopilotForPlan: %v", err)
	}
	if second == nil {
		t.Fatalf("slot 2 returned nil run")
	}
	if second.Status != "skipped" {
		t.Fatalf("slot 2 run status = %q, want skipped", second.Status)
	}
	if !second.FailureReason.Valid || !strings.Contains(second.FailureReason.String, "previous run still in flight") {
		t.Fatalf("slot 2 failure_reason = %q, want it to mention the in-flight predecessor", second.FailureReason.String)
	}
	if second.TaskID.Valid {
		t.Fatalf("skipped run must not be linked to a task, got task_id=%s", util.UUIDToString(second.TaskID))
	}

	// The manual "run now" surface reports the same typed admission code so the
	// HTTP handler can return it to the clicking member.
	manualRun, code, err := autopilotSvc.DispatchAutopilotManual(ctx, ap, trigger.ID, nil, parseUUID(testUserID))
	if err != nil {
		t.Fatalf("DispatchAutopilotManual while in flight: %v", err)
	}
	if code != dispatch.ReasonAlreadyActive {
		t.Fatalf("manual dispatch reason code = %q, want %q", code, dispatch.ReasonAlreadyActive)
	}
	if manualRun == nil || manualRun.Status != "skipped" {
		t.Fatalf("manual dispatch while in flight = %+v, want skipped run", manualRun)
	}

	// Never created a concurrent task: slot 1's task is the only one across slot
	// 1, slot 2 and the manual call.
	var taskCount int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue
		 WHERE autopilot_run_id IN (SELECT id FROM autopilot_run WHERE autopilot_id = $1)
	`, ap.ID).Scan(&taskCount); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if taskCount != 1 {
		t.Fatalf("expected exactly 1 task (slot 1), got %d — an overlapping slot must not dispatch", taskCount)
	}
}

// TestAutopilotMutexGateAllowsDispatchAfterTerminal is the counterpart ALL-206
// regression: once the previous run reaches a terminal state (here completed,
// the same flip the task listener applies), the next scheduled slot MUST
// dispatch normally. The mutex gate is per-autopilot, never global.
func TestAutopilotMutexGateAllowsDispatchAfterTerminal(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, taskSvc)

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id::text FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load fixture agent: %v", err)
	}

	ap := createAutopilotForMutexTest(t, ctx, queries, "ALL-206 mutex terminal", agentID)
	trigger := createTriggerForMutexTest(t, ctx, queries, ap)

	plannedAt1 := time.Now().UTC().Truncate(time.Second).Add(-2 * time.Minute)
	first, err := autopilotSvc.DispatchAutopilotForPlan(ctx, ap, trigger.ID, "schedule", nil, plannedAt1)
	if err != nil {
		t.Fatalf("slot 1 DispatchAutopilotForPlan: %v", err)
	}
	if first == nil || first.Status != "running" || !first.TaskID.Valid {
		t.Fatalf("slot 1 run = %+v, want running with task", first)
	}

	// Close out slot 1 the way the task listener (SyncRunFromTask) does: the run
	// flips to completed once its queued task finishes.
	if _, err := testPool.Exec(ctx,
		`UPDATE autopilot_run SET status = 'completed', completed_at = now() WHERE id = $1`, first.ID); err != nil {
		t.Fatalf("close slot 1 run: %v", err)
	}

	// The next occurrence must now dispatch normally — a fresh run and task, not
	// a mutex skip.
	plannedAt2 := plannedAt1.Add(30 * time.Second)
	second, err := autopilotSvc.DispatchAutopilotForPlan(ctx, ap, trigger.ID, "schedule", nil, plannedAt2)
	if err != nil {
		t.Fatalf("slot 2 DispatchAutopilotForPlan: %v", err)
	}
	if second == nil {
		t.Fatalf("slot 2 returned nil run")
	}
	if second.Status != "running" || !second.TaskID.Valid {
		t.Fatalf("slot 2 run = status %q, task_id valid %v; want normal running dispatch", second.Status, second.TaskID.Valid)
	}
	if second.FailureReason.Valid {
		t.Fatalf("slot 2 run must not carry a failure_reason, got %q", second.FailureReason.String)
	}

	// Exactly two tasks — one per dispatched slot: no loss, no duplication.
	var taskCount int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue
		 WHERE autopilot_run_id IN (SELECT id FROM autopilot_run WHERE autopilot_id = $1)
	`, ap.ID).Scan(&taskCount); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if taskCount != 2 {
		t.Fatalf("expected exactly 2 tasks (one per slot), got %d", taskCount)
	}
}

// createAutopilotForMutexTest seeds an active run_only autopilot assigned to the
// fixture agent. Deleting the autopilot cascades to its triggers and runs
// (migration 042), so a single DELETE is the whole cleanup.
func createAutopilotForMutexTest(t *testing.T, ctx context.Context, queries *db.Queries, title, agentID string) db.Autopilot {
	t.Helper()
	ap, err := queries.CreateAutopilot(ctx, db.CreateAutopilotParams{
		WorkspaceID:        parseUUID(testWorkspaceID),
		Title:              title,
		Description:        pgtype.Text{String: "ALL-206 mutex gate test", Valid: true},
		AssigneeType:       "agent",
		AssigneeID:         parseUUID(agentID),
		Status:             "active",
		ExecutionMode:      "run_only",
		IssueTitleTemplate: pgtype.Text{},
		CreatedByType:      "member",
		CreatedByID:        parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("CreateAutopilot: %v", err)
	}
	t.Cleanup(func() {
		if _, err := testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, ap.ID); err != nil {
			t.Logf("cleanup autopilot: %v", err)
		}
	})
	return ap
}

// createTriggerForMutexTest seeds an enabled schedule trigger on the autopilot.
func createTriggerForMutexTest(t *testing.T, ctx context.Context, queries *db.Queries, ap db.Autopilot) db.AutopilotTrigger {
	t.Helper()
	trigger, err := queries.CreateAutopilotTrigger(ctx, db.CreateAutopilotTriggerParams{
		AutopilotID:    ap.ID,
		Kind:           "schedule",
		Enabled:        true,
		CronExpression: pgtype.Text{String: "*/5 * * * *", Valid: true},
		Timezone:       pgtype.Text{String: "UTC", Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateAutopilotTrigger: %v", err)
	}
	return trigger
}
