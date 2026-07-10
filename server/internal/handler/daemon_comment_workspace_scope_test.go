package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// claimTriggerCommentContentForTest claims the next task for runtimeID and
// returns the trigger_comment_content the claim response carried (empty when
// the field is absent — it is omitempty on the wire).
func claimTriggerCommentContentForTest(t *testing.T, runtimeID string) (taskID string, triggerContent string, raw string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil,
		testWorkspaceID, "comment-workspace-scope")
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Task *struct {
			ID                    string `json:"id"`
			TriggerCommentContent string `json:"trigger_comment_content"`
		} `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode claim response: %v", err)
	}
	if resp.Task == nil {
		return "", "", w.Body.String()
	}
	return resp.Task.ID, resp.Task.TriggerCommentContent, w.Body.String()
}

func enqueueTriggerCommentTask(t *testing.T, ctx context.Context, agentID, runtimeID, issueID, triggerCommentID string) string {
	t.Helper()
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, trigger_comment_id)
		VALUES ($1, $2, $3, 'queued', 0, $4)
		RETURNING id
	`, agentID, runtimeID, issueID, triggerCommentID).Scan(&taskID); err != nil {
		t.Fatalf("enqueue trigger-comment task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
	return taskID
}

// TestClaimDeliversSameWorkspaceTriggerComment is the positive path: a
// triggering comment in the task's own workspace must still be embedded in the
// claim response. The MUL-4252 workspace-scoped fetch must not regress the
// normal delivery of same-workspace comment content into the agent prompt.
func TestClaimDeliversSameWorkspaceTriggerComment(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "same-ws trigger runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "same-ws trigger agent")

	const body = "same-workspace trigger body ABC123"
	var commentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type)
		VALUES ($1, $2, 'member', $3, $4, 'comment')
		RETURNING id
	`, issueID, testWorkspaceID, testUserID, body).Scan(&commentID); err != nil {
		t.Fatalf("insert same-workspace comment: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM comment WHERE id = $1`, commentID) })

	want := enqueueTriggerCommentTask(t, ctx, agentID, runtimeID, issueID, commentID)
	got, content, raw := claimTriggerCommentContentForTest(t, runtimeID)
	if got != want {
		t.Fatalf("claimed task id = %q, want %q: %s", got, want, raw)
	}
	if content != body {
		t.Fatalf("trigger_comment_content = %q, want the same-workspace body %q", content, body)
	}
}

// TestClaimDoesNotLeakForeignWorkspaceCommentContent is the MUL-4252 guard: if
// a task row's trigger_comment_id points at a comment owned by a DIFFERENT
// workspace, the workspace-scoped fetch must resolve it to "missing" and leave
// the prompt field empty — never leaking another tenant's comment text into
// this agent's prompt.
func TestClaimDoesNotLeakForeignWorkspaceCommentContent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "foreign trigger runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "foreign trigger agent")

	// A comment that lives entirely in a DIFFERENT workspace (its own issue).
	// The task row below (in the runtime's workspace) carries its UUID as the
	// trigger comment; the secret content must never surface on the claim.
	otherWS := createOtherTestWorkspace(t)
	var foreignIssueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position)
		VALUES ($1, 'foreign issue', 'in_progress', 'none', $2, 'member',
			(SELECT COALESCE(MAX(number), 90000) + 1 FROM issue WHERE workspace_id = $1), 0)
		RETURNING id
	`, otherWS, testUserID).Scan(&foreignIssueID); err != nil {
		t.Fatalf("insert foreign-workspace issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, foreignIssueID) })

	const secret = "FOREIGN-WORKSPACE-SECRET-DO-NOT-LEAK"
	var foreignCommentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type)
		VALUES ($1, $2, 'member', $3, $4, 'comment')
		RETURNING id
	`, foreignIssueID, otherWS, testUserID, secret).Scan(&foreignCommentID); err != nil {
		t.Fatalf("insert foreign-workspace comment: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM comment WHERE id = $1`, foreignCommentID) })

	want := enqueueTriggerCommentTask(t, ctx, agentID, runtimeID, issueID, foreignCommentID)
	got, content, raw := claimTriggerCommentContentForTest(t, runtimeID)
	if got != want {
		t.Fatalf("claimed task id = %q, want %q: %s", got, want, raw)
	}
	if content != "" {
		t.Fatalf("trigger_comment_content must be empty for a foreign-workspace comment, got %q", content)
	}
	if strings.Contains(raw, secret) {
		t.Fatalf("claim response leaked foreign-workspace comment text:\n%s", raw)
	}
}
