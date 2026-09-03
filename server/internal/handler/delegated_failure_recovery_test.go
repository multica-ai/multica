package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUpdateComment_RequeuesDelegatedFailureRecoverySurvivor(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "delegated recovery cancellation runtime")
	coordinatorID, sourceIssueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "delegated recovery coordinator")
	if _, err := testPool.Exec(ctx, `
		UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, sourceIssueID, coordinatorID); err != nil {
		t.Fatalf("assign source issue: %v", err)
	}

	var workerID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, 'delegated recovery worker', '', 'cloud', '{}'::jsonb, $2, 'private', 1, $3)
		RETURNING id`, testWorkspaceID, runtimeID, testUserID).Scan(&workerID); err != nil {
		t.Fatalf("create worker agent: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, workerID) })

	var workerIssueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position)
		VALUES (
			$1, 'delegated recovery worker issue', 'in_progress', 'none', $2, 'member',
			(SELECT COALESCE(MAX(number), 82649) + 1 FROM issue WHERE workspace_id = $1),
			0
		)
		RETURNING id`, testWorkspaceID, testUserID).Scan(&workerIssueID); err != nil {
		t.Fatalf("create worker issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, workerIssueID) })

	var sourceTaskID, failedTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority,
			originator_user_id, accountable_user_id, originator_source
		)
		VALUES ($1, $2, $3, 'completed', 0, $4, $4, 'direct_human')
		RETURNING id`, coordinatorID, runtimeID, sourceIssueID, testUserID).Scan(&sourceTaskID); err != nil {
		t.Fatalf("create source task: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority,
			originator_user_id, accountable_user_id, originator_source,
			delegated_from_task_id, trigger_evidence_kind, failure_reason, error, completed_at
		)
		VALUES ($1, $2, $3, 'failed', 0, $4, $4, 'delegation', $5, 'comment',
		        'agent_error.process_failure', 'worker exited', now())
		RETURNING id`, workerID, runtimeID, workerIssueID, testUserID, sourceTaskID).Scan(&failedTaskID); err != nil {
		t.Fatalf("create failed delegated task: %v", err)
	}

	var humanCommentID, recoveryCommentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at)
		VALUES ($1, $2, 'member', $3, 'original instruction', 'comment', now() - interval '1 second')
		RETURNING id`, sourceIssueID, testWorkspaceID, testUserID).Scan(&humanCommentID); err != nil {
		t.Fatalf("create human comment: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, source_task_id)
		VALUES ($1, $2, 'system', $3, 'resume delegated coordination', 'progress_update', $4)
		RETURNING id`, sourceIssueID, testWorkspaceID, testUserID, failedTaskID).Scan(&recoveryCommentID); err != nil {
		t.Fatalf("create recovery comment: %v", err)
	}

	var cancelledTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, trigger_comment_id,
			coalesced_comment_ids, delivered_comment_ids, originator_user_id,
			accountable_user_id, originator_source
		)
		VALUES ($1, $2, $3, 'queued', 0, $4, ARRAY[$5::uuid], '{}', $6, $6, 'direct_human')
		RETURNING id`, coordinatorID, runtimeID, sourceIssueID, recoveryCommentID, humanCommentID, testUserID).Scan(&cancelledTaskID); err != nil {
		t.Fatalf("create queued coordinator batch: %v", err)
	}
	w := httptest.NewRecorder()
	req := newRequest(http.MethodPut, "/api/comments/"+humanCommentID, map[string]any{
		"content": "edited instruction",
	})
	req = withURLParam(req, "commentId", humanCommentID)
	testHandler.UpdateComment(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateComment: got %d: %s", w.Code, w.Body.String())
	}

	var cancelledStatus string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, cancelledTaskID).Scan(&cancelledStatus); err != nil {
		t.Fatalf("read cancelled task: %v", err)
	}
	if cancelledStatus != "cancelled" {
		t.Fatalf("original batch status = %q, want cancelled", cancelledStatus)
	}
	var activeTasks int
	var recoveryCovered bool
	if err := testPool.QueryRow(ctx, `
		SELECT count(*), COALESCE(bool_or(trigger_comment_id = $3::uuid OR $3::uuid = ANY(coalesced_comment_ids)), false)
		FROM agent_task_queue
		WHERE agent_id = $1 AND issue_id = $2
		  AND status IN ('queued', 'dispatched', 'running', 'waiting_local_directory')`, coordinatorID, sourceIssueID, recoveryCommentID).
		Scan(&activeTasks, &recoveryCovered); err != nil {
		t.Fatalf("read rebuilt coordinator task: %v", err)
	}
	if activeTasks != 1 || !recoveryCovered {
		t.Fatalf("rebuilt active tasks/recovery coverage = %d/%v, want 1/true", activeTasks, recoveryCovered)
	}
}
