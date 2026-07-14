package main

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type scheduleTestWakeup struct {
	notifications chan string
}

func (w *scheduleTestWakeup) NotifyTaskAvailable(_, taskID string) {
	w.notifications <- taskID
}

// TestDispatchAutopilotForPlanIsIdempotent locks in the
// occurrence-level idempotency contract (MUL-3551):
//
//   - A second DispatchAutopilotForPlan with the same (trigger_id,
//     planned_at) MUST return the SAME run row that the first call
//     created. No second autopilot_run, no second issue / task, no
//     second failure recorded.
//
// This is the dispatch-layer half of the two-defence design. The
// primary defence lives in sys_cron_executions
// (uq_sys_cron_execution). This one catches the stale-steal case
// where a runner crashes between "create run" and "write SUCCESS in
// sys_cron_executions": the next runner re-enters the dispatch and
// must reuse the in-flight run instead of duplicating it.
func TestDispatchAutopilotForPlanIsIdempotent(t *testing.T) {
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

	ap, err := queries.CreateAutopilot(ctx, db.CreateAutopilotParams{
		WorkspaceID:        parseUUID(testWorkspaceID),
		Title:              "Dispatch for plan idempotency",
		Description:        pgtype.Text{String: "Dispatch for plan test", Valid: true},
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
		if _, err := testPool.Exec(context.Background(),
			`DELETE FROM autopilot WHERE id = $1`, ap.ID); err != nil {
			t.Logf("cleanup autopilot: %v", err)
		}
	})

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
	if trigger.OverlapPolicy != "allow" {
		t.Fatalf("schedule trigger must default overlap_policy=allow, got %q", trigger.OverlapPolicy)
	}

	// Use a fixed planned_at so the partial unique index has something
	// concrete to enforce against. Truncate to seconds — the column is
	// TIMESTAMPTZ and pgx round-trips sub-microsecond, but we want the
	// comparison to be byte-stable across the two calls.
	plannedAt := time.Now().UTC().Truncate(time.Second).Add(-30 * time.Second)

	first, err := autopilotSvc.DispatchAutopilotForPlan(
		ctx, ap, trigger.ID, "schedule", nil, plannedAt,
	)
	if err != nil {
		t.Fatalf("first DispatchAutopilotForPlan: %v", err)
	}
	if first == nil {
		t.Fatalf("first call returned nil run")
	}
	if !first.PlannedAt.Valid {
		t.Fatalf("first run should have planned_at set")
	}
	if !first.PlannedAt.Time.Equal(plannedAt) {
		t.Fatalf("first run planned_at mismatch: got %s, want %s",
			first.PlannedAt.Time.Format(time.RFC3339Nano),
			plannedAt.Format(time.RFC3339Nano))
	}

	// Second call with the SAME (trigger, planned_at) must reuse the
	// first run, not create a new one.
	second, err := autopilotSvc.DispatchAutopilotForPlan(
		ctx, ap, trigger.ID, "schedule", nil, plannedAt,
	)
	if err != nil {
		t.Fatalf("second DispatchAutopilotForPlan: %v", err)
	}
	if second == nil {
		t.Fatalf("second call returned nil run")
	}
	if second.ID != first.ID {
		t.Fatalf("second call must reuse first run: first.ID=%s second.ID=%s",
			util.UUIDToString(first.ID), util.UUIDToString(second.ID))
	}

	// Belt-and-suspenders: the partial unique index plus the lookup
	// in DispatchAutopilotForPlan together guarantee exactly one row.
	var rowCount int
	if err := testPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM autopilot_run WHERE autopilot_id = $1`, ap.ID,
	).Scan(&rowCount); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("expected exactly 1 autopilot_run for the (trigger, planned_at) pair, got %d", rowCount)
	}

	// A different planned_at for the same trigger MUST be allowed —
	// it represents the next scheduled occurrence, not a duplicate.
	plannedAt2 := plannedAt.Add(5 * time.Minute)
	third, err := autopilotSvc.DispatchAutopilotForPlan(
		ctx, ap, trigger.ID, "schedule", nil, plannedAt2,
	)
	if err != nil {
		t.Fatalf("third DispatchAutopilotForPlan with new planned_at: %v", err)
	}
	if third.ID == first.ID {
		t.Fatalf("different planned_at must produce a different run, got reuse")
	}

	if err := testPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM autopilot_run WHERE autopilot_id = $1`, ap.ID,
	).Scan(&rowCount); err != nil {
		t.Fatalf("count rows after 3rd call: %v", err)
	}
	if rowCount != 2 {
		t.Fatalf("expected 2 autopilot_run rows after distinct planned_ats, got %d", rowCount)
	}
}

