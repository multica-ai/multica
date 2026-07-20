package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// newCreateIssueRunFixture builds a create_issue autopilot whose run is linked to
// an agent-authored issue but left UNBOUND (task_id NULL) — the crash-window state
// SyncRunFromCreateIssueTask must repair (MUL-4809 §4.1). It returns the service,
// the dispatch agent id, the run, the pool, and a helper that inserts a task on the
// issue. Pass a valid dispatchedRunID to stamp the task's dispatched_autopilot_run_id
// (the autopilot's own dispatched task); pass pgtype.UUID{} for an ordinary
// comment/chat task that carries no provenance.
func newCreateIssueRunFixture(t *testing.T) (*AutopilotService, string, db.AutopilotRun, *pgxpool.Pool, func(agent string, offset time.Duration, status string, dispatchedRunID pgtype.UUID) db.AgentTaskQueue) {
	t.Helper()
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	queries := db.New(pool)

	suffix := time.Now().UnixNano()
	var userID, workspaceID, runtimeID, agentID string
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('ci run', $1) RETURNING id`,
		fmt.Sprintf("ci-run-%d@multica.ai", suffix)).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO workspace (name, slug, description, issue_prefix) VALUES ('ci run', $1, '', 'CIR') RETURNING id`,
		fmt.Sprintf("ci-run-%d", suffix)).Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
		pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at, visibility, owner_id)
		VALUES ($1, NULL, 'ci run rt', 'cloud', 'ci_run', 'online', 'rt', '{}'::jsonb, now(), 'private', $2) RETURNING id
	`, workspaceID, userID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id)
		VALUES ($1, 'ci run agent', '', 'cloud', '{}'::jsonb, $2, 'private', 1, $3) RETURNING id
	`, workspaceID, runtimeID, userID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	ap, err := queries.CreateAutopilot(ctx, db.CreateAutopilotParams{
		WorkspaceID:   mustUUID(t, workspaceID),
		Title:         "ci run",
		AssigneeType:  "agent",
		AssigneeID:    mustUUID(t, agentID),
		Status:        "active",
		ExecutionMode: "create_issue",
		CreatedByType: "member",
		CreatedByID:   mustUUID(t, userID),
	})
	if err != nil {
		t.Fatalf("CreateAutopilot: %v", err)
	}

	var issueIDStr string
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id, number, origin_type, origin_id)
		VALUES ($1, 'ci run issue', 'todo', 'none', 'agent', $2, 'agent', $2,
		        COALESCE((SELECT MAX(number) FROM issue WHERE workspace_id = $1), 0) + 1, 'autopilot', $3)
		RETURNING id::text
	`, workspaceID, agentID, util.UUIDToString(ap.ID)).Scan(&issueIDStr); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	issueUUID := mustUUID(t, issueIDStr)

	run, err := queries.CreateAutopilotRun(ctx, db.CreateAutopilotRunParams{
		AutopilotID: ap.ID,
		Source:      "manual",
		Status:      "issue_created",
	})
	if err != nil {
		t.Fatalf("CreateAutopilotRun: %v", err)
	}
	run, err = queries.UpdateAutopilotRunIssueCreated(ctx, db.UpdateAutopilotRunIssueCreatedParams{
		ID:      run.ID,
		IssueID: issueUUID,
	})
	if err != nil {
		t.Fatalf("link run to issue: %v", err)
	}

	insertTask := func(agent string, offset time.Duration, status string, dispatchedRunID pgtype.UUID) db.AgentTaskQueue {
		var id string
		if err := pool.QueryRow(context.Background(),
			`INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, created_at, dispatched_autopilot_run_id)
			 VALUES ($1, $2, $3, $4, 0, now() + $5::interval, $6) RETURNING id`,
			agent, runtimeID, issueIDStr, status, fmt.Sprintf("%d seconds", int(offset.Seconds())), dispatchedRunID).Scan(&id); err != nil {
			t.Fatalf("insert task: %v", err)
		}
		task, err := queries.GetAgentTask(context.Background(), mustUUID(t, id))
		if err != nil {
			t.Fatalf("get task: %v", err)
		}
		return task
	}

	bus := events.New()
	taskSvc := NewTaskService(queries, pool, nil, bus)
	svc := NewAutopilotService(queries, pool, bus, taskSvc)
	// These fixtures exercise the task-driven finalization path; force the gate on.
	svc.FeatureFlags = autopilotTaskDrivenFlags(true)
	return svc, agentID, run, pool, insertTask
}

func isTerminalRunStatus(status string) bool {
	return status == "completed" || status == "failed" || status == "skipped"
}

// TestSyncRunFromCreateIssueTaskRepairsUnboundRunPrecisely covers MUL-4809 §4.1:
// when a run's task_id was never bound (a crash between enqueue and bind),
// finalization must attribute the run to the task carrying its provenance stamp —
// never to an unstamped comment task on the same issue. The comment task finishing
// first must not finalize the run; the repair binds the run to the stamped task,
// and only that task finalizes it.
func TestSyncRunFromCreateIssueTaskRepairsUnboundRunPrecisely(t *testing.T) {
	ctx := context.Background()
	svc, agentID, run, _, insertTask := newCreateIssueRunFixture(t)

	dispatched := insertTask(agentID, 0, "completed", run.ID)                  // the autopilot's own (stamped) task
	comment := insertTask(agentID, 30*time.Second, "completed", pgtype.UUID{}) // a later, unstamped comment task

	// The comment task finishing first must NOT finalize the run, but repair must
	// still bind the run to the real dispatched task.
	svc.SyncRunFromCreateIssueTask(ctx, comment)
	after, err := svc.Queries.GetAutopilotRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if isTerminalRunStatus(after.Status) {
		t.Fatalf("run finalized off a comment task: status=%q", after.Status)
	}
	if !after.TaskID.Valid || after.TaskID.Bytes != dispatched.ID.Bytes {
		t.Fatalf("repair did not bind the dispatched task: task_id valid=%v", after.TaskID.Valid)
	}

	// The dispatched task completing then finalizes the run.
	svc.SyncRunFromCreateIssueTask(ctx, dispatched)
	final, err := svc.Queries.GetAutopilotRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if final.Status != "completed" {
		t.Fatalf("dispatched task did not finalize the run: status=%q", final.Status)
	}
}

// TestSyncRunFromCreateIssueTaskIgnoresUnstampedTask is the precise-provenance
// counter-example Elon asked for (MUL-4809 §4.1 P0-1): when the run's task_id was
// never bound and the ONLY task on the issue is an unstamped comment/assignment
// task queued shortly after issue creation (the real dispatched task never existed
// — e.g. a crash before enqueue), that stray task must NOT finalize the run. A
// time-window heuristic would have bound it; precise provenance refuses to.
func TestSyncRunFromCreateIssueTaskIgnoresUnstampedTask(t *testing.T) {
	ctx := context.Background()
	svc, agentID, run, _, insertTask := newCreateIssueRunFixture(t)

	// Same agent, same issue, seconds after creation — but NOT stamped with the
	// run's provenance, because it isn't the autopilot's dispatched task.
	stray := insertTask(agentID, 10*time.Second, "completed", pgtype.UUID{})

	svc.SyncRunFromCreateIssueTask(ctx, stray)
	after, err := svc.Queries.GetAutopilotRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if isTerminalRunStatus(after.Status) {
		t.Fatalf("run finalized off an unstamped stray task: status=%q", after.Status)
	}
	if after.TaskID.Valid {
		t.Fatalf("run bound to an unstamped stray task: task_id=%x", after.TaskID.Bytes)
	}
}

// TestSyncRunFromCreateIssueTaskCompletesRegardlessOfIssueStatus covers MUL-4809
// §4.1 / §9.2: the run is finalized purely by task outcome. Even when the agent
// leaves the issue in an in_progress-Category status (In Review), the completed
// task must complete the run, and finalizing the run must not touch issue status.
func TestSyncRunFromCreateIssueTaskCompletesRegardlessOfIssueStatus(t *testing.T) {
	ctx := context.Background()
	svc, agentID, run, pool, insertTask := newCreateIssueRunFixture(t)

	if _, err := pool.Exec(ctx, `UPDATE issue SET status = 'in_review' WHERE id = $1`, run.IssueID); err != nil {
		t.Fatalf("set issue in_review: %v", err)
	}

	dispatched := insertTask(agentID, 0, "completed", run.ID)
	svc.SyncRunFromCreateIssueTask(ctx, dispatched)

	got, err := svc.Queries.GetAutopilotRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if got.Status != "completed" {
		t.Fatalf("run did not complete while issue was in_review: status=%q", got.Status)
	}
	var issueStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM issue WHERE id = $1`, run.IssueID).Scan(&issueStatus); err != nil {
		t.Fatalf("read issue status: %v", err)
	}
	if issueStatus != "in_review" {
		t.Fatalf("run finalization changed issue status to %q", issueStatus)
	}
}

