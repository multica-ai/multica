package handler

import (
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func postComment(t *testing.T, issueID string, body map[string]any) *testutil.Response {
	t.Helper()
	req := testutil.WithURLParams(
		testutil.WithHeaders(
			testutil.JSONRequest("POST", "/api/issues/"+issueID+"/comments", body),
			"X-User-ID", testUserID, "X-Workspace-ID", testWorkspaceID),
		"id", issueID)
	return testutil.Call(t, testHandler.CreateComment, req)
}

func TestCreateCommentIdempotencyKeyReplaysExistingComment(t *testing.T) {
	issueID := dbfx.Issue(t, "Comment idempotency issue")
	dbfx.Cleanup(t, "DELETE FROM comment WHERE issue_id = $1", issueID)

	body := map[string]any{"content": "final report", "client_request_id": "task-1:final-report"}
	first := postComment(t, issueID, body).Want(http.StatusCreated).Map()
	replay := postComment(t, issueID, body).Want(http.StatusOK).Map()

	if first["id"] == "" || first["id"] != replay["id"] {
		t.Fatalf("replay returned a different comment: first=%v replay=%v", first["id"], replay["id"])
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM comment WHERE issue_id = $1", issueID); got != 1 {
		t.Fatalf("comments on issue = %d, want exactly 1 after replay", got)
	}

	// A different key on the same issue creates a second comment normally.
	other := postComment(t, issueID,
		map[string]any{"content": "another", "client_request_id": "task-1:other"}).
		Want(http.StatusCreated).Map()
	if other["id"] == first["id"] {
		t.Fatal("distinct keys must create distinct comments")
	}
}

func TestCreateCommentIdempotencyKeyIsScopedPerIssue(t *testing.T) {
	issueA := dbfx.Issue(t, "Idempotency scope issue A")
	issueB := dbfx.Issue(t, "Idempotency scope issue B")
	dbfx.Cleanup(t, "DELETE FROM comment WHERE issue_id IN ($1, $2)", issueA, issueB)

	a := postComment(t, issueA,
		map[string]any{"content": "on A", "client_request_id": "step-1"}).
		Want(http.StatusCreated).Map()
	b := postComment(t, issueB,
		map[string]any{"content": "on B", "client_request_id": "step-1"}).
		Want(http.StatusCreated).Map()
	if a["id"] == b["id"] {
		t.Fatal("the same key on different issues must create independent comments")
	}
}

func TestCreateCommentIdempotencyKeyTooLongRejected(t *testing.T) {
	issueID := dbfx.Issue(t, "Idempotency length issue")
	postComment(t, issueID, map[string]any{
		"content":           "x",
		"client_request_id": strings.Repeat("k", maxClientRequestIDLength+1),
	}).Want(http.StatusBadRequest)
}

func TestCreateCommentWithoutIdempotencyKeyStillDuplicates(t *testing.T) {
	// Pin the default: no key means no dedup — two identical posts are two
	// comments, exactly as before this feature.
	issueID := dbfx.Issue(t, "No-key duplicate issue")
	dbfx.Cleanup(t, "DELETE FROM comment WHERE issue_id = $1", issueID)

	postComment(t, issueID, map[string]any{"content": "same"}).Want(http.StatusCreated)
	postComment(t, issueID, map[string]any{"content": "same"}).Want(http.StatusCreated)
	if got := dbfx.Count(t, "SELECT count(*) FROM comment WHERE issue_id = $1", issueID); got != 2 {
		t.Fatalf("comments = %d, want 2 (no dedup without a key)", got)
	}
}