func TestDispatchScheduledAutopilotForPlanCoalescesActiveRun(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	wakeups := &scheduleTestWakeup{notifications: make(chan string, 4)}
	taskSvc.Wakeup = wakeups
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, taskSvc)

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id::text FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load fixture agent: %v", err)
	}

	ap, err := queries.CreateAutopilot(ctx, db.CreateAutopilotParams{
		WorkspaceID:        parseUUID(testWorkspaceID),
		Title:              "Coalesce active scheduled run",
		Description:        pgtype.Text{String: "single-flight schedule test", Valid: true},
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
		if _, err := testPool.Exec(context.Background(),
			`DELETE FROM autopilot WHERE id = $1`, ap.ID); err != nil {
			t.Logf("cleanup autopilot: %v", err)
		}
	})

	trigger, err := queries.CreateAutopilotTrigger(ctx, db.CreateAutopilotTriggerParams{
		AutopilotID:    ap.ID,
		Kind:           "schedule",
		Enabled:        true,
		CronExpression: pgtype.Text{String: "*/5 * * * *", Valid: true},
		Timezone:       pgtype.Text{String: "UTC", Valid: true},
		OverlapPolicy:  pgtype.Text{String: "coalesce", Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateAutopilotTrigger: %v", err)
	}

	firstPlan := time.Now().UTC().Truncate(time.Second).Add(-30 * time.Second)
	first, err := autopilotSvc.DispatchScheduledAutopilotForPlan(
		ctx, ap, trigger, "schedule", nil, firstPlan,
	)
	if err != nil {
		t.Fatalf("first scheduled dispatch: %v", err)
	}
	if first.Coalesced {
		t.Fatal("first scheduled dispatch must create work")
	}
	if first.Run == nil || !first.Run.TaskID.Valid {
		t.Fatal("first scheduled dispatch must create a linked task")
	}
	select {
	case <-wakeups.notifications:
	default:
		t.Fatal("first scheduled dispatch must wake the task runtime after commit")
	}

	// The caller can carry a stale pre-update snapshot. Live policy must be
	// reloaded under the admission lock so allow -> coalesce cannot leak one
	// extra run while the earlier task is active.
	staleAllowTrigger := trigger
	staleAllowTrigger.OverlapPolicy = "allow"
	second, err := autopilotSvc.DispatchScheduledAutopilotForPlan(
		ctx, ap, staleAllowTrigger, "schedule", nil, firstPlan.Add(5*time.Minute),
	)
	if err != nil {
		t.Fatalf("second scheduled dispatch: %v", err)
	}
	if !second.Coalesced {
		t.Fatal("second scheduled dispatch must coalesce while the first task is active")
	}
	if second.Run == nil || second.Run.ID != first.Run.ID {
		t.Fatal("coalesced result must point at the active run")
	}

	var runCount, taskCount int
	if err := testPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM autopilot_run WHERE autopilot_id = $1`, ap.ID,
	).Scan(&runCount); err != nil {
		t.Fatalf("count runs: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		SELECT COUNT(*)
		  FROM agent_task_queue task
		  JOIN autopilot_run run ON run.id = task.autopilot_run_id
		 WHERE run.autopilot_id = $1
	`, ap.ID).Scan(&taskCount); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if runCount != 1 || taskCount != 1 {
		t.Fatalf("active coalescing must leave exactly one run/task, got runs=%d tasks=%d", runCount, taskCount)
	}
	for i, status := range []string{"dispatched", "running", "waiting_local_directory"} {
		if _, err := testPool.Exec(ctx, `
			UPDATE agent_task_queue SET status = $2 WHERE id = $1
		`, first.Run.TaskID, status); err != nil {
			t.Fatalf("set first task status=%s: %v", status, err)
		}
		out, err := autopilotSvc.DispatchScheduledAutopilotForPlan(
			ctx, ap, trigger, "schedule", nil, firstPlan.Add(time.Duration(i+2)*5*time.Minute),
		)
		if err != nil {
			t.Fatalf("dispatch while task status=%s: %v", status, err)
		}
		if !out.Coalesced || out.Run == nil || out.Run.ID != first.Run.ID {
			t.Fatalf("task status=%s must coalesce onto the active run", status)
		}
	}

	// A terminal task releases the single-flight gate. Race two later plan
	// times through separate goroutines to model two scheduler replicas: one
	// may create the next run, while the other must observe it and coalesce.
	if _, err := testPool.Exec(ctx, `
		UPDATE agent_task_queue
		   SET status = 'completed', completed_at = now()
		 WHERE id = $1
	`, first.Run.TaskID); err != nil {
		t.Fatalf("complete first task: %v", err)
	}
	completedTask, err := queries.GetAgentTask(ctx, first.Run.TaskID)
	if err != nil {
		t.Fatalf("reload completed task: %v", err)
	}
	autopilotSvc.SyncRunFromTask(ctx, completedTask)

	retried, err := autopilotSvc.DispatchScheduledAutopilotForPlan(
		ctx, ap, trigger, "schedule", nil, firstPlan,
	)
	if err != nil {
		t.Fatalf("exact-plan retry after completion: %v", err)
	}
	if retried.Coalesced || retried.Run == nil || retried.Run.ID != first.Run.ID {
		t.Fatal("exact-plan retry must reuse the completed run without coalescing")
	}
	select {
	case taskID := <-wakeups.notifications:
		t.Fatalf("exact-plan retry must not re-wake the completed task %s", taskID)
	default:
	}

	type concurrentResult struct {
		result *service.ScheduledAutopilotDispatchResult
		err    error
	}
	results := make(chan concurrentResult, 2)
	for _, plan := range []time.Time{firstPlan.Add(25 * time.Minute), firstPlan.Add(30 * time.Minute)} {
		plan := plan
		go func() {
			result, err := autopilotSvc.DispatchScheduledAutopilotForPlan(
				ctx, ap, trigger, "schedule", nil, plan,
			)
			results <- concurrentResult{result: result, err: err}
		}()
	}
	created, coalesced := 0, 0
	for range 2 {
		out := <-results
		if out.err != nil {
			t.Fatalf("concurrent scheduled dispatch: %v", out.err)
		}
		if out.result.Coalesced {
			coalesced++
		} else {
			created++
		}
	}
	if created != 1 || coalesced != 1 {
		t.Fatalf("concurrent dispatches must create one and coalesce one, got created=%d coalesced=%d", created, coalesced)
	}

	if err := testPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM autopilot_run WHERE autopilot_id = $1`, ap.ID,
	).Scan(&runCount); err != nil {
		t.Fatalf("count runs after terminal release: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		SELECT COUNT(*)
		  FROM agent_task_queue task
		  JOIN autopilot_run run ON run.id = task.autopilot_run_id
		 WHERE run.autopilot_id = $1
	`, ap.ID).Scan(&taskCount); err != nil {
		t.Fatalf("count tasks after terminal release: %v", err)
	}
	if runCount != 2 || taskCount != 2 {
		t.Fatalf("terminal release plus concurrent admission must leave two total run/tasks, got runs=%d tasks=%d", runCount, taskCount)
	}

	if _, err := queries.UpdateAutopilot(ctx, db.UpdateAutopilotParams{
		ID:     ap.ID,
		Status: pgtype.Text{String: "paused", Valid: true},
	}); err != nil {
		t.Fatalf("pause autopilot: %v", err)
	}
	paused, err := autopilotSvc.DispatchScheduledAutopilotForPlan(
		ctx, ap, trigger, "schedule", nil, firstPlan.Add(35*time.Minute),
	)
	if err != nil {
		t.Fatalf("dispatch with stale active snapshot: %v", err)
	}
	if paused.SkippedReason != "autopilot_inactive" || paused.Run != nil {
		t.Fatalf("locked status recheck must fail closed, got %+v", paused)
	}
	if err := testPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM autopilot_run WHERE autopilot_id = $1`, ap.ID,
	).Scan(&runCount); err != nil {
		t.Fatalf("count runs after paused race: %v", err)
	}
	if runCount != 2 {
		t.Fatalf("paused race must create zero runs, got total=%d", runCount)
	}
}

func TestDispatchAutopilotSuppressesRecentDuplicateIssue(t *testing.T) {
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

	title := "Autopilot recent duplicate issue " + time.Now().UTC().Format("20060102150405.000000000")
	ap, err := queries.CreateAutopilot(ctx, db.CreateAutopilotParams{
		WorkspaceID:        parseUUID(testWorkspaceID),
		Title:              "Recent duplicate issue guard",
		Description:        pgtype.Text{String: "Recent duplicate issue guard test", Valid: true},
		AssigneeType:       "agent",
		AssigneeID:         parseUUID(agentID),
		Status:             "active",
		ExecutionMode:      "create_issue",
		IssueTitleTemplate: pgtype.Text{String: title, Valid: true},
		CreatedByType:      "member",
		CreatedByID:        parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("CreateAutopilot: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = testPool.Exec(bg, `DELETE FROM autopilot WHERE id = $1`, ap.ID)
		_, _ = testPool.Exec(bg, `DELETE FROM issue WHERE workspace_id = $1 AND title = $2`, testWorkspaceID, title)
	})

	first, err := autopilotSvc.DispatchAutopilot(ctx, ap, pgtype.UUID{}, "manual", nil)
	if err != nil {
		t.Fatalf("first DispatchAutopilot: %v", err)
	}
	if first == nil || first.Status != "issue_created" || !first.IssueID.Valid {
		t.Fatalf("first dispatch = %+v, want issue_created with issue_id", first)
	}

	second, err := autopilotSvc.DispatchAutopilot(ctx, ap, pgtype.UUID{}, "manual", nil)
	if err != nil {
		t.Fatalf("second DispatchAutopilot: %v", err)
	}
	if second == nil || second.Status != "skipped" {
		t.Fatalf("second dispatch = %+v, want skipped duplicate run", second)
	}
	if second.IssueID.Valid {
		t.Fatalf("duplicate run linked issue_id=%s, want no new issue", util.UUIDToString(second.IssueID))
	}

	var count int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM issue WHERE workspace_id = $1 AND title = $2`,
		testWorkspaceID, title,
	).Scan(&count); err != nil {
		t.Fatalf("count issues: %v", err)
	}
	if count != 1 {
		t.Fatalf("recent duplicate autopilot dispatch should leave 1 matching issue, got %d", count)
	}
}

