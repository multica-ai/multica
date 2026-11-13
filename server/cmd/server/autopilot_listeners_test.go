package main

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	dbfx "github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// seedAutopilotScheduleTrigger gives an autopilot a schedule trigger recording its
// creator. Since MUL-6951 an automatic dispatch resolves the human it acts as from
// this row — and admits the assignee agent as that human — so a dispatch with no
// trigger has no principal and fails closed.
func seedAutopilotScheduleTrigger(t *testing.T, queries *db.Queries, ap db.Autopilot) pgtype.UUID {
	t.Helper()
	trig, err := queries.CreateAutopilotTrigger(context.Background(), db.CreateAutopilotTriggerParams{
		AutopilotID:    ap.ID,
		Kind:           "schedule",
		Enabled:        true,
		CronExpression: pgtype.Text{String: "*/5 * * * *", Valid: true},
		Timezone:       pgtype.Text{String: "UTC", Valid: true},
		CreatedByType:  pgtype.Text{String: "member", Valid: ap.CreatedByType == "member"},
		CreatedByID:    ap.CreatedByID,
	})
	if err != nil {
		t.Fatalf("seed schedule trigger: %v", err)
	}
	return trig.ID
}

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
				if _, err := taskSvc.CompleteTask(ctx, task.ID, []byte(`{"output":"done"}`), "", "", "", false, "", ""); err != nil {
					t.Fatalf("CompleteTask: %v", err)
				}
			},
			wantStatus: "completed",
			wantResult: "done",
		},
		{
			name: "failed",
			finalize: func(task db.AgentTaskQueue) {
				if _, err := taskSvc.FailTask(ctx, task.ID, "boom", "", "", "", "agent_error", false, "", ""); err != nil {
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

			run, err := autopilotSvc.DispatchAutopilot(ctx, ap, seedAutopilotScheduleTrigger(t, queries, ap), "manual", nil)
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
			task, err := queries.StartAgentTaskWithSupplement(ctx, db.StartAgentTaskWithSupplementParams{TaskID: run.TaskID})
			if err != nil {
				t.Fatalf("StartAgentTaskWithSupplement: %v", err)
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
	taskSvc      *service.TaskService
	autopilotSvc *service.AutopilotService
	queries      *db.Queries
	run          *db.AutopilotRun
	taskID       pgtype.UUID
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

	run, err := autopilotSvc.DispatchAutopilot(ctx, ap, seedAutopilotScheduleTrigger(t, queries, ap), "schedule", nil)
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
		t.Fatal("create_issue issue task unexpectedly has autopilot_run_id; test must exercise linked issue lookup")
	}
	if run.Status != "issue_created" {
		t.Fatalf("expected pre-failure run status issue_created, got %q", run.Status)
	}

	return linkedIssueAutopilotFixture{taskSvc: taskSvc, autopilotSvc: autopilotSvc, queries: queries, run: run, taskID: tasks[0].ID}
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
	if _, err := queries.StartAgentTaskWithSupplement(context.Background(), db.StartAgentTaskWithSupplementParams{TaskID: taskID}); err != nil {
		t.Fatalf("StartAgentTaskWithSupplement: %v", err)
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
	if _, err := f.taskSvc.FailTask(ctx, f.taskID, errMsg, "", "", "", "codex_semantic_inactivity", false, "", ""); err != nil {
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
	if _, err := f.taskSvc.FailTask(ctx, f.taskID, errMsg, "", "", "", "agent_error", false, "", ""); err != nil {
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
	if _, err := f.taskSvc.FailTask(ctx, f.taskID, "runtime went offline", "", "", "", "timeout", false, "", ""); err != nil {
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
	if updatedRun.Status != "issue_created" {
		t.Fatalf("expected run to stay issue_created while a retry is pending, got %q", updatedRun.Status)
	}
}

// setLinkedIssueStatus moves the run's linked issue to status and feeds the
// reloaded row to SyncRunFromIssue, the same entry the issue:updated listener
// reaches after its status gate.
func setLinkedIssueStatus(t *testing.T, f linkedIssueAutopilotFixture, status string) {
	t.Helper()
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `UPDATE issue SET status = $2 WHERE id = $1`, f.run.IssueID, status); err != nil {
		t.Fatalf("set issue status %q: %v", status, err)
	}
	issue, err := f.queries.GetIssue(ctx, f.run.IssueID)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	f.autopilotSvc.SyncRunFromIssue(ctx, issue)
}

func requireRunStatus(t *testing.T, f linkedIssueAutopilotFixture, want string) db.AutopilotRun {
	t.Helper()
	run, err := f.queries.GetAutopilotRun(context.Background(), f.run.ID)
	if err != nil {
		t.Fatalf("GetAutopilotRun: %v", err)
	}
	if run.Status != want {
		t.Fatalf("expected run status %q, got %q (reason %+v)", want, run.Status, run.FailureReason)
	}
	return run
}

// TestAutopilotCreateIssueTaskCompletionCompletesRun: the first agent task on
// the linked issue finishing cleanly is run success, and a later blocked
// status on the issue must not flip it to failed. (MUL-8200)
func TestAutopilotCreateIssueTaskCompletionCompletesRun(t *testing.T) {
	ctx := context.Background()
	f := dispatchCreateIssueAutopilot(t, "Create-issue completion listener")

	runTaskWithBudget(t, f.queries, f.taskID, 1)
	if _, err := f.taskSvc.CompleteTask(ctx, f.taskID, []byte(`{"output":"done"}`), "", "", "", false, "", ""); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}
	requireRunStatus(t, f, "completed")

	setLinkedIssueStatus(t, f, "blocked")
	requireRunStatus(t, f, "completed")
	setLinkedIssueStatus(t, f, "cancelled")
	requireRunStatus(t, f, "completed")
}

// TestAutopilotCreateIssueBlockedIssueKeepsRunOpen is the MUL-8198 shape: a
// reviewer blocks the issue while the autopilot's task is still running. The
// run must stay open and complete once the task finishes. (MUL-8200)
func TestAutopilotCreateIssueBlockedIssueKeepsRunOpen(t *testing.T) {
	ctx := context.Background()
	f := dispatchCreateIssueAutopilot(t, "Create-issue blocked listener")

	runTaskWithBudget(t, f.queries, f.taskID, 1)
	setLinkedIssueStatus(t, f, "blocked")
	requireRunStatus(t, f, "issue_created")

	if _, err := f.taskSvc.CompleteTask(ctx, f.taskID, []byte(`{"output":"done"}`), "", "", "", false, "", ""); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}
	requireRunStatus(t, f, "completed")
}

// TestAutopilotCreateIssueCancelledBeforeTaskFailsRun: cancelling the issue
// before any task finishes still fails the run, so it does not hang. (MUL-8200)
func TestAutopilotCreateIssueCancelledBeforeTaskFailsRun(t *testing.T) {
	f := dispatchCreateIssueAutopilot(t, "Create-issue cancelled listener")

	setLinkedIssueStatus(t, f, "cancelled")
	run := requireRunStatus(t, f, "failed")
	if !run.FailureReason.Valid || run.FailureReason.String != "issue cancelled" {
		t.Fatalf("expected failure reason %q, got %+v", "issue cancelled", run.FailureReason)
	}
}

// TestAutopilotCreateIssueEarlyDeliveryWaitsForTaskOutcome: an issue moved to
// in_review/done while the dispatch task still runs must not complete the run;
// the task's failure decides it. (MUL-8200 review)
func TestAutopilotCreateIssueEarlyDeliveryWaitsForTaskOutcome(t *testing.T) {
	for _, status := range []string{"in_review", "done"} {
		t.Run(status, func(t *testing.T) {
			ctx := context.Background()
			f := dispatchCreateIssueAutopilot(t, "Create-issue early delivery "+status)
			runTaskWithBudget(t, f.queries, f.taskID, 1)
			setLinkedIssueStatus(t, f, status)
			before, err := f.queries.GetAutopilotRun(ctx, f.run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if before.Status != "issue_created" {
				t.Errorf("issue became %s while first task is still running: run=%s, want issue_created", status, before.Status)
			}
			if _, err := f.taskSvc.FailTask(ctx, f.taskID, "runtime exited after setting issue status", "", "", "", "agent_error", false, "", ""); err != nil {
				t.Fatal(err)
			}
			requireRunStatus(t, f, "failed")
		})
	}
}

// TestAutopilotCreateIssueUnrelatedMentionDoesNotSettleRun: a mention task on
// the same issue completing must not stand in for the autopilot's own
// dispatch. (MUL-8200 review)
func TestAutopilotCreateIssueUnrelatedMentionDoesNotSettleRun(t *testing.T) {
	ctx := context.Background()
	f := dispatchCreateIssueAutopilot(t, "Create-issue unrelated mention")
	runTaskWithBudget(t, f.queries, f.taskID, 1)
	first, err := f.queries.GetAgentTask(ctx, f.taskID)
	if err != nil {
		t.Fatal(err)
	}
	fixture := dbfx.New(testPool, testWorkspaceID, testUserID)
	otherAgent := fixture.Agent(t, "Other mentioned agent", util.UUIDToString(first.RuntimeID))
	comment := fixture.Comment(t, util.UUIDToString(f.run.IssueID), "Please answer a quick question")
	issue, err := f.queries.GetIssue(ctx, f.run.IssueID)
	if err != nil {
		t.Fatal(err)
	}
	other, err := f.taskSvc.EnqueueTaskForMention(ctx, issue, parseUUID(otherAgent), parseUUID(comment), service.OriginNamed)
	if err != nil {
		t.Fatal(err)
	}
	fixture.Cleanup(t, "DELETE FROM agent_task_queue WHERE id = $1", other.ID)
	runTaskWithBudget(t, f.queries, other.ID, 1)
	if _, err := f.taskSvc.CompleteTask(ctx, other.ID, []byte(`{"output":"Answer to the quick question"}`), "", "", "", false, "", ""); err != nil {
		t.Fatal(err)
	}
	before, err := f.queries.GetAutopilotRun(ctx, f.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if before.Status != "issue_created" {
		t.Errorf("unrelated mention completed while original task is running: run=%s, want issue_created", before.Status)
	}
	if _, err := f.taskSvc.FailTask(ctx, f.taskID, "original autopilot task crashed", "", "", "", "agent_error", false, "", ""); err != nil {
		t.Fatal(err)
	}
	requireRunStatus(t, f, "failed")
}

// TestAutopilotCreateIssueRetryCompletionCompletesRun: an auto-retry of the
// dispatch task is part of the dispatch chain, so its success completes the run.
func TestAutopilotCreateIssueRetryCompletionCompletesRun(t *testing.T) {
	ctx := context.Background()
	f := dispatchCreateIssueAutopilot(t, "Create-issue retry completion listener")

	runTaskWithBudget(t, f.queries, f.taskID, 2)
	if _, err := f.taskSvc.FailTask(ctx, f.taskID, "runtime went offline", "", "", "", "timeout", false, "", ""); err != nil {
		t.Fatalf("FailTask: %v", err)
	}
	requireRunStatus(t, f, "issue_created")

	var retryID pgtype.UUID
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent_task_queue WHERE retry_of_task_id = $1`, f.taskID).Scan(&retryID); err != nil {
		t.Fatalf("load retry task: %v", err)
	}
	runTaskWithBudget(t, f.queries, retryID, 2)
	if _, err := f.taskSvc.CompleteTask(ctx, retryID, []byte(`{"output":"done"}`), "", "", "", false, "", ""); err != nil {
		t.Fatalf("CompleteTask retry: %v", err)
	}
	requireRunStatus(t, f, "completed")
}

// TestAutopilotCreateIssueDeliveryAfterCommittedFailureFailsRun: the dispatch
// failure is committed but task:failed not yet processed when the issue moves
// to in_review; the committed failure must win. (MUL-8200 re-review)
func TestAutopilotCreateIssueDeliveryAfterCommittedFailureFailsRun(t *testing.T) {
	ctx := context.Background()
	f := dispatchCreateIssueAutopilot(t, "Delivery racing failure notification")
	runTaskWithBudget(t, f.queries, f.taskID, 1)
	observed := false
	// Failure comments publish after the failed row commits but before task:failed.
	// Interleave an issue status update in that real callback window.
	f.taskSvc.Bus.Subscribe(protocol.EventCommentCreated, func(e events.Event) {
		task, err := f.queries.GetAgentTask(ctx, f.taskID)
		if err != nil {
			t.Fatal(err)
		}
		if task.Status != "failed" {
			t.Fatalf("expected a committed failure, got %s", task.Status)
		}
		observed = true
		setLinkedIssueStatus(t, f, "in_review")
	})
	if _, err := f.taskSvc.FailTask(ctx, f.taskID, "dispatch task crashed", "", "", "", "agent_error", false, "", ""); err != nil {
		t.Fatal(err)
	}
	if !observed {
		t.Fatal("did not exercise the post-commit/pre-task-event window")
	}
	requireRunStatus(t, f, "failed")
}

// TestAutopilotCreateIssueMentionEnqueuedBeforeDispatch: a mention task
// enqueued between issue:created and the dispatch enqueue is older than the
// real dispatch and must still not settle the run. (MUL-8200 re-review)
func TestAutopilotCreateIssueMentionEnqueuedBeforeDispatch(t *testing.T) {
	ctx := context.Background()
	fixture := dbfx.New(testPool, testWorkspaceID, testUserID)
	runtimeID := fixture.Runtime(t, "Dispatch order runtime")
	ownerID := fixture.Agent(t, "Autopilot assignee", runtimeID)
	otherID := fixture.Agent(t, "Early mention agent", runtimeID)
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	apSvc := service.NewAutopilotService(queries, testPool, bus, taskSvc)
	registerAutopilotListeners(bus, apSvc)
	ap, err := queries.CreateAutopilot(ctx, db.CreateAutopilotParams{
		WorkspaceID: parseUUID(testWorkspaceID), Title: "Dispatch order autopilot",
		AssigneeType: "agent", AssigneeID: parseUUID(ownerID), Status: "active",
		ExecutionMode: "create_issue", IssueTitleTemplate: pgtype.Text{String: "Dispatch order issue", Valid: true},
		CreatedByType: "member", CreatedByID: parseUUID(testUserID),
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture.Cleanup(t, "DELETE FROM autopilot WHERE id = $1", ap.ID)
	var mention db.AgentTaskQueue
	// The issue is already committed and externally readable at issue:created.
	// Pause dispatch here to exercise a mention arriving before its later enqueue.
	bus.Subscribe(protocol.EventIssueCreated, func(e events.Event) {
		var issueID pgtype.UUID
		fixture.QueryRow(t, "SELECT id FROM issue WHERE origin_id = $1", ap.ID).Scan(&issueID)
		fixture.Cleanup(t, "DELETE FROM issue WHERE id = $1", issueID)
		fixture.Cleanup(t, "DELETE FROM comment WHERE issue_id = $1", issueID)
		fixture.Cleanup(t, "DELETE FROM agent_task_queue WHERE issue_id = $1", issueID)
		issue, err := queries.GetIssue(ctx, issueID)
		if err != nil {
			t.Fatal(err)
		}
		commentID := fixture.Comment(t, util.UUIDToString(issueID), "Quick question for the other agent")
		mention, err = taskSvc.EnqueueTaskForMention(ctx, issue, parseUUID(otherID), parseUUID(commentID), service.OriginNamed)
		if err != nil {
			t.Fatal(err)
		}
	})
	run, err := apSvc.DispatchAutopilot(ctx, ap, seedAutopilotScheduleTrigger(t, queries, ap), "schedule", nil)
	if err != nil {
		t.Fatal(err)
	}
	var dispatchedID pgtype.UUID
	fixture.QueryRow(t, "SELECT id FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2", run.IssueID, ownerID).Scan(&dispatchedID)
	f := linkedIssueAutopilotFixture{taskSvc: taskSvc, autopilotSvc: apSvc, queries: queries, run: run, taskID: dispatchedID}
	runTaskWithBudget(t, queries, dispatchedID, 1)
	runTaskWithBudget(t, queries, mention.ID, 1)
	if _, err := taskSvc.CompleteTask(ctx, mention.ID, []byte(`{"output":"Quick answer"}`), "", "", "", false, "", ""); err != nil {
		t.Fatal(err)
	}
	state, err := queries.GetAutopilotRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != "issue_created" {
		t.Errorf("early unrelated mention completed the run: got %s, want issue_created", state.Status)
	}
	if _, err := taskSvc.FailTask(ctx, dispatchedID, "real dispatch failed", "", "", "", "agent_error", false, "", ""); err != nil {
		t.Fatal(err)
	}
	requireRunStatus(t, f, "failed")
}

// TestAutopilotCreateIssueDeliveryBetweenEnqueueAndLink: in_review lands after
// the dispatch task is queued but before its link is written; the task's
// failure must still fail the run. (MUL-8200 third review)
func TestAutopilotCreateIssueDeliveryBetweenEnqueueAndLink(t *testing.T) {
	ctx := context.Background()
	fixture := dbfx.New(testPool, testWorkspaceID, testUserID)
	runtimeID := fixture.Runtime(t, "Link window runtime")
	agentID := fixture.Agent(t, "Link window assignee", runtimeID)
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	apSvc := service.NewAutopilotService(queries, testPool, bus, taskSvc)
	registerAutopilotListeners(bus, apSvc)
	ap, err := queries.CreateAutopilot(ctx, db.CreateAutopilotParams{
		WorkspaceID: parseUUID(testWorkspaceID), Title: "Link window autopilot",
		AssigneeType: "agent", AssigneeID: parseUUID(agentID), Status: "active",
		ExecutionMode: "create_issue", IssueTitleTemplate: pgtype.Text{String: "Link window issue", Valid: true},
		CreatedByType: "member", CreatedByID: parseUUID(testUserID),
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture.Cleanup(t, "DELETE FROM autopilot WHERE id = $1", ap.ID)
	var f linkedIssueAutopilotFixture
	// The task is committed and announced before EnqueueTaskForIssue returns.
	// A status request may be handled here before linkDispatchTask runs.
	bus.Subscribe(protocol.EventTaskQueued, func(e events.Event) {
		taskID := parseUUID(e.Payload.(map[string]any)["task_id"].(string))
		task, err := queries.GetAgentTask(ctx, taskID)
		if err != nil {
			t.Fatal(err)
		}
		run, err := queries.GetAutopilotRunByIssue(ctx, task.IssueID)
		if err != nil {
			t.Fatal(err)
		}
		if run.TaskID.Valid {
			t.Fatal("did not exercise the pre-link window")
		}
		fixture.Cleanup(t, "DELETE FROM issue WHERE id = $1", task.IssueID)
		fixture.Cleanup(t, "DELETE FROM comment WHERE issue_id = $1", task.IssueID)
		fixture.Cleanup(t, "DELETE FROM agent_task_queue WHERE issue_id = $1", task.IssueID)
		f = linkedIssueAutopilotFixture{taskSvc: taskSvc, autopilotSvc: apSvc, queries: queries, run: &run, taskID: taskID}
		runTaskWithBudget(t, queries, taskID, 1)
		setLinkedIssueStatus(t, f, "in_review")
	})
	if _, err := apSvc.DispatchAutopilot(ctx, ap, seedAutopilotScheduleTrigger(t, queries, ap), "schedule", nil); err != nil {
		t.Fatal(err)
	}
	if f.run == nil {
		t.Fatal("queued event was not observed")
	}
	if _, err := taskSvc.FailTask(ctx, f.taskID, "failed after early delivery", "", "", "", "agent_error", false, "", ""); err != nil {
		t.Fatal(err)
	}
	requireRunStatus(t, f, "failed")
}

// TestAutopilotCreateIssueWebhookRecoveryLinksDispatch: a webhook-recovered
// dispatch (crash before enqueue or before link) still settles the run from
// its task outcome. (MUL-8200 third review)
func TestAutopilotCreateIssueWebhookRecoveryLinksDispatch(t *testing.T) {
	for _, taskWasEnqueued := range []bool{false, true} {
		name := "before_enqueue"
		if taskWasEnqueued {
			name = "after_enqueue_before_link"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			f := dispatchCreateIssueAutopilot(t, "Webhook recovery "+name)
			fixture := dbfx.New(testPool, testWorkspaceID, testUserID)
			// Reproduce the durable state left by a worker crash after issue
			// creation, either before enqueue or just before recording the link.
			fixture.Exec(t, "UPDATE autopilot_run SET task_id = NULL, source = 'webhook', webhook_delivery_id = id WHERE id = $1", f.run.ID)
			if !taskWasEnqueued {
				fixture.Exec(t, "DELETE FROM agent_task_queue WHERE id = $1", f.taskID)
			}
			ap, err := f.queries.GetAutopilot(ctx, f.run.AutopilotID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.autopilotSvc.DispatchAutopilotForWebhookDelivery(ctx, ap, f.run.TriggerID, nil, f.run.ID); err != nil {
				t.Fatal(err)
			}
			tasks, err := f.queries.ListTasksByIssue(ctx, f.run.IssueID)
			if err != nil {
				t.Fatal(err)
			}
			if len(tasks) != 1 {
				t.Fatalf("recovery produced %d tasks, want one", len(tasks))
			}
			runTaskWithBudget(t, f.queries, tasks[0].ID, 1)
			if _, err := f.taskSvc.FailTask(ctx, tasks[0].ID, "recovered dispatch failed", "", "", "", "agent_error", false, "", ""); err != nil {
				t.Fatal(err)
			}
			requireRunStatus(t, f, "failed")
		})
	}
}

// TestAutopilotCreateIssuePreUpgradeRunObservesTaskFailure: an active run
// dispatched before task_id was recorded still fails from its dispatch task.
// (MUL-8200 third review)
func TestAutopilotCreateIssuePreUpgradeRunObservesTaskFailure(t *testing.T) {
	ctx := context.Background()
	f := dispatchCreateIssueAutopilot(t, "Pre-upgrade running autopilot")
	fixture := dbfx.New(testPool, testWorkspaceID, testUserID)
	fixture.Exec(t, "UPDATE autopilot_run SET task_id = NULL WHERE id = $1", f.run.ID)
	runTaskWithBudget(t, f.queries, f.taskID, 1)
	if _, err := f.taskSvc.FailTask(ctx, f.taskID, "pre-upgrade dispatch failed", "", "", "", "agent_error", false, "", ""); err != nil {
		t.Fatal(err)
	}
	requireRunStatus(t, f, "failed")
}

// TestAutopilotCreateIssueDeliveryWithoutDispatchTask: with no dispatch task
// yet, in_review waits while the issue is agent-assigned (enqueue pending) and
// completes the run once the issue belongs to a member. (MUL-8200)
func TestAutopilotCreateIssueDeliveryWithoutDispatchTask(t *testing.T) {
	f := dispatchCreateIssueAutopilot(t, "Create-issue delivery without dispatch task")
	fixture := dbfx.New(testPool, testWorkspaceID, testUserID)
	fixture.Exec(t, "UPDATE autopilot_run SET task_id = NULL WHERE id = $1", f.run.ID)
	fixture.Exec(t, "DELETE FROM agent_task_queue WHERE id = $1", f.taskID)

	setLinkedIssueStatus(t, f, "in_review")
	requireRunStatus(t, f, "issue_created")

	fixture.Exec(t, "UPDATE issue SET assignee_type = 'member', assignee_id = $2 WHERE id = $1", f.run.IssueID, parseUUID(testUserID))
	setLinkedIssueStatus(t, f, "done")
	requireRunStatus(t, f, "completed")
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

	run, err := autopilotSvc.DispatchAutopilot(ctx, ap, seedAutopilotScheduleTrigger(t, queries, ap), "schedule", nil)
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

	run, err := autopilotSvc.DispatchAutopilot(ctx, ap, seedAutopilotScheduleTrigger(t, queries, ap), "schedule", nil)
	if err != nil {
		t.Fatalf("DispatchAutopilot: %v", err)
	}
	if run == nil {
		t.Fatal("expected a run, got nil")
	}
	if run.Status != "issue_created" {
		t.Fatalf("expected run status 'issue_created', got %q", run.Status)
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

	run, err := autopilotSvc.DispatchAutopilot(ctx, ap, seedAutopilotScheduleTrigger(t, queries, ap), "manual", nil)
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
