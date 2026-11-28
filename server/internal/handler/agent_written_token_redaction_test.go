package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
)

const redactedTokenMarker = "[REDACTED_MULTICA_TOKEN]"

func syntheticMulticaToken(prefix, pair string) string {
	return prefix + strings.Repeat(pair, 20)
}

// A run's final output is stored as the task result and becomes the fallback
// comment, so the token must be gone from both.
func TestCompleteTaskRedactsMulticaTokenInResult(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	token := syntheticMulticaToken("mat_", "a1")
	taskID := seedBatchTask(t, "complete-redaction")
	task, err := testHandler.Queries.GetAgentTask(ctx, util.MustParseUUID(taskID))
	if err != nil {
		t.Fatal(err)
	}
	testutil.Call(t, testHandler.CompleteTask, daemonTaskRequest(t, "/api/daemon/tasks/"+taskID+"/complete", taskID,
		map[string]any{"output": "done " + token})).Want(http.StatusOK)

	completed, err := testHandler.Queries.GetAgentTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(completed.Result), token) || !strings.Contains(string(completed.Result), redactedTokenMarker) {
		t.Fatalf("task result kept the token: %s", completed.Result)
	}
	var body string
	if err := testPool.QueryRow(ctx, "SELECT content FROM comment WHERE issue_id=$1 AND source_task_id=$2", task.IssueID, task.ID).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if body != "done "+redactedTokenMarker {
		t.Fatalf("fallback comment = %q", body)
	}
}

// Agents also post and edit comments directly through the CLI. Those writes
// are filtered; a member's own comment is stored exactly as written.
func TestAgentCommentWritesRedactMulticaTokens(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	taskID := seedBatchTask(t, "comment-redaction")
	task, err := testHandler.Queries.GetAgentTask(ctx, util.MustParseUUID(taskID))
	if err != nil {
		t.Fatal(err)
	}
	issueID := uuidToString(task.IssueID)
	agentID := uuidToString(task.AgentID)
	asAgent := func(r *http.Request) *http.Request {
		r.Header.Set("X-Agent-ID", agentID)
		r.Header.Set("X-Task-ID", taskID)
		return r
	}

	token := syntheticMulticaToken("mat_", "a1")
	create := asAgent(withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments",
		map[string]any{"content": "posted " + token}), "id", issueID))
	var created CommentResponse
	testutil.Call(t, testHandler.CreateComment, create).Want(http.StatusCreated).JSON(&created)
	if created.AuthorType != "agent" || created.Content != "posted "+redactedTokenMarker {
		t.Fatalf("agent comment: author=%s content=%q", created.AuthorType, created.Content)
	}

	edited := syntheticMulticaToken("mul_", "b2")
	edit := asAgent(withURLParam(newRequest(http.MethodPatch, "/api/comments/"+created.ID, map[string]any{
		"content": "edited " + edited, "content_base": created.Content, "expected_revision": created.Revision,
	}), "commentId", created.ID))
	w := httptest.NewRecorder()
	testHandler.UpdateComment(w, edit)
	var updated CommentResponse
	if w.Code != http.StatusOK {
		t.Fatalf("agent edit status=%d body=%s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Content != "edited "+redactedTokenMarker {
		t.Fatalf("agent edit content = %q", updated.Content)
	}

	literal := "my note " + token
	human := withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments",
		map[string]any{"content": literal}), "id", issueID)
	var humanComment CommentResponse
	testutil.Call(t, testHandler.CreateComment, human).Want(http.StatusCreated).JSON(&humanComment)
	if humanComment.AuthorType != "member" || humanComment.Content != literal {
		t.Fatalf("member comment changed: author=%s content=%q", humanComment.AuthorType, humanComment.Content)
	}
	humanEdit := withURLParam(newRequest(http.MethodPatch, "/api/comments/"+humanComment.ID,
		map[string]any{"content": literal + " (edited)"}), "commentId", humanComment.ID)
	var humanEdited CommentResponse
	testutil.Call(t, testHandler.UpdateComment, humanEdit).Want(http.StatusOK).JSON(&humanEdited)
	if humanEdited.Content != literal+" (edited)" {
		t.Fatalf("member edit changed: %q", humanEdited.Content)
	}
}

// The cancel acknowledgement can carry a diagnostic from the daemon; the
// stored error is filtered and the status and reason are untouched.
func TestCancelAckRedactsMulticaTokenInError(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	taskID := seedBatchTask(t, "cancel-ack-redaction")
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'cancelled' WHERE id = $1`, taskID)
	token := syntheticMulticaToken("mcn_", "c3")
	testutil.Call(t, testHandler.AckTaskCancelled, daemonTaskRequest(t, "/api/daemon/tasks/"+taskID+"/cancel-ack", taskID, map[string]any{
		"error_message": "preserved work: " + token, "failure_reason": "worktree_finalize_failed",
	})).Want(http.StatusOK)
	task, err := testHandler.Queries.GetAgentTask(context.Background(), util.MustParseUUID(taskID))
	if err != nil {
		t.Fatal(err)
	}
	if task.Error.String != "preserved work: "+redactedTokenMarker || task.Status != "cancelled" || task.FailureReason.String != "worktree_finalize_failed" {
		t.Fatalf("cancel ack: status=%s reason=%s error=%q", task.Status, task.FailureReason.String, task.Error.String)
	}
}