// TestDispatchAutopilotForPlanRejectsZeroArgs locks in the
// fail-loud contract: a caller that forgets to set trigger_id or
// planned_at would silently disable the idempotency guard, and the
// only safe answer is an error.
func TestDispatchAutopilotForPlanRejectsZeroArgs(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, taskSvc)

	ap := db.Autopilot{
		ID:            parseUUID(testWorkspaceID), // placeholder; will not be loaded since validation fails first
		WorkspaceID:   parseUUID(testWorkspaceID),
		ExecutionMode: "run_only",
		AssigneeType:  "agent",
		AssigneeID:    parseUUID(testWorkspaceID), // arbitrary; we never get past the input guard
		Status:        "active",
	}

	t.Run("invalid trigger_id", func(t *testing.T) {
		_, err := autopilotSvc.DispatchAutopilotForPlan(
			ctx, ap, pgtype.UUID{}, "schedule", nil, time.Now().UTC(),
		)
		if err == nil {
			t.Fatalf("expected error for invalid trigger_id")
		}
	})

	t.Run("zero planned_at", func(t *testing.T) {
		_, err := autopilotSvc.DispatchAutopilotForPlan(
			ctx, ap, parseUUID(testWorkspaceID), "schedule", nil, time.Time{},
		)
		if err == nil {
			t.Fatalf("expected error for zero planned_at")
		}
	})
}

