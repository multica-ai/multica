package main

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// seedDispatchedIssueTask inserts a fresh issue-scoped task for the given agent
// (no trigger comment, so the completion fallback is assignment-style), flips it
// to dispatched then started so started_at is set as the HasAgentCommentedSince
// anchor, and returns its id.
func seedDispatchedIssueTask(t *testing.T, issueID, agentID pgtype.UUID) pgtype.UUID {
	t.Helper()
	ctx := context.Background()
	var runtimeID pgtype.UUID
	if err := testPool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	var taskID pgtype.UUID
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, originator_user_id)
		VALUES ($1, $2, $3, 'queued', 0, $4)
		RETURNING id`,
		agentID, runtimeID, issueID, toPgUUID(t, testUserID),
	).Scan(&taskID); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET status = 'dispatched', dispatched_at = now() WHERE id = $1`, taskID); err != nil {
		t.Fatalf("dispatch task: %v", err)
	}
	if _, err := db.New(testPool).StartAgentTask(ctx, taskID); err != nil {
		t.Fatalf("start task: %v", err)
	}
	return taskID
}

// latestAgentCommentSourceTaskID returns source_task_id (as text, or "" when
// NULL) of the most recent agent-authored comment on the issue.
func latestAgentCommentSourceTaskID(t *testing.T, issueID pgtype.UUID) string {
	t.Helper()
	var sourceTaskID string
	if err := testPool.QueryRow(context.Background(),
		`SELECT COALESCE(source_task_id::text, '') FROM comment
		  WHERE issue_id = $1 AND author_type = 'agent'
		  ORDER BY created_at DESC LIMIT 1`,
		issueID,
	).Scan(&sourceTaskID); err != nil {
		t.Fatalf("read latest agent comment source_task_id: %v", err)
	}
	return sourceTaskID
}

// TestSynthesizedSuccessCommentStampsSourceTaskID pins the U1 fix (R1): when a
// completed issue task synthesizes its success fallback comment from the run's
// final output, that comment must carry source_task_id = task.ID so the
// comment→run lookup resolves. Before the fix, task.go passed pgtype.UUID{}
// here and the most common "agent finished, here's the output" comment was
// untraceable from the UI.
func TestSynthesizedSuccessCommentStampsSourceTaskID(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)

	agentIDStr := getAgentID(t)
	issueIDStr := createIssueAssignedToAgent(t, "source_task_id success stamp test", agentIDStr)
	clearTasks(t, issueIDStr) // drop any assignment-fallback task so we start clean
	agentID := toPgUUID(t, agentIDStr)
	issueID := toPgUUID(t, issueIDStr)

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID)
		clearTasks(t, issueIDStr)
		resp := authRequest(t, "DELETE", "/api/issues/"+issueIDStr, nil)
		resp.Body.Close()
	})

	taskID := seedDispatchedIssueTask(t, issueID, agentID)

	// Non-trivial output, no agent comment during the run → the synthesized
	// success fallback fires and must stamp source_task_id = taskID.
	if _, err := taskSvc.CompleteTask(ctx, taskID, []byte(`{"output":"I fixed the issue by editing the config file"}`), "", ""); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}

	if got, want := latestAgentCommentSourceTaskID(t, issueID), util.UUIDToString(taskID); got != want {
		t.Fatalf("synthesized success comment source_task_id = %q, want %q (task.ID)", got, want)
	}
}

// TestFailureSystemCommentStampsSourceTaskID pins R2 (no regression): the
// per-failure system comment on an issue task that fails without auto-retry
// must carry source_task_id = task.ID. "agent_error" is non-retryable, so
// MaybeRetryFailedTask does not suppress the per-failure comment.
func TestFailureSystemCommentStampsSourceTaskID(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)

	agentIDStr := getAgentID(t)
	issueIDStr := createIssueAssignedToAgent(t, "source_task_id failure stamp test", agentIDStr)
	clearTasks(t, issueIDStr)
	agentID := toPgUUID(t, agentIDStr)
	issueID := toPgUUID(t, issueIDStr)

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID)
		clearTasks(t, issueIDStr)
		resp := authRequest(t, "DELETE", "/api/issues/"+issueIDStr, nil)
		resp.Body.Close()
	})

	taskID := seedDispatchedIssueTask(t, issueID, agentID)

	if _, err := taskSvc.FailTask(ctx, taskID, "agent_error: crashed", "", "", "agent_error"); err != nil {
		t.Fatalf("FailTask: %v", err)
	}

	if got, want := latestAgentCommentSourceTaskID(t, issueID), util.UUIDToString(taskID); got != want {
		t.Fatalf("failure system comment source_task_id = %q, want %q (task.ID)", got, want)
	}
}

