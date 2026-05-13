package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func createIssueForLocalRunTest(t *testing.T, status string) IssueResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "Local CLI run test",
		"status": status,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&issue); err != nil {
		t.Fatalf("decode issue: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, issue.ID)
		testPool.Exec(ctx, `DELETE FROM local_cli_run WHERE issue_id = $1`, issue.ID)
		testPool.Exec(ctx, `DELETE FROM comment WHERE issue_id = $1`, issue.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issue.ID)
	})
	return issue
}

func createLocalRunForTest(t *testing.T, issueID string, body map[string]any) map[string]any {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/"+issueID+"/local-runs", body)
	req = withURLParam(req, "id", issueID)
	testHandler.CreateLocalCLIRun(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateLocalCLIRun: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var run map[string]any
	if err := json.NewDecoder(w.Body).Decode(&run); err != nil {
		t.Fatalf("decode run: %v", err)
	}
	return run
}

func withLocalRunWorkspace(req *http.Request) *http.Request {
	return req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, db.Member{}))
}

func TestCreateLocalCLIRun_CreatesThreadAndMarksIssueInProgress(t *testing.T) {
	ctx := context.Background()
	issue := createIssueForLocalRunTest(t, "todo")

	run := createLocalRunForTest(t, issue.ID, map[string]any{
		"cli_name":      "codex",
		"work_dir":      "/tmp/project",
		"comments_mode": "thread",
	})

	if run["kind"] != "local_cli" || run["cli_name"] != "codex" || run["status"] != "running" {
		t.Fatalf("unexpected run response: %+v", run)
	}
	topCommentID, _ := run["top_comment_id"].(string)
	if topCommentID == "" {
		t.Fatalf("top_comment_id missing from run response: %+v", run)
	}

	updated, err := testHandler.Queries.GetIssue(ctx, parseUUID(issue.ID))
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if updated.Status != "in_progress" {
		t.Fatalf("issue status = %q, want in_progress", updated.Status)
	}

	var content, commentType string
	if err := testPool.QueryRow(ctx, `
		SELECT content, type FROM comment WHERE id = $1
	`, topCommentID).Scan(&content, &commentType); err != nil {
		t.Fatalf("load top comment: %v", err)
	}
	if commentType != "system" || !strings.Contains(content, "Started local `codex` run") {
		t.Fatalf("top comment type/content = %q/%q", commentType, content)
	}
}

