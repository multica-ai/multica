package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// statusSyncFixture seeds a workspace, a member-owned agent with a runtime, a
// squad led by that agent, and returns the TaskService wired to the shared
// pool plus the ids (workspace, user, agent, squad, runtime).
func statusSyncFixture(t *testing.T) (*TaskService, *pgxpool.Pool, string, string, string, string, string) {
	t.Helper()
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	var userID, workspaceID, runtimeID, agentID, squadID string
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('Sync User', $1) RETURNING id`,
		fmt.Sprintf("s1-%d@multica.test", suffix)).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID) })

	if err := pool.QueryRow(ctx, `INSERT INTO workspace (name, slug) VALUES ('sync ws', $1) RETURNING id`,
		fmt.Sprintf("sync-%d", suffix)).Scan(&workspaceID); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID) })

	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`,
		workspaceID, userID); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	if err := issuestatus.Ensure(ctx, db.New(pool), util.MustParseUUID(workspaceID)); err != nil {
		t.Fatalf("seed statuses: %v", err)
	}

	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info, metadata, owner_id)
		VALUES ($1, 'sync-runtime', 'cloud', 'codex', 'online', '', '{}'::jsonb, $2)
		RETURNING id`, workspaceID, userID).Scan(&runtimeID); err != nil {
		t.Fatalf("seed runtime: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_config, runtime_id, visibility,
			max_concurrent_tasks, owner_id, instructions, custom_env, custom_args)
		VALUES ($1, 'sync-agent', 'cloud', '{}'::jsonb, $2, 'workspace', 1, $3, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id`, workspaceID, runtimeID, userID).Scan(&agentID); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID) })

	if err := pool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'pge-core-squad', '', $2, $3)
		RETURNING id`, workspaceID, agentID, userID).Scan(&squadID); err != nil {
		t.Fatalf("seed squad: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID) })

	q := db.New(pool)
	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	return svc, pool, workspaceID, userID, agentID, squadID, runtimeID
}

// statusSyncIssue inserts a fresh issue with the given status and returns its id.
func statusSyncIssue(t *testing.T, pool *pgxpool.Pool, workspaceID, userID, agentID, status string) string {
	t.Helper()
	ctx := context.Background()
	var id string
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_type, creator_id, assignee_type, assignee_id, status)
		VALUES ($1, 'sync issue', 'member', $2, 'agent', $3, $4)
		RETURNING id`, workspaceID, userID, agentID, status).Scan(&id); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, id) })
	return id
}

