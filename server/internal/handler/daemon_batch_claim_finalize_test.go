package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type batchClaimReceiptResponse struct {
	Tasks []struct {
		ID                  string   `json:"id"`
		RuntimeID           string   `json:"runtime_id"`
		AuthToken           string   `json:"auth_token"`
		DeliveredCommentIDs []string `json:"delivered_comment_ids"`
	} `json:"tasks"`
}

// TestClaimTasksByRuntime_MaxTasksZeroClaimsNothing pins the MUL-4257 review
// fix: max_tasks=0 is a valid "no free slots" poll that must claim nothing —
// it must NOT be coerced to 1 (which would dispatch a task the daemon can't run
// and strand it until stale reclaim).
func TestClaimTasksByRuntime_MaxTasksZeroClaimsNothing(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	rt := createClaimReclaimRuntime(t, ctx, "Batch max0 rt")
	a, i := createClaimReclaimAgentAndIssue(t, ctx, rt, "Batch max0 agent")
	taskID := seedQueuedIssueTask(t, ctx, a, rt, i)

	w := postBatchClaim(t, testWorkspaceID, []string{rt}, 0)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp batchClaimReceiptResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Tasks) != 0 {
		t.Fatalf("max_tasks=0 claimed %d tasks, want 0", len(resp.Tasks))
	}
	var status string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if status != "queued" {
		t.Fatalf("task status = %s, want still queued", status)
	}
}

// TestClaimTasksByRuntime_MaxTasksNegativeIsBadRequest pins that a negative
// max_tasks is rejected rather than silently coerced.
func TestClaimTasksByRuntime_MaxTasksNegativeIsBadRequest(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	rt := createClaimReclaimRuntime(t, context.Background(), "Batch neg rt")
	w := postBatchClaim(t, testWorkspaceID, []string{rt}, -1)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for negative max_tasks, got %d: %s", w.Code, w.Body.String())
	}
}

// TestClaimTasksByRuntime_SkipsInvalidRuntimeID pins the MUL-4257 review fix:
// a malformed runtime_id must be skipped (non-panicking parse), not turned into
// a 500 — and a valid runtime in the same request is still claimed.
func TestClaimTasksByRuntime_SkipsInvalidRuntimeID(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	rt := createClaimReclaimRuntime(t, ctx, "Batch badid rt")
	a, i := createClaimReclaimAgentAndIssue(t, ctx, rt, "Batch badid agent")
	seedQueuedIssueTask(t, ctx, a, rt, i)

	w := postBatchClaim(t, testWorkspaceID, []string{"not-a-uuid", rt}, 5)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (invalid id skipped, not 500), got %d: %s", w.Code, w.Body.String())
	}
	var resp batchClaimReceiptResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Tasks) != 1 || resp.Tasks[0].RuntimeID != rt {
		t.Fatalf("expected the valid runtime's task to be claimed despite the invalid id, got %+v", resp.Tasks)
	}
}

