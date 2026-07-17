package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestAutopilotRunOnlyTaskTerminalEventsUpdateRun(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, taskSvc)
	registerAutopilotListeners(bus, autopilotSvc)

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id::text FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load fixture agent: %v", err)
	}

	tests := []struct {
		name       string
		finalize   func(task db.AgentTaskQueue)
		wantStatus string
		wantResult string
		wantReason string
	}{
		{
			name: "completed",
			finalize: func(task db.AgentTaskQueue) {
				if _, err := taskSvc.CompleteTask(ctx, task.ID, []byte(`{"output":"done"}`), "", ""); err != nil {
					t.Fatalf("CompleteTask: %v", err)
				}
			},
			wantStatus: "completed",
			wantResult: "done",
		},
		{
			name: "failed",
			finalize: func(task db.AgentTaskQueue) {
				if _, err := taskSvc.FailTask(ctx, task.ID, "boom", "", "", "agent_error"); err != nil {
					t.Fatalf("FailTask: %v", err)
				}
			},
			wantStatus: "failed",
			wantReason: "boom",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ap, err := queries.CreateAutopilot(ctx, db.CreateAutopilotParams{
				WorkspaceID:        parseUUID(testWorkspaceID),
				Title:              "Run-only listener " + tc.name,
				Description:        pgtype.Text{String: "Run listener regression test", Valid: true},
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

			run, err := autopilotSvc.DispatchAutopilot(ctx, ap, pgtype.UUID{}, "manual", nil)
			if err != nil {
				t.Fatalf("DispatchAutopilot: %v", err)
			}
			if !run.TaskID.Valid {
				t.Fatal("run_only dispatch did not link a task")
			}

			if _, err := testPool.Exec(ctx,
				`UPDATE agent_task_queue SET status = 'dispatched', dispatched_at = now() WHERE id = $1`,
				run.TaskID,
			); err != nil {
				t.Fatalf("mark task dispatched: %v", err)
			}
			task, err := queries.StartAgentTask(ctx, run.TaskID)
			if err != nil {
				t.Fatalf("StartAgentTask: %v", err)
			}

			tc.finalize(task)

			updatedRun, err := queries.GetAutopilotRun(ctx, run.ID)
			if err != nil {
				t.Fatalf("GetAutopilotRun: %v", err)
			}
			if updatedRun.Status != tc.wantStatus {
				t.Fatalf("expected run status %q, got %q", tc.wantStatus, updatedRun.Status)
			}
			if tc.wantResult != "" && !strings.Contains(string(updatedRun.Result), tc.wantResult) {
				t.Fatalf("expected run result to contain %q, got %s", tc.wantResult, string(updatedRun.Result))
			}
			if tc.wantReason != "" {
				if !updatedRun.FailureReason.Valid {
					t.Fatalf("expected failure reason %q, got invalid", tc.wantReason)
				}
				if updatedRun.FailureReason.String != tc.wantReason {
					t.Fatalf("expected failure reason %q, got %q", tc.wantReason, updatedRun.FailureReason.String)
				}
			}
		})
	}
}

// linkedIssueAutopilotFixture is the starting state every create_issue
// linked-issue listener test shares: a dispatched create_issue run sitting in
// issue_created with exactly one issue task that carries no autopilot_run_id
// (so it must be reached via the issue_id lookup, not SyncRunFromTask).
type linkedIssueAutopilotFixture struct {
	taskSvc *service.TaskService
	svc     *service.AutopilotService
	queries *db.Queries
	run     *db.AutopilotRun
	taskID  pgtype.UUID
}