// TestSyncRunFromCreateIssueTaskCancelledReasonTraceable covers MUL-4809 §9.2: a
// cancelled dispatched task fails the run with a reason that names the cancellation
// rather than a generic "task failed".
func TestSyncRunFromCreateIssueTaskCancelledReasonTraceable(t *testing.T) {
	ctx := context.Background()
	svc, agentID, run, _, insertTask := newCreateIssueRunFixture(t)

	dispatched := insertTask(agentID, 0, "cancelled", run.ID)
	svc.SyncRunFromCreateIssueTask(ctx, dispatched)

	got, err := svc.Queries.GetAutopilotRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if got.Status != "failed" {
		t.Fatalf("cancelled task did not fail the run: status=%q", got.Status)
	}
	if !got.FailureReason.Valid || !strings.Contains(strings.ToLower(got.FailureReason.String), "cancel") {
		t.Fatalf("cancelled run reason not traceable: %q", got.FailureReason.String)
	}
}

// TestSyncRunFromCreateIssueTaskDoesNotRewriteTerminalRun covers MUL-4809 §9.2:
// once a run has finalized, a later comment task on the same issue must not
// resurrect or rewrite it — GetAutopilotRunByIssue excludes terminal runs, so the
// sync is a no-op.
func TestSyncRunFromCreateIssueTaskDoesNotRewriteTerminalRun(t *testing.T) {
	ctx := context.Background()
	svc, agentID, run, _, insertTask := newCreateIssueRunFixture(t)

	dispatched := insertTask(agentID, 0, "completed", run.ID)
	svc.SyncRunFromCreateIssueTask(ctx, dispatched)
	done, err := svc.Queries.GetAutopilotRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if done.Status != "completed" {
		t.Fatalf("run did not complete: status=%q", done.Status)
	}

	comment := insertTask(agentID, time.Minute, "failed", pgtype.UUID{})
	svc.SyncRunFromCreateIssueTask(ctx, comment)
	after, err := svc.Queries.GetAutopilotRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if after.Status != "completed" {
		t.Fatalf("comment task rewrote a terminal run: status=%q", after.Status)
	}
	if after.TaskID.Bytes != done.TaskID.Bytes {
		t.Fatal("comment task rebound a terminal run's task_id")
	}
}