// seedCommentBackedQueuedTask inserts a queued task triggered by a real comment
// on its issue, returning (taskID, commentID).
func seedCommentBackedQueuedTask(t *testing.T, ctx context.Context, agentID, runtimeID, issueID string) (string, string) {
	t.Helper()
	var commentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content)
		VALUES ($1, $2, 'member', $3, 'please handle this')
		RETURNING id
	`, testWorkspaceID, issueID, testUserID).Scan(&commentID); err != nil {
		t.Fatalf("seed comment: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM comment WHERE id = $1`, commentID) })

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, trigger_comment_id, status, priority)
		VALUES ($1, $2, $3, $4, 'queued', 0)
		RETURNING id
	`, agentID, runtimeID, issueID, commentID).Scan(&taskID); err != nil {
		t.Fatalf("seed comment-backed task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
	return taskID, commentID
}

func assertCommentDelivered(t *testing.T, ctx context.Context, taskID, commentID string) {
	t.Helper()
	var member bool
	if err := testPool.QueryRow(ctx, `
		SELECT $1 = ANY(delivered_comment_ids) FROM agent_task_queue WHERE id = $2
	`, commentID, taskID).Scan(&member); err != nil {
		t.Fatalf("read delivered_comment_ids: %v", err)
	}
	if !member {
		t.Fatalf("trigger comment %s not persisted in task %s delivered_comment_ids", commentID, taskID)
	}
}

// TestClaimTasksByRuntime_PersistsCommentDeliveryReceipt pins the MUL-4257
// must-fix: the batch path routes through FinalizeTaskClaim, so a comment-backed
// task claimed via batch persists the delivered_comment_ids receipt AND returns
// it in the response.
func TestClaimTasksByRuntime_PersistsCommentDeliveryReceipt(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	rt := createClaimReclaimRuntime(t, ctx, "Batch receipt rt")
	a, i := createClaimReclaimAgentAndIssue(t, ctx, rt, "Batch receipt agent")
	taskID, commentID := seedCommentBackedQueuedTask(t, ctx, a, rt, i)

	w := postBatchClaim(t, testWorkspaceID, []string{rt}, 5)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp batchClaimReceiptResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Tasks) != 1 {
		t.Fatalf("claimed %d tasks, want 1: %s", len(resp.Tasks), w.Body.String())
	}
	found := false
	for _, id := range resp.Tasks[0].DeliveredCommentIDs {
		if id == commentID {
			found = true
		}
	}
	if !found {
		t.Fatalf("response delivered_comment_ids %v missing trigger comment %s", resp.Tasks[0].DeliveredCommentIDs, commentID)
	}
	assertCommentDelivered(t, ctx, taskID, commentID)
}

// TestClaimTasksByRuntime_StaleReclaimRecordsDeliveryReceipt pins that a
// comment-backed task recovered via the batch reclaim path (dispatched, never
// started, past the recovery window) is re-finalized so its delivery receipt is
// recorded on the replacement claim.
func TestClaimTasksByRuntime_StaleReclaimRecordsDeliveryReceipt(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	rt := createClaimReclaimRuntime(t, ctx, "Batch stale-receipt rt")
	a, i := createClaimReclaimAgentAndIssue(t, ctx, rt, "Batch stale-receipt agent")

	var commentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content)
		VALUES ($1, $2, 'member', $3, 'stale reclaim comment')
		RETURNING id
	`, testWorkspaceID, i, testUserID).Scan(&commentID); err != nil {
		t.Fatalf("seed comment: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM comment WHERE id = $1`, commentID) })

	// Stale dispatched, comment-backed, never started, past the 90s window.
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, trigger_comment_id, status, priority, dispatched_at, started_at)
		VALUES ($1, $2, $3, $4, 'dispatched', 0, now() - interval '120 seconds', NULL)
		RETURNING id
	`, a, rt, i, commentID).Scan(&taskID); err != nil {
		t.Fatalf("seed stale dispatched comment task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	w := postBatchClaim(t, testWorkspaceID, []string{rt}, 5)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp batchClaimReceiptResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Tasks) != 1 || resp.Tasks[0].ID != taskID {
		t.Fatalf("expected the stale task %s reclaimed, got %+v", taskID, resp.Tasks)
	}
	assertCommentDelivered(t, ctx, taskID, commentID)
}

// TestClaimTasksByRuntime_SkipsRuntimeOwnedByAnotherDaemon pins the MUL-4257
// review must-fix: a daemon must not batch-claim a task routed to a runtime
// bound to a DIFFERENT daemon, even in the same workspace. The runtime is
// skipped and its task stays queued for the owning machine.
func TestClaimTasksByRuntime_SkipsRuntimeOwnedByAnotherDaemon(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// Runtime bound to a different daemon in the same (handler-test) workspace.
	var rt string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at, visibility, owner_id)
		VALUES ($1, 'other-daemon-machine', 'Other daemon RT', 'cloud', 'handler_test_runtime', 'online', 'x', '{}'::jsonb, now(), 'private', $2)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&rt); err != nil {
		t.Fatalf("create other-daemon runtime: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = $1`, rt) })

	a, i := createClaimReclaimAgentAndIssue(t, ctx, rt, "Other daemon agent")
	taskID := seedQueuedIssueTask(t, ctx, a, rt, i)

	// postBatchClaim sends daemon_id = batchClaimTestDaemonID ("batch-claim-review"),
	// which differs from the runtime's "other-daemon-machine".
	w := postBatchClaim(t, testWorkspaceID, []string{rt}, 5)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp batchClaimReceiptResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Tasks) != 0 {
		t.Fatalf("daemon-A claimed %d tasks from a runtime owned by daemon-B, want 0", len(resp.Tasks))
	}
	var status string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if status != "queued" {
		t.Fatalf("task status = %s, want still queued for the owning daemon", status)
	}
}

// TestClaimTasksByRuntime_RequiresDaemonID pins that the batch claim rejects a
// request with no daemon_id rather than falling back to workspace-only scoping.
func TestClaimTasksByRuntime_RequiresDaemonID(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/tasks/claim",
		map[string]any{"runtime_ids": []string{}, "max_tasks": 5},
		testWorkspaceID, batchClaimTestDaemonID)
	testHandler.ClaimTasksByRuntime(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when daemon_id is missing, got %d: %s", w.Code, w.Body.String())
	}
}