// dispatchCreateIssueAutopilot creates an active create_issue autopilot,
// dispatches it, and returns the linked run plus its single issue task.
// Cleanup (autopilot, issue, tasks, comments) is registered on t.
func dispatchCreateIssueAutopilot(t *testing.T, title string) linkedIssueAutopilotFixture {
	t.Helper()
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, taskSvc)
	registerAutopilotListeners(bus, autopilotSvc)

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id::text FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load fixture agent: %v", err)
	}

	ap, err := queries.CreateAutopilot(ctx, db.CreateAutopilotParams{
		WorkspaceID:        parseUUID(testWorkspaceID),
		Title:              title,
		Description:        pgtype.Text{String: "VEN-661 / VEN-662 regression test", Valid: true},
		AssigneeType:       "agent",
		AssigneeID:         parseUUID(agentID),
		Status:             "active",
		ExecutionMode:      "create_issue",
		IssueTitleTemplate: pgtype.Text{String: "Linked issue", Valid: true},
		CreatedByType:      "member",
		CreatedByID:        parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("CreateAutopilot: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, ap.ID)
	})

	run, err := autopilotSvc.DispatchAutopilot(ctx, ap, pgtype.UUID{}, "schedule", nil)
	if err != nil {
		t.Fatalf("DispatchAutopilot: %v", err)
	}
	if !run.IssueID.Valid {
		t.Fatal("create_issue dispatch did not link an issue")
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, run.IssueID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, run.IssueID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, run.IssueID)
	})

	tasks, err := queries.ListTasksByIssue(ctx, run.IssueID)
	if err != nil {
		t.Fatalf("ListTasksByIssue: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected one issue task, got %d", len(tasks))
	}
	if tasks[0].AutopilotRunID.Valid {
		t.Fatal("create_issue issue task unexpectedly has autopilot_run_id; the run owns the pointer via run.task_id instead")
	}
	// MUL-4809 §4.1: dispatch now binds the run to its dispatched task and advances
	// to running. The run finalizes on that task's terminal state (matched by
	// run.task_id + retry lineage), not on the issue status.
	if run.Status != "running" {
		t.Fatalf("expected post-dispatch run status running, got %q", run.Status)
	}
	if run.TaskID.Bytes != tasks[0].ID.Bytes {
		t.Fatalf("run.task_id not bound to the dispatched task: run=%x task=%x", run.TaskID.Bytes, tasks[0].ID.Bytes)
	}

	return linkedIssueAutopilotFixture{taskSvc: taskSvc, svc: autopilotSvc, queries: queries, run: run, taskID: tasks[0].ID}
}

// runTaskWithBudget marks the issue task dispatched with the given attempt
// budget and transitions it to running, mirroring the daemon claim → start
// flow so FailTask sees a realistic row (and so the auto-retry budget is
// whatever the test wants).
func runTaskWithBudget(t *testing.T, queries *db.Queries, taskID pgtype.UUID, maxAttempts int) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(),
		`UPDATE agent_task_queue SET status = 'dispatched', dispatched_at = now(), max_attempts = $2 WHERE id = $1`,
		taskID, maxAttempts,
	); err != nil {
		t.Fatalf("mark task dispatched: %v", err)
	}
	if _, err := queries.StartAgentTask(context.Background(), taskID); err != nil {
		t.Fatalf("StartAgentTask: %v", err)
	}
}

// TestAutopilotCreateIssueTaskNoProgressFailureUpdatesRun is the original
// VEN-661 regression: a Codex no-progress failure with no retries left fails
// the linked run.
func TestAutopilotCreateIssueTaskNoProgressFailureUpdatesRun(t *testing.T) {
	ctx := context.Background()
	f := dispatchCreateIssueAutopilot(t, "Create-issue no-progress listener")

	// max_attempts = 1 means the failed attempt has no retry budget left.
	runTaskWithBudget(t, f.queries, f.taskID, 1)

	const errMsg = "codex app-server no progress timeout after 30s"
	if _, err := f.taskSvc.FailTask(ctx, f.taskID, errMsg, "", "", "codex_semantic_inactivity"); err != nil {
		t.Fatalf("FailTask: %v", err)
	}

	updatedRun, err := f.queries.GetAutopilotRun(ctx, f.run.ID)
	if err != nil {
		t.Fatalf("GetAutopilotRun: %v", err)
	}
	if updatedRun.Status != "failed" {
		t.Fatalf("expected run status failed, got %q", updatedRun.Status)
	}
	if !updatedRun.FailureReason.Valid || !strings.Contains(updatedRun.FailureReason.String, "no progress timeout") {
		t.Fatalf("expected no-progress failure reason, got %+v", updatedRun.FailureReason)
	}
}

