package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// insertCommentForScopeTest inserts a member comment on issueID in the given
// workspace and returns its id.
func insertCommentForScopeTest(t *testing.T, ctx context.Context, issueID, workspaceID, content string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type)
		VALUES ($1, $2, 'member', $3, $4, 'comment')
		RETURNING id
	`, issueID, workspaceID, testUserID, content).Scan(&id); err != nil {
		t.Fatalf("insert comment: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM comment WHERE id = $1`, id) })
	return id
}

// enqueueIssueTaskWithTrigger enqueues a real task for (issueID, agentID) via the
// production TaskService.EnqueueTaskForIssue path — the path that snapshots
// trigger_summary and resolves the originator from the triggering comment — so
// the MUL-4252 workspace scoping is exercised end-to-end rather than bypassed
// with a hand-written row. Returns the created task id.
func enqueueIssueTaskWithTrigger(t *testing.T, ctx context.Context, agentID, issueID, triggerCommentID string) string {
	t.Helper()
	task, err := testHandler.TaskService.EnqueueTaskForIssue(ctx, db.Issue{
		ID:           parseUUID(issueID),
		WorkspaceID:  parseUUID(testWorkspaceID),
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
		AssigneeID:   parseUUID(agentID),
		CreatorType:  "member",
		CreatorID:    parseUUID(testUserID),
		Priority:     "none",
	}, parseUUID(triggerCommentID))
	if err != nil {
		t.Fatalf("EnqueueTaskForIssue: %v", err)
	}
	taskID := uuidToString(task.ID)
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
	return taskID
}

// claimTriggerFieldsForTest claims the next task for runtimeID and returns the
// triggering comment content + summary the claim response carried (both empty
// when absent — omitempty on the wire) plus the raw response body.
func claimTriggerFieldsForTest(t *testing.T, runtimeID string) (taskID, triggerContent, triggerSummary, raw string) {
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
			TriggerSummary        string `json:"trigger_summary"`
		} `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode claim response: %v", err)
	}
	if resp.Task == nil {
		return "", "", "", w.Body.String()
	}
	return resp.Task.ID, resp.Task.TriggerCommentContent, resp.Task.TriggerSummary, w.Body.String()
}

// TestClaimDeliversSameWorkspaceTriggerComment is the positive path: a triggering
// comment in the task's own workspace must still be snapshotted into
// trigger_summary at enqueue and embedded (content + summary) in the claim
// response. The MUL-4252 workspace-scoped lookups must not regress delivery.
func TestClaimDeliversSameWorkspaceTriggerComment(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "same-ws trigger runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "same-ws trigger agent")

	const body = "same-workspace trigger body ABC123"
	commentID := insertCommentForScopeTest(t, ctx, issueID, testWorkspaceID, body)

	want := enqueueIssueTaskWithTrigger(t, ctx, agentID, issueID, commentID)
	got, content, summary, raw := claimTriggerFieldsForTest(t, runtimeID)
	if got != want {
		t.Fatalf("claimed task id = %q, want %q: %s", got, want, raw)
	}
	if content != body {
		t.Fatalf("trigger_comment_content = %q, want same-workspace body %q", content, body)
	}
	if summary != body {
		t.Fatalf("trigger_summary = %q, want same-workspace body %q", summary, body)
	}
}

// TestClaimDoesNotLeakForeignWorkspaceTriggerCommentOrSummary is the MUL-4252
// guard, exercised through the real enqueue + claim path. A task whose
// trigger_comment_id points at a comment owned by a DIFFERENT workspace must
// leak neither the full body (claim-time fetch) nor the truncated summary
// (enqueue-time snapshot), and must not inherit an originator from it.
func TestClaimDoesNotLeakForeignWorkspaceTriggerCommentOrSummary(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "foreign trigger runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "foreign trigger agent")

	// A comment that lives entirely in a DIFFERENT workspace (its own issue).
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
	foreignCommentID := insertCommentForScopeTest(t, ctx, foreignIssueID, otherWS, secret)

	// Enqueue through the production path: this is where trigger_summary is
	// snapshotted and the originator resolved. Both must fail closed.
	taskID := enqueueIssueTaskWithTrigger(t, ctx, agentID, issueID, foreignCommentID)

	// Stored row: neither the summary nor the originator may come from the
	// foreign comment.
	var storedSummary pgtype.Text
	var storedOriginator pgtype.UUID
	if err := testPool.QueryRow(ctx,
		`SELECT trigger_summary, originator_user_id FROM agent_task_queue WHERE id = $1`, taskID,
	).Scan(&storedSummary, &storedOriginator); err != nil {
		t.Fatalf("read stored task: %v", err)
	}
	if storedSummary.Valid {
		t.Fatalf("stored trigger_summary must be NULL for a foreign-workspace comment, got %q", storedSummary.String)
	}
	if storedOriginator.Valid {
		t.Fatalf("stored originator_user_id must be NULL for a foreign-workspace comment, got %s", uuidToString(storedOriginator))
	}

	// Wire: the claim response must carry neither the body nor the summary.
	got, content, summary, raw := claimTriggerFieldsForTest(t, runtimeID)
	if got != taskID {
		t.Fatalf("claimed task id = %q, want %q: %s", got, taskID, raw)
	}
	if content != "" {
		t.Fatalf("trigger_comment_content must be empty for a foreign-workspace comment, got %q", content)
	}
	if summary != "" {
		t.Fatalf("trigger_summary must be empty for a foreign-workspace comment, got %q", summary)
	}
	if strings.Contains(raw, secret) {
		t.Fatalf("claim response leaked foreign-workspace comment text:\n%s", raw)
	}
}