// TestDispatchAutopilotForPlanRecoversPartialRun is the regression
// for the #4443 review blocker:
//
//	"DispatchAutopilotForPlan reuses existing run unconditionally,
//	 will mark a half-written run as SUCCESS even when no
//	 issue/task was ever created."
//
// We seed a partial-state autopilot_run for (trigger, planned_at) —
// the run exists with a non-terminal status but the corresponding
// downstream linkage (task_id for run_only, issue_id for create_issue)
// is NULL. A subsequent DispatchAutopilotForPlan call at the same
// (trigger, planned_at) MUST NOT return the partial row as-is;
// instead it must mark the partial row FAILED + clear its planned_at
// to release the partial-unique slot, then create a fresh dispatched
// run with the downstream linkage actually populated.
func TestDispatchAutopilotForPlanRecoversPartialRun(t *testing.T) {
	for _, mode := range []string{"run_only", "create_issue"} {
		t.Run(mode, func(t *testing.T) {
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

			ap, err := queries.CreateAutopilot(ctx, db.CreateAutopilotParams{
				WorkspaceID:        parseUUID(testWorkspaceID),
				Title:              "Partial recovery " + mode,
				Description:        pgtype.Text{String: "partial run recovery test", Valid: true},
				AssigneeType:       "agent",
				AssigneeID:         parseUUID(agentID),
				Status:             "active",
				ExecutionMode:      mode,
				IssueTitleTemplate: pgtype.Text{String: "Partial recovery", Valid: true},
				CreatedByType:      "member",
				CreatedByID:        parseUUID(testUserID),
			})
			if err != nil {
				t.Fatalf("CreateAutopilot: %v", err)
			}
			t.Cleanup(func() {
				if _, err := testPool.Exec(context.Background(),
					`DELETE FROM autopilot WHERE id = $1`, ap.ID); err != nil {
					t.Logf("cleanup autopilot: %v", err)
				}
			})

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

			plannedAt := time.Now().UTC().Truncate(time.Second).Add(-2 * time.Minute)
			plannedTS := pgtype.Timestamptz{Time: plannedAt, Valid: true}

			// Seed a PARTIAL run: a prior attempt wrote the run row
			// (status reflects the dispatch path's initial state) but
			// crashed before creating the downstream resource —
			// task_id is NULL for run_only, issue_id is NULL for
			// create_issue.
			initialStatus := "running"
			if mode == "create_issue" {
				initialStatus = "issue_created"
			}
			partial, err := queries.CreateAutopilotRun(ctx, db.CreateAutopilotRunParams{
				AutopilotID:    ap.ID,
				TriggerID:      trigger.ID,
				Source:         "schedule",
				Status:         initialStatus,
				TriggerPayload: nil,
				PlannedAt:      plannedTS,
			})
			if err != nil {
				t.Fatalf("seed partial run: %v", err)
			}
			// Confirm the partial state: no downstream linkage.
			if partial.TaskID.Valid {
				t.Fatalf("seed partial run should have task_id=NULL, got %s", util.UUIDToString(partial.TaskID))
			}
			if partial.IssueID.Valid {
				t.Fatalf("seed partial run should have issue_id=NULL, got %s", util.UUIDToString(partial.IssueID))
			}

			// Retry the dispatch — this is the stale-steal codepath.
			fresh, err := autopilotSvc.DispatchAutopilotForPlan(
				ctx, ap, trigger.ID, "schedule", nil, plannedAt,
			)
			if err != nil {
				t.Fatalf("DispatchAutopilotForPlan retry: %v", err)
			}
			if fresh == nil {
				t.Fatalf("retry returned nil run")
			}
			if fresh.ID == partial.ID {
				t.Fatalf("retry must NOT reuse the partial run; got the same id %s", util.UUIDToString(fresh.ID))
			}

			// The partial row must now be FAILED with planned_at
			// cleared, so the new row's planned_at is unique.
			var partialStatus string
			var partialPlannedAt pgtype.Timestamptz
			var partialFailureReason pgtype.Text
			if err := testPool.QueryRow(ctx,
				`SELECT status, planned_at, failure_reason FROM autopilot_run WHERE id = $1`,
				partial.ID,
			).Scan(&partialStatus, &partialPlannedAt, &partialFailureReason); err != nil {
				t.Fatalf("read partial row after recovery: %v", err)
			}
			if partialStatus != "failed" {
				t.Fatalf("partial run must be marked failed, got %q", partialStatus)
			}
			if partialPlannedAt.Valid {
				t.Fatalf("partial run planned_at must be cleared to release partial-unique slot, still valid")
			}
			if !partialFailureReason.Valid || partialFailureReason.String == "" {
				t.Fatalf("partial run must carry a recovery failure_reason for ops, got empty")
			}

			// The fresh row must carry the original planned_at and a
			// real downstream linkage from the just-completed
			// dispatch.
			if !fresh.PlannedAt.Valid {
				t.Fatalf("fresh run planned_at must be set")
			}
			if !fresh.PlannedAt.Time.Equal(plannedAt) {
				t.Fatalf("fresh run planned_at mismatch: got %s want %s",
					fresh.PlannedAt.Time.Format(time.RFC3339Nano),
					plannedAt.Format(time.RFC3339Nano))
			}
			switch mode {
			case "run_only":
				if !fresh.TaskID.Valid {
					t.Fatalf("run_only retry must produce a run with task_id set")
				}
			case "create_issue":
				if !fresh.IssueID.Valid {
					t.Fatalf("create_issue retry must produce a run with issue_id set")
				}
			}

			// Verify the partial-unique constraint is happy: exactly
			// one row per (trigger_id, planned_at) where both are
			// non-NULL.
			var liveRows int
			if err := testPool.QueryRow(ctx, `
				SELECT COUNT(*) FROM autopilot_run
				 WHERE trigger_id = $1 AND planned_at = $2
			`, trigger.ID, plannedTS).Scan(&liveRows); err != nil {
				t.Fatalf("count live (trigger, planned) rows: %v", err)
			}
			if liveRows != 1 {
				t.Fatalf("expected exactly 1 live row at (trigger, planned_at) after recovery, got %d", liveRows)
			}
		})
	}
}
