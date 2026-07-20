package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// newCreateIssueRunFixture builds a create_issue autopilot whose run is linked to
// an agent-authored issue but left UNBOUND (task_id NULL) — the crash-window state
// SyncRunFromCreateIssueTask must repair (MUL-4809 §4.1 item 4). It returns the
// service, the dispatch agent id, the run, and a helper that inserts a task on the
// issue with a chosen agent and created_at offset from issue creation.
func newCreateIssueRunFixture(t *testing.T) (*AutopilotService, string, db.AutopilotRun, func(agent string, offset time.Duration, status string) db.AgentTaskQueue) {
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

	// Issue authored by the dispatch agent — dispatchCreateIssue sets the issue
	// creator to the resolved leader, so the repair attributes the dispatched task
	// by that creator.
	var issueIDStr string
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number, origin_type, origin_id)
		VALUES ($1, 'ci run issue', 'todo', 'none', 'agent', $2,
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

	var issueCreatedAt time.Time
	if err := pool.QueryRow(ctx, `SELECT created_at FROM issue WHERE id = $1`, issueIDStr).Scan(&issueCreatedAt); err != nil {
		t.Fatalf("read issue created_at: %v", err)
	}

	insertTask := func(agent string, offset time.Duration, status string) db.AgentTaskQueue {
		var id string
		if err := pool.QueryRow(context.Background(),
			`INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, created_at) VALUES ($1, $2, $3, $4, 0, $5) RETURNING id`,
			agent, runtimeID, issueIDStr, status, issueCreatedAt.Add(offset)).Scan(&id); err != nil {
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
	return svc, agentID, run, insertTask
}

func isTerminalRunStatus(status string) bool {
	return status == "completed" || status == "failed" || status == "skipped"
}

// TestSyncRunFromCreateIssueTaskRepairsUnboundRunPrecisely covers MUL-4809 §4.1
// item 4: when a run's task_id was never bound (a crash between enqueue and bind),
// finalization must attribute the run to the FIRST dispatched task only. A later
// comment task on the same issue must not finalize the run — instead the repair
// binds the run to the real dispatched task, and only that task finalizes it.
func TestSyncRunFromCreateIssueTaskRepairsUnboundRunPrecisely(t *testing.T) {
	ctx := context.Background()
	svc, agentID, run, insertTask := newCreateIssueRunFixture(t)

	dispatched := insertTask(agentID, 0, "completed")           // the autopilot's own task
	comment := insertTask(agentID, 30*time.Second, "completed") // a later comment task on the same issue

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

// TestSyncRunFromCreateIssueTaskIgnoresTaskOutsideBindWindow covers the crash-window
// guard: when the run's task_id was never bound and the only task on the issue was
// queued long after issue creation (so the real dispatched task never existed — an
// enqueue crash), a stray terminal task must NOT finalize the run.
func TestSyncRunFromCreateIssueTaskIgnoresTaskOutsideBindWindow(t *testing.T) {
	ctx := context.Background()
	svc, agentID, run, insertTask := newCreateIssueRunFixture(t)

	stray := insertTask(agentID, autopilotBindRepairWindow+time.Minute, "completed")

	svc.SyncRunFromCreateIssueTask(ctx, stray)
	after, err := svc.Queries.GetAutopilotRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if isTerminalRunStatus(after.Status) {
		t.Fatalf("run finalized off a task outside the bind window: status=%q", after.Status)
	}
	if after.TaskID.Valid {
		t.Fatalf("run bound to a task outside the bind window: task_id=%x", after.TaskID.Bytes)
	}
}
