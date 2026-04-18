package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestAddReaction_WriteThroughHumanCallsGitLab verifies that a human-authored
// reaction on a GitLab-connected comment POSTs to the GitLab note award_emoji
// endpoint and that the returned award is upserted into the cache keyed by
// gitlab_award_id.
func TestAddReaction_WriteThroughHumanCallsGitLab(t *testing.T) {
	ctx := context.Background()

	var capturedMethod, capturedPath string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":9950,"name":"heart","user":{"id":7},"created_at":"2026-04-17T12:00:00Z","updated_at":"2026-04-17T12:00:00Z"}`))
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	defer h.Queries.DeleteWorkspaceGitlabConnection(ctx, parseUUID(testWorkspaceID))

	seedGitlabWriteThroughFixture(t, h)

	issueID := seedGitlabConnectedIssue(t, 700, 42)
	commentID := uuid.New().String()
	if _, err := testPool.Exec(ctx,
		`INSERT INTO comment (id, workspace_id, issue_id, author_type, author_id, content, gitlab_note_id, external_updated_at)
		 VALUES ($1, $2, $3, 'member', $4, 'x', 8900, '2026-04-17T12:00:00Z')`,
		commentID, testWorkspaceID, issueID, testUserID,
	); err != nil {
		t.Fatalf("seed comment: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM comment_reaction WHERE comment_id = $1`, commentID)
		_, _ = testPool.Exec(ctx, `DELETE FROM comment WHERE id = $1`, commentID)
		_, _ = testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/comments/"+commentID+"/reactions", strings.NewReader(`{"emoji":"heart"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req = withURLParam(req, "commentId", commentID)
	rec := httptest.NewRecorder()

	h.AddReaction(rec, req)

	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if capturedMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", capturedMethod)
	}
	if capturedPath != "/api/v4/projects/42/issues/700/notes/8900/award_emoji" {
		t.Errorf("path = %s", capturedPath)
	}

	var count int
	if err := testPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM comment_reaction WHERE comment_id = $1 AND gitlab_award_id = 9950`,
		commentID).Scan(&count); err != nil {
		t.Fatalf("query cache row: %v", err)
	}
	if count != 1 {
		t.Errorf("cache row missing with gitlab_award_id=9950, count=%d", count)
	}
}

// TestAddReaction_WriteThroughAgentStaysMulticaOnly verifies that an
// agent-authored reaction (X-Agent-ID header) on a GitLab-connected workspace
// does NOT call GitLab and falls through to the legacy Multica-only path.
func TestAddReaction_WriteThroughAgentStaysMulticaOnly(t *testing.T) {
	ctx := context.Background()

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, "Handler Test Agent",
	).Scan(&agentID); err != nil {
		t.Fatalf("look up test agent: %v", err)
	}

	var gitlabCalls int
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gitlabCalls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	defer h.Queries.DeleteWorkspaceGitlabConnection(ctx, parseUUID(testWorkspaceID))

	seedGitlabWriteThroughFixture(t, h)

	issueID := seedGitlabConnectedIssue(t, 701, 42)
	commentID := uuid.New().String()
	if _, err := testPool.Exec(ctx,
		`INSERT INTO comment (id, workspace_id, issue_id, author_type, author_id, content, gitlab_note_id, external_updated_at)
		 VALUES ($1, $2, $3, 'member', $4, 'x', 8901, '2026-04-17T12:00:00Z')`,
		commentID, testWorkspaceID, issueID, testUserID,
	); err != nil {
		t.Fatalf("seed comment: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM comment_reaction WHERE comment_id = $1`, commentID)
		_, _ = testPool.Exec(ctx, `DELETE FROM comment WHERE id = $1`, commentID)
		_, _ = testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/comments/"+commentID+"/reactions", strings.NewReader(`{"emoji":"rocket"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req.Header.Set("X-Agent-ID", agentID)
	req = withURLParam(req, "commentId", commentID)
	rec := httptest.NewRecorder()

	h.AddReaction(rec, req)

	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if gitlabCalls != 0 {
		t.Errorf("GitLab got %d calls — agent reactions must stay Multica-only", gitlabCalls)
	}
	var count int
	if err := testPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM comment_reaction WHERE comment_id = $1 AND actor_type = 'agent' AND emoji = 'rocket'`,
		commentID).Scan(&count); err != nil {
		t.Fatalf("query cache row: %v", err)
	}
	if count != 1 {
		t.Errorf("Multica cache row missing, count=%d", count)
	}
}

// TestAddReaction_WriteThroughGitLabErrorReturns502 verifies that a GitLab
// error aborts the request and does NOT leave an orphaned cache row.
func TestAddReaction_WriteThroughGitLabErrorReturns502(t *testing.T) {
	ctx := context.Background()

	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"forbidden"}`))
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	defer h.Queries.DeleteWorkspaceGitlabConnection(ctx, parseUUID(testWorkspaceID))

	seedGitlabWriteThroughFixture(t, h)

	issueID := seedGitlabConnectedIssue(t, 702, 42)
	commentID := uuid.New().String()
	if _, err := testPool.Exec(ctx,
		`INSERT INTO comment (id, workspace_id, issue_id, author_type, author_id, content, gitlab_note_id, external_updated_at)
		 VALUES ($1, $2, $3, 'member', $4, 'x', 8902, '2026-04-17T12:00:00Z')`,
		commentID, testWorkspaceID, issueID, testUserID,
	); err != nil {
		t.Fatalf("seed comment: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM comment_reaction WHERE comment_id = $1`, commentID)
		_, _ = testPool.Exec(ctx, `DELETE FROM comment WHERE id = $1`, commentID)
		_, _ = testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/comments/"+commentID+"/reactions", strings.NewReader(`{"emoji":"thumbsup"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req = withURLParam(req, "commentId", commentID)
	rec := httptest.NewRecorder()

	h.AddReaction(rec, req)

	if rec.Code < 400 {
		t.Fatalf("status = %d, want >=400", rec.Code)
	}
	var count int
	if err := testPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM comment_reaction WHERE comment_id = $1`,
		commentID).Scan(&count); err != nil {
		t.Fatalf("query cache row count: %v", err)
	}
	if count != 0 {
		t.Errorf("cache leaked on error, count = %d", count)
	}
}