// TestAutopilotCreateIssueTaskAgentErrorFailureUpdatesRun covers the VEN-662
// generalization: an ordinary, non-retryable agent failure must also close the
// linked run instead of leaving it stuck in issue_created.
func TestAutopilotCreateIssueTaskAgentErrorFailureUpdatesRun(t *testing.T) {
	ctx := context.Background()
	f := dispatchCreateIssueAutopilot(t, "Create-issue agent-error listener")

	runTaskWithBudget(t, f.queries, f.taskID, 1)

	// agent_error is not in retryableReasons, so the first terminal failure is
	// final — the run must fail carrying the agent's error text.
	const errMsg = "build failed: ./pkg/foo: undefined: Bar"
	if _, err := f.taskSvc.FailTask(ctx, f.taskID, errMsg, "", "", "agent_error"); err != nil {
		t.Fatalf("FailTask: %v", err)
	}

	updatedRun, err := f.queries.GetAutopilotRun(ctx, f.run.ID)
	if err != nil {
		t.Fatalf("GetAutopilotRun: %v", err)
	}
	if updatedRun.Status != "failed" {
		t.Fatalf("expected run status failed, got %q", updatedRun.Status)
	}
	if !updatedRun.FailureReason.Valid || !strings.Contains(updatedRun.FailureReason.String, "build failed") {
		t.Fatalf("expected agent-error failure reason, got %+v", updatedRun.FailureReason)
	}
}

// TestAutopilotCreateIssueTaskRetryPendingKeepsRunOpen locks in the wait guard:
// when FailTask auto-retries a retryable failure (attempt budget remaining), an
// active retry task still exists for the issue, so the run must stay open until
// the final attempt resolves.
func TestAutopilotCreateIssueTaskRetryPendingKeepsRunOpen(t *testing.T) {
	ctx := context.Background()
	f := dispatchCreateIssueAutopilot(t, "Create-issue retry-pending listener")

	// max_attempts = 2 with attempt = 1 leaves budget for one auto-retry.
	runTaskWithBudget(t, f.queries, f.taskID, 2)

	// timeout is retryable, so FailTask enqueues a fresh attempt before it
	// broadcasts the failure event.
	if _, err := f.taskSvc.FailTask(ctx, f.taskID, "runtime went offline", "", "", "timeout"); err != nil {
		t.Fatalf("FailTask: %v", err)
	}

	hasActive, err := f.queries.HasActiveTaskForIssue(ctx, f.run.IssueID)
	if err != nil {
		t.Fatalf("HasActiveTaskForIssue: %v", err)
	}
	if !hasActive {
		t.Fatal("expected an active retry task for the issue after a retryable failure")
	}

	updatedRun, err := f.queries.GetAutopilotRun(ctx, f.run.ID)
	if err != nil {
		t.Fatalf("GetAutopilotRun: %v", err)
	}
	if updatedRun.Status != "running" {
		t.Fatalf("expected run to stay running (open) while a retry is pending, got %q", updatedRun.Status)
	}
}

// TestAutopilotCreateIssueTaskCompletionUpdatesRun is the core §4.1 decoupling:
// a create_issue run now COMPLETES on its dispatched task's terminal success —
// no issue status change involved.
func TestAutopilotCreateIssueTaskCompletionUpdatesRun(t *testing.T) {
	ctx := context.Background()
	f := dispatchCreateIssueAutopilot(t, "Create-issue completion listener")

	runTaskWithBudget(t, f.queries, f.taskID, 1)

	if _, err := f.taskSvc.CompleteTask(ctx, f.taskID, []byte(`{"output":"done"}`), "", ""); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}

	updatedRun, err := f.queries.GetAutopilotRun(ctx, f.run.ID)
	if err != nil {
		t.Fatalf("GetAutopilotRun: %v", err)
	}
	if updatedRun.Status != "completed" {
		t.Fatalf("expected run status completed after the dispatched task completed, got %q", updatedRun.Status)
	}
	if !updatedRun.CompletedAt.Valid {
		t.Fatal("expected completed_at to be set")
	}
}

