package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func createIssueWithStatusForCommentTest(t *testing.T, status string) IssueResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":      "Issue comment status test",
		"status":     status,
		"project_id": testProjectID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)

	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, issue.ID)
		testPool.Exec(ctx, `DELETE FROM comment WHERE issue_id = $1`, issue.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issue.ID)
	})

	return issue
}

func TestCreateComment_MarksBlockedIssueInProgress(t *testing.T) {
	ctx := context.Background()
	issue := createIssueWithStatusForCommentTest(t, "blocked")

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/"+issue.ID+"/comments", map[string]any{
		"content": "Please continue this task",
	})
	req = withURLParam(req, "id", issue.ID)
	testHandler.CreateComment(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	updated, err := testHandler.Queries.GetIssue(ctx, parseUUID(issue.ID))
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if updated.Status != "in_progress" {
		t.Fatalf("issue status: got %q, want %q", updated.Status, "in_progress")
	}
}

func TestCreateComment_DoesNotMoveSystemCommentToInProgress(t *testing.T) {
	ctx := context.Background()
	issue := createIssueWithStatusForCommentTest(t, "blocked")

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/"+issue.ID+"/comments", map[string]any{
		"content": "Runtime failed",
		"type":    "system",
	})
	req = withURLParam(req, "id", issue.ID)
	testHandler.CreateComment(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	updated, err := testHandler.Queries.GetIssue(ctx, parseUUID(issue.ID))
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if updated.Status != "blocked" {
		t.Fatalf("issue status: got %q, want %q", updated.Status, "blocked")
	}
}