func TestCreateLocalCLIRun_CommentsOffAndNoStatusUpdate(t *testing.T) {
	ctx := context.Background()
	issue := createIssueForLocalRunTest(t, "todo")

	run := createLocalRunForTest(t, issue.ID, map[string]any{
		"cli_name":         "claude",
		"work_dir":         "/tmp/project",
		"comments_mode":    "off",
		"no_status_update": true,
	})

	if _, ok := run["top_comment_id"]; ok {
		t.Fatalf("comments off run should not have top_comment_id: %+v", run)
	}
	updated, err := testHandler.Queries.GetIssue(ctx, parseUUID(issue.ID))
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if updated.Status != "todo" {
		t.Fatalf("issue status = %q, want todo", updated.Status)
	}
	var count int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM comment WHERE issue_id = $1`, issue.ID).Scan(&count); err != nil {
		t.Fatalf("count comments: %v", err)
	}
	if count != 0 {
		t.Fatalf("comments = %d, want 0", count)
	}
}

func TestCreateLocalCLIRunRejectsInvalidCommentsMode(t *testing.T) {
	issue := createIssueForLocalRunTest(t, "todo")

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/"+issue.ID+"/local-runs", map[string]any{
		"cli_name":      "codex",
		"comments_mode": "loud",
	})
	req = withURLParam(req, "id", issue.ID)
	testHandler.CreateLocalCLIRun(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}

func TestCreateLocalCLIMessage_FinalCreatesLocalDisplayReplyAndRedactedTranscript(t *testing.T) {
	ctx := context.Background()
	issue := createIssueForLocalRunTest(t, "in_progress")
	run := createLocalRunForTest(t, issue.ID, map[string]any{
		"cli_name":      "codex",
		"work_dir":      "/tmp/project",
		"comments_mode": "thread",
	})
	runID := run["id"].(string)
	topCommentID := run["top_comment_id"].(string)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/local-runs/"+runID+"/messages", map[string]any{
		"type":    "final",
		"content": "done OPENAI_API_KEY=sk-proj-abc123def456ghi789jkl012mno345",
	})
	req = withLocalRunWorkspace(req)
	req = withURLParam(req, "runId", runID)
	testHandler.CreateLocalCLIMessage(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateLocalCLIMessage: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var msg map[string]any
	if err := json.NewDecoder(w.Body).Decode(&msg); err != nil {
		t.Fatalf("decode message: %v", err)
	}
	if msg["type"] != "final" || strings.Contains(msg["content"].(string), "sk-proj-abc123") {
		t.Fatalf("message not stored as redacted final: %+v", msg)
	}

	var replyID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM comment WHERE issue_id = $1 AND parent_id = $2
	`, issue.ID, topCommentID).Scan(&replyID); err != nil {
		t.Fatalf("load final reply: %v", err)
	}

	commentsW := httptest.NewRecorder()
	commentsReq := newRequest("GET", "/api/issues/"+issue.ID+"/comments", nil)
	commentsReq = withURLParam(commentsReq, "id", issue.ID)
	testHandler.ListComments(commentsW, commentsReq)
	if commentsW.Code != http.StatusOK {
		t.Fatalf("ListComments: expected 200, got %d: %s", commentsW.Code, commentsW.Body.String())
	}
	var comments []CommentResponse
	if err := json.NewDecoder(commentsW.Body).Decode(&comments); err != nil {
		t.Fatalf("decode comments: %v", err)
	}
	var found *CommentResponse
	for i := range comments {
		if comments[i].ID == replyID {
			found = &comments[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("final reply %s not found in comments: %+v", replyID, comments)
	}
	wantDisplay := handlerTestName + "-local-codex"
	if found.AuthorDisplayName == nil || *found.AuthorDisplayName != wantDisplay {
		t.Fatalf("author_display_name = %v, want %q", found.AuthorDisplayName, wantDisplay)
	}
}

func TestCreateLocalCLIMessage_UserInputCreatesMemberReplyOnly(t *testing.T) {
	issue := createIssueForLocalRunTest(t, "in_progress")
	run := createLocalRunForTest(t, issue.ID, map[string]any{
		"cli_name":      "codex",
		"work_dir":      "/tmp/project",
		"comments_mode": "thread",
	})
	runID := run["id"].(string)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/local-runs/"+runID+"/messages", map[string]any{
		"type":    "user_input",
		"content": "/status",
	})
	req = withLocalRunWorkspace(req)
	req = withURLParam(req, "runId", runID)
	testHandler.CreateLocalCLIMessage(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateLocalCLIMessage: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	commentsW := httptest.NewRecorder()
	commentsReq := newRequest("GET", "/api/issues/"+issue.ID+"/comments", nil)
	commentsReq = withURLParam(commentsReq, "id", issue.ID)
	testHandler.ListComments(commentsW, commentsReq)
	if commentsW.Code != http.StatusOK {
		t.Fatalf("ListComments: expected 200, got %d: %s", commentsW.Code, commentsW.Body.String())
	}
	var comments []CommentResponse
	if err := json.NewDecoder(commentsW.Body).Decode(&comments); err != nil {
		t.Fatalf("decode comments: %v", err)
	}
	for _, c := range comments {
		if c.Content == "/status" {
			if c.AuthorDisplayName != nil {
				t.Fatalf("user input reply should use normal member display, got %q", *c.AuthorDisplayName)
			}
			return
		}
	}
	t.Fatalf("user input reply not found: %+v", comments)
}

func TestCreateLocalCLIMessage_CommentsOffDoesNotCreateReply(t *testing.T) {
	issue := createIssueForLocalRunTest(t, "in_progress")
	run := createLocalRunForTest(t, issue.ID, map[string]any{
		"cli_name":      "codex",
		"comments_mode": "off",
	})
	runID := run["id"].(string)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/local-runs/"+runID+"/messages", map[string]any{
		"type":    "final",
		"content": "done",
	})
	req = withLocalRunWorkspace(req)
	req = withURLParam(req, "runId", runID)
	testHandler.CreateLocalCLIMessage(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateLocalCLIMessage: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var count int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM comment WHERE issue_id = $1`, issue.ID).Scan(&count); err != nil {
		t.Fatalf("count comments: %v", err)
	}
	if count != 0 {
		t.Fatalf("comments = %d, want 0", count)
	}
}

func TestUpdateLocalCLIRunStoresTerminalStatusAndExitCode(t *testing.T) {
	issue := createIssueForLocalRunTest(t, "in_progress")
	run := createLocalRunForTest(t, issue.ID, map[string]any{"cli_name": "codex"})
	runID := run["id"].(string)

	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/local-runs/"+runID, map[string]any{
		"status":    "failed",
		"exit_code": 17,
		"error":     "boom",
	})
	req = withLocalRunWorkspace(req)
	req = withURLParam(req, "runId", runID)
	testHandler.UpdateLocalCLIRun(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateLocalCLIRun: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode run: %v", err)
	}
	if resp["status"] != "failed" || int(resp["exit_code"].(float64)) != 17 || resp["error"] != "boom" {
		t.Fatalf("unexpected update response: %+v", resp)
	}
	if resp["completed_at"] == nil {
		t.Fatalf("completed_at missing for failed run: %+v", resp)
	}
}