// TestAutopilotCreateIssueCommentTaskDoesNotFinalizeRun locks in the precise
// lineage match: a LATER task on the same issue that is NOT the run's dispatched
// task (nor a retry of it) — e.g. a comment-triggered task — must never finalize
// the run. This is why the run binds to a specific task_id instead of matching
// by issue alone.
func TestAutopilotCreateIssueCommentTaskDoesNotFinalizeRun(t *testing.T) {
	ctx := context.Background()
	f := dispatchCreateIssueAutopilot(t, "Create-issue comment-task listener")

	// A fabricated terminal task on the same issue with no retry_of lineage to the
	// run's dispatched task and no autopilot_run_id — its retry-root is itself, so
	// it can't match run.task_id.
	commentTask := db.AgentTaskQueue{
		ID:      parseUUID("11111111-1111-1111-1111-111111111111"),
		IssueID: f.run.IssueID,
		Status:  "completed",
	}
	if commentTask.ID.Bytes == f.run.TaskID.Bytes {
		t.Fatal("fabricated comment task id collided with the run's dispatched task id")
	}
	f.svc.SyncRunFromCreateIssueTask(ctx, commentTask)

	updatedRun, err := f.queries.GetAutopilotRun(ctx, f.run.ID)
	if err != nil {
		t.Fatalf("GetAutopilotRun: %v", err)
	}
	if updatedRun.Status != "running" {
		t.Fatalf("a non-lineage comment task finalized the run (status %q); it must stay running", updatedRun.Status)
	}
}