// TestSyncRunFromCreateIssueTaskRetryKeepsRunRunningUntilFinal covers MUL-4809 §4.1
// item 3 / §9.2: a retryable failure with a system retry already queued must NOT
// fail the run; only the final terminal attempt (no further retry) does — so
// failure-rate auto-pause stays accurate.
func TestSyncRunFromCreateIssueTaskRetryKeepsRunRunningUntilFinal(t *testing.T) {
	ctx := context.Background()
	svc, agentID, run, pool, insertTask := newCreateIssueRunFixture(t)

	// Bind the run to its dispatched task, as dispatchCreateIssue does.
	dispatched := insertTask(agentID, 0, "running", run.ID)
	bound, err := svc.bindAutopilotRunTask(ctx, run.ID, dispatched.ID)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	if bound.Status != "running" {
		t.Fatalf("bind did not move run to running: %q", bound.Status)
	}

	// The dispatched attempt fails, but a system retry is already queued (FailTask
	// enqueues the retry before broadcasting the failure). The run must stay open.
	var retryID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, retry_of_task_id, created_at)
		 VALUES ($1, $2, $3, 'queued', 0, $4, now()) RETURNING id`,
		dispatched.AgentID, dispatched.RuntimeID, dispatched.IssueID, dispatched.ID).Scan(&retryID); err != nil {
		t.Fatalf("insert retry: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status = 'failed' WHERE id = $1`, dispatched.ID); err != nil {
		t.Fatalf("fail dispatched: %v", err)
	}
	dispatched.Status = "failed"
	svc.SyncRunFromCreateIssueTask(ctx, dispatched)
	stillRunning, err := svc.Queries.GetAutopilotRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if stillRunning.Status != "running" {
		t.Fatalf("run finalized while a retry was pending: status=%q", stillRunning.Status)
	}

	// The retry fails with no further attempt queued — now the run fails.
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status = 'failed' WHERE id = $1`, retryID); err != nil {
		t.Fatalf("fail retry: %v", err)
	}
	retryTask, err := svc.Queries.GetAgentTask(ctx, mustUUID(t, retryID))
	if err != nil {
		t.Fatalf("get retry task: %v", err)
	}
	svc.SyncRunFromCreateIssueTask(ctx, retryTask)
	final, err := svc.Queries.GetAutopilotRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if final.Status != "failed" {
		t.Fatalf("run did not fail after the final retry failed: status=%q", final.Status)
	}
}