// dispatchTask flips a queued task to dispatched (the daemon claim step) so
// StartTask can transition it to running.
func dispatchTask(t *testing.T, pool *pgxpool.Pool, taskID pgtype.UUID) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		UPDATE agent_task_queue
		SET status = 'dispatched', dispatched_at = now(), prepare_lease_expires_at = NULL
		WHERE id = $1`, taskID); err != nil {
		t.Fatalf("dispatch task: %v", err)
	}
}

// enqueueAndStartTask enqueues a task for the issue, dispatches it, and
// transitions it to running via the real StartTask path, returning the task id.
func enqueueAndStartTask(t *testing.T, svc *TaskService, pool *pgxpool.Pool, workspaceID, agentID, issueID string) string {
	t.Helper()
	ctx := context.Background()
	issue := db.Issue{
		ID:           util.MustParseUUID(issueID),
		WorkspaceID:  util.MustParseUUID(workspaceID),
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
		AssigneeID:   util.MustParseUUID(agentID),
	}
	task, err := svc.EnqueueTaskForIssue(ctx, issue)
	if err != nil {
		t.Fatalf("enqueue task: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, task.ID) })
	dispatchTask(t, pool, task.ID)
	started, err := svc.StartTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("start task: %v", err)
	}
	return started.ID.String()
}

// issueStatus returns the current status of an issue.
func issueStatus(t *testing.T, svc *TaskService, issueID string) string {
	t.Helper()
	issue, err := svc.Queries.GetIssue(context.Background(), util.MustParseUUID(issueID))
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}
	return issue.Status
}

// TestRunStartSyncsIssueToInProgress verifies that starting a run on an
// unstarted issue promotes it to in_progress (RIC-1024 status sync).
func TestRunStartSyncsIssueToInProgress(t *testing.T) {
	svc, pool, workspaceID, userID, agentID, _, _ := statusSyncFixture(t)
	issueID := statusSyncIssue(t, pool, workspaceID, userID, agentID, "todo")

	enqueueAndStartTask(t, svc, pool, workspaceID, agentID, issueID)

	if got := issueStatus(t, svc, issueID); got != "in_progress" {
		t.Fatalf("after start: issue status = %q, want in_progress", got)
	}
}

// TestRunStartPullsBackPrematureInReview verifies that a run starting while an
// issue sits in in_review pulls it back to in_progress (the "running 时提前
// in_review" inversion RIC-1019).
func TestRunStartPullsBackPrematureInReview(t *testing.T) {
	svc, pool, workspaceID, userID, agentID, _, _ := statusSyncFixture(t)
	issueID := statusSyncIssue(t, pool, workspaceID, userID, agentID, "in_review")

	enqueueAndStartTask(t, svc, pool, workspaceID, agentID, issueID)

	if got := issueStatus(t, svc, issueID); got != "in_progress" {
		t.Fatalf("after start on in_review issue: status = %q, want in_progress", got)
	}
}

// TestRunStartLeavesHumanOwnedStatuses verifies the start sync never touches
// done / cancelled / blocked.
func TestRunStartLeavesHumanOwnedStatuses(t *testing.T) {
	for _, status := range []string{"done", "cancelled", "blocked"} {
		t.Run(status, func(t *testing.T) {
			svc, pool, workspaceID, userID, agentID, _, _ := statusSyncFixture(t)
			issueID := statusSyncIssue(t, pool, workspaceID, userID, agentID, status)
			enqueueAndStartTask(t, svc, pool, workspaceID, agentID, issueID)
			if got := issueStatus(t, svc, issueID); got != status {
				t.Fatalf("after start on %s issue: status = %q, want unchanged", status, got)
			}
		})
	}
}

// completeRunningTask flips a running task to completed via the real
// CompleteTaskWithTransition path.
func completeRunningTask(t *testing.T, svc *TaskService, taskID string) {
	t.Helper()
	_, transitioned, err := svc.CompleteTaskWithTransition(
		context.Background(), util.MustParseUUID(taskID), []byte(`{"output":"done"}`),
		"", "", "", false, "", "",
	)
	if err != nil {
		t.Fatalf("complete task: %v", err)
	}
	if !transitioned {
		t.Fatalf("expected transitioned=true from completion CAS")
	}
}

// TestRunCompletionSyncsIssueToInReview verifies a completed direct delivery
// promotes an in_progress issue to in_review (completed + awaits acceptance).
func TestRunCompletionSyncsIssueToInReview(t *testing.T) {
	svc, pool, workspaceID, userID, agentID, _, _ := statusSyncFixture(t)
	issueID := statusSyncIssue(t, pool, workspaceID, userID, agentID, "in_progress")
	taskID := enqueueAndStartTask(t, svc, pool, workspaceID, agentID, issueID)

	completeRunningTask(t, svc, taskID)

	if got := issueStatus(t, svc, issueID); got != "in_review" {
		t.Fatalf("after completion: issue status = %q, want in_review", got)
	}
}

// TestRunCompletionNeverSetsDone verifies a completed delivery lands on
// in_review, never on done (done stays human).
func TestRunCompletionNeverSetsDone(t *testing.T) {
	svc, pool, workspaceID, userID, agentID, _, _ := statusSyncFixture(t)
	issueID := statusSyncIssue(t, pool, workspaceID, userID, agentID, "todo")
	taskID := enqueueAndStartTask(t, svc, pool, workspaceID, agentID, issueID)

	completeRunningTask(t, svc, taskID)

	if got := issueStatus(t, svc, issueID); got != "in_review" {
		t.Fatalf("after completion from todo: issue status = %q, want in_review", got)
	}
}

// TestRunCompletionLeavesDoneAndBlocked verifies the completion sync never
// overwrites done / cancelled / blocked.
func TestRunCompletionLeavesDoneAndBlocked(t *testing.T) {
	for _, status := range []string{"done", "cancelled", "blocked"} {
		t.Run(status, func(t *testing.T) {
			svc, pool, workspaceID, userID, agentID, _, _ := statusSyncFixture(t)
			issueID := statusSyncIssue(t, pool, workspaceID, userID, agentID, status)
			taskID := enqueueAndStartTask(t, svc, pool, workspaceID, agentID, issueID)
			completeRunningTask(t, svc, taskID)
			if got := issueStatus(t, svc, issueID); got != status {
				t.Fatalf("after completion on %s issue: status = %q, want unchanged", status, got)
			}
		})
	}
}

// TestSquadLeaderDispatchCompletionKeepsInProgress verifies a squad leader's
// dispatch turn (is_leader_task=true) does NOT move the parent to in_review —
// the leader keeps it in_progress while members work.
func TestSquadLeaderDispatchCompletionKeepsInProgress(t *testing.T) {
	svc, pool, workspaceID, userID, agentID, squadID, _ := statusSyncFixture(t)
	issueID := statusSyncIssue(t, pool, workspaceID, userID, agentID, "in_progress")

	// Enqueue a squad-leader task (is_leader_task=true) directly.
	issue := db.Issue{
		ID:           util.MustParseUUID(issueID),
		WorkspaceID:  util.MustParseUUID(workspaceID),
		AssigneeType: pgtype.Text{String: "squad", Valid: true},
		AssigneeID:   util.MustParseUUID(squadID),
	}
	ctx := context.Background()
	task, err := svc.EnqueueTaskForSquadLeader(ctx, issue, util.MustParseUUID(agentID), util.MustParseUUID(squadID), pgtype.UUID{}, OriginDerived)
	if err != nil {
		t.Fatalf("enqueue squad leader task: %v", err)
	}
	t.Cleanup(func() { pool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, task.ID) })

	dispatchTask(t, pool, task.ID)
	if _, err := svc.StartTask(ctx, task.ID); err != nil {
		t.Fatalf("start squad leader task: %v", err)
	}
	completeRunningTask(t, svc, task.ID.String())

	if got := issueStatus(t, svc, issueID); got != "in_progress" {
		t.Fatalf("after squad leader dispatch completion: issue status = %q, want in_progress", got)
	}
}