// TestAutopilotDispatchSkipsWhenRuntimeOffline locks in the MUL-1899
// admission gate: when the assignee agent's runtime is not online we must
// record a `skipped` autopilot_run with a failure_reason and NOT enqueue an
// agent_task_queue row. This is the fix for "活跃 schedule 持续给离线 local
// agent 入队".
func TestAutopilotDispatchSkipsWhenRuntimeOffline(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, taskSvc)

	// Spin up a dedicated runtime + agent so we can flip the runtime to
	// offline without affecting the shared fixture used by other tests.
	var runtimeID, agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at
		)
		VALUES ($1, NULL, 'Offline runtime', 'local', 'mul1899_offline_runtime', 'offline', '{}'::jsonb, '{}'::jsonb, now())
		RETURNING id::text
	`, parseUUID(testWorkspaceID)).Scan(&runtimeID); err != nil {
		t.Fatalf("create offline runtime: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, 'mul1899-offline-agent', '', 'local', '{}'::jsonb, $2, 'workspace', 1, $3)
		RETURNING id::text
	`, parseUUID(testWorkspaceID), runtimeID, parseUUID(testUserID)).Scan(&agentID); err != nil {
		t.Fatalf("create offline agent: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})

	ap, err := queries.CreateAutopilot(ctx, db.CreateAutopilotParams{
		WorkspaceID:        parseUUID(testWorkspaceID),
		Title:              "Offline-runtime autopilot",
		Description:        pgtype.Text{String: "MUL-1899 admission test", Valid: true},
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
		_, _ = testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, ap.ID)
	})

	run, err := autopilotSvc.DispatchAutopilot(ctx, ap, pgtype.UUID{}, "schedule", nil)
	if err != nil {
		t.Fatalf("DispatchAutopilot: %v", err)
	}
	if run == nil {
		t.Fatal("expected a run, got nil")
	}
	if run.Status != "skipped" {
		t.Fatalf("expected run status 'skipped', got %q", run.Status)
	}
	if !run.FailureReason.Valid || !strings.Contains(run.FailureReason.String, "offline") {
		t.Fatalf("expected failure reason mentioning 'offline', got %+v", run.FailureReason)
	}
	if run.TaskID.Valid {
		t.Fatalf("expected no task to be enqueued, got task_id %v", run.TaskID)
	}

	// Defensive: confirm at the DB layer that nothing landed on the queue.
	var taskCount int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_task_queue WHERE agent_id = $1`,
		agentID,
	).Scan(&taskCount); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if taskCount != 0 {
		t.Fatalf("expected 0 queued tasks for offline-runtime agent, got %d", taskCount)
	}
}

// TestAutopilotCreateIssueDispatchCreatesIssueWhenRuntimeOffline locks in the
// audit-trail contract for tracked autopilots: create_issue mode must still
// create a visible issue when the assignee runtime is offline, leaving the
// issue/task to be claimed when the runtime comes back instead of silently
// recording an unrecoverable skipped run.
func TestAutopilotCreateIssueDispatchCreatesIssueWhenRuntimeOffline(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, taskSvc)

	var runtimeID, agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at
		)
		VALUES ($1, NULL, 'Offline create-issue runtime', 'local', 'ws1325_offline_runtime', 'offline', '{}'::jsonb, '{}'::jsonb, now())
		RETURNING id::text
	`, parseUUID(testWorkspaceID)).Scan(&runtimeID); err != nil {
		t.Fatalf("create offline runtime: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, 'ws1325-offline-create-issue-agent', '', 'local', '{}'::jsonb, $2, 'workspace', 1, $3)
		RETURNING id::text
	`, parseUUID(testWorkspaceID), runtimeID, parseUUID(testUserID)).Scan(&agentID); err != nil {
		t.Fatalf("create offline agent: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})

	ap, err := queries.CreateAutopilot(ctx, db.CreateAutopilotParams{
		WorkspaceID:        parseUUID(testWorkspaceID),
		Title:              "Offline create-issue autopilot",
		Description:        pgtype.Text{String: "WS-1325 regression test", Valid: true},
		AssigneeType:       "agent",
		AssigneeID:         parseUUID(agentID),
		Status:             "active",
		ExecutionMode:      "create_issue",
		IssueTitleTemplate: pgtype.Text{String: "Tracked issue", Valid: true},
		CreatedByType:      "member",
		CreatedByID:        parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("CreateAutopilot: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, ap.ID)
	})

	run, err := autopilotSvc.DispatchAutopilot(ctx, ap, pgtype.UUID{}, "schedule", nil)
	if err != nil {
		t.Fatalf("DispatchAutopilot: %v", err)
	}
	if run == nil {
		t.Fatal("expected a run, got nil")
	}
	if run.Status != "running" {
		t.Fatalf("expected run status 'running' after task-bound dispatch, got %q", run.Status)
	}
	if !run.IssueID.Valid {
		t.Fatal("create_issue dispatch did not link an issue")
	}
	if run.FailureReason.Valid {
		t.Fatalf("expected no failure reason, got %q", run.FailureReason.String)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, run.IssueID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, run.IssueID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, run.IssueID)
	})

	tasks, err := queries.ListTasksByIssue(ctx, run.IssueID)
	if err != nil {
		t.Fatalf("ListTasksByIssue: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected one queued issue task, got %d", len(tasks))
	}
	if tasks[0].AgentID != parseUUID(agentID) {
		t.Fatalf("task agent mismatch: got %v want %v", tasks[0].AgentID, parseUUID(agentID))
	}
	if tasks[0].RuntimeID != parseUUID(runtimeID) {
		t.Fatalf("task runtime mismatch: got %v want %v", tasks[0].RuntimeID, parseUUID(runtimeID))
	}
}

// TestManualTriggerDoesNotErrorOnPostAdmissionSkip locks in PR #2888 review
// fix #2: if the dispatcher decides to skip after the admission gate has
// already passed (e.g. the leader's runtime went offline between admission
// and task creation), DispatchAutopilot must return (run, nil) with
// status='skipped' rather than (nil, err). Without this, manual trigger
// surfaces a 500 to the user even though the work was correctly suppressed
// — the same regression Emacs flagged on the original PR.
//
// We synthesise the race by:
//  1. Creating an online runtime + agent so the admission gate passes.
//  2. Flipping the runtime to offline.
//  3. Triggering the autopilot. Admission has already loaded the agent +
//     runtime once with status='online' at row-fetch time, so the second
//     check inside dispatchRunOnly is what catches the offline state.
//
// In this implementation the admission gate also re-reads the runtime, so
// the same offline state actually fires the admission skip first. That is
// fine for the assertion we care about: the manual trigger must not 500 and
// the run must be `skipped`. The post-admission branch is exercised
// separately by the errDispatchSkipped unwrap unit test in the service
// package.
func TestManualTriggerDoesNotErrorOnPostAdmissionSkip(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, taskSvc)

	var runtimeID, agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at
		)
		VALUES ($1, NULL, 'Manual-trigger skip runtime', 'local', 'mul2429_manual_skip_runtime', 'offline', '{}'::jsonb, '{}'::jsonb, now())
		RETURNING id::text
	`, parseUUID(testWorkspaceID)).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, 'mul2429-manual-skip-agent', '', 'local', '{}'::jsonb, $2, 'workspace', 1, $3)
		RETURNING id::text
	`, parseUUID(testWorkspaceID), runtimeID, parseUUID(testUserID)).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})

	ap, err := queries.CreateAutopilot(ctx, db.CreateAutopilotParams{
		WorkspaceID:        parseUUID(testWorkspaceID),
		Title:              "Manual-trigger skip autopilot",
		Description:        pgtype.Text{String: "PR #2888 review fix #2", Valid: true},
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
		_, _ = testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, ap.ID)
	})

	run, err := autopilotSvc.DispatchAutopilot(ctx, ap, pgtype.UUID{}, "manual", nil)
	if err != nil {
		t.Fatalf("manual DispatchAutopilot returned error (would 500 the handler): %v", err)
	}
	if run == nil {
		t.Fatal("expected a run, got nil")
	}
	if run.Status != "skipped" {
		t.Fatalf("expected run status 'skipped', got %q", run.Status)
	}
}

// TestAutopilotRunTerminalTransitionsAreCAS locks in the compare-and-set
// contract (MUL-4809 §4.1 P0-1): once a run is terminal, neither a racing
// terminal write nor a late bind may overwrite or resurrect it. Concurrent
// finalizers OF THIS BINARY become first-writer-wins (the loser gets
// pgx.ErrNoRows). (Note: this cannot constrain an old-version pod still running
// the unguarded write during a rolling deploy — that needs the deploy-enable
// gate tracked as later work.)
func TestAutopilotRunTerminalTransitionsAreCAS(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id::text FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load fixture agent: %v", err)
	}
	ap, err := queries.CreateAutopilot(ctx, db.CreateAutopilotParams{
		WorkspaceID:        parseUUID(testWorkspaceID),
		Title:              "CAS transition test",
		AssigneeType:       "agent",
		AssigneeID:         parseUUID(agentID),
		Status:             "active",
		ExecutionMode:      "create_issue",
		IssueTitleTemplate: pgtype.Text{String: "x", Valid: true},
		CreatedByType:      "member",
		CreatedByID:        parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("CreateAutopilot: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, ap.ID) })

	run, err := queries.CreateAutopilotRun(ctx, db.CreateAutopilotRunParams{
		AutopilotID: ap.ID,
		Source:      "manual",
		Status:      "issue_created",
	})
	if err != nil {
		t.Fatalf("CreateAutopilotRun: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM autopilot_run WHERE id = $1`, run.ID) })

	// A terminal transition from an active run succeeds.
	completed, err := queries.UpdateAutopilotRunCompleted(ctx, db.UpdateAutopilotRunCompletedParams{ID: run.ID})
	if err != nil || completed.Status != "completed" {
		t.Fatalf("first complete: status=%q err=%v", completed.Status, err)
	}

	// A racing FAIL on the already-completed run must be rejected (CAS lost).
	if _, err := queries.UpdateAutopilotRunFailed(ctx, db.UpdateAutopilotRunFailedParams{
		ID: run.ID, FailureReason: pgtype.Text{String: "late failure", Valid: true},
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("fail on completed run: want pgx.ErrNoRows, got %v", err)
	}

	// A late BIND must not resurrect the completed run back to running.
	if _, err := queries.UpdateAutopilotRunRunning(ctx, db.UpdateAutopilotRunRunningParams{
		ID: run.ID, TaskID: parseUUID("22222222-2222-2222-2222-222222222222"),
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("bind on completed run: want pgx.ErrNoRows, got %v", err)
	}

	// The run is unchanged: still completed, and never bound to the late task.
	final, err := queries.GetAutopilotRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("reload run: %v", err)
	}
	if final.Status != "completed" || final.TaskID.Valid {
		t.Fatalf("run mutated after CAS-lost writes: status=%q task_id_valid=%v", final.Status, final.TaskID.Valid)
	}
}
