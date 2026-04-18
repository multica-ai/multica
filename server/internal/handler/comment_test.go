package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	gitlabsync "github.com/multica-ai/multica/server/internal/gitlab"
)

// seedGitlabConnectedIssue inserts an issue cache row in the test workspace
// with the given gitlab_iid + gitlab_project_id and returns its UUID. Used by
// Phase 3c comment write-through tests to wire a comment onto a GitLab-synced
// parent issue.
func seedGitlabConnectedIssue(t *testing.T, iid int, projectID int64) string {
	t.Helper()
	var issueID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (
			workspace_id, title, status, priority, creator_id, creator_type,
			number, position, gitlab_iid, gitlab_project_id, external_updated_at
		)
		VALUES ($1, 'gitlab-synced-issue', 'todo', 'medium', $2, 'member',
			nextval(pg_get_serial_sequence('issue','number')), 0, $3, $4, now())
		RETURNING id
	`, testWorkspaceID, testUserID, iid, projectID).Scan(&issueID); err != nil {
		// Fall back to a plain number — the sequence lookup may not be valid
		// in this test DB. Use a deterministic fake number instead.
		if err2 := testPool.QueryRow(context.Background(), `
			INSERT INTO issue (
				workspace_id, title, status, priority, creator_id, creator_type,
				number, position, gitlab_iid, gitlab_project_id, external_updated_at
			)
			VALUES ($1, 'gitlab-synced-issue', 'todo', 'medium', $2, 'member',
				$3, 0, $3, $4, now())
			RETURNING id
		`, testWorkspaceID, testUserID, iid, projectID).Scan(&issueID); err2 != nil {
			t.Fatalf("seed gitlab-connected issue: %v", err2)
		}
	}
	return issueID
}

// TestCreateComment_WriteThroughHumanSendsNoteWithoutPrefix verifies that on
// a GitLab-connected workspace, a human-authored comment is POSTed to
// GitLab's notes endpoint with the content unchanged (no agent prefix) and
// that the returned note is upserted into the cache keyed by gitlab_note_id.
func TestCreateComment_WriteThroughHumanSendsNoteWithoutPrefix(t *testing.T) {
	ctx := context.Background()

	var capturedMethod, capturedPath, capturedBody string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		capturedBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":8801,"body":"hello","author":{"id":9},"created_at":"2026-04-17T12:00:00Z","updated_at":"2026-04-17T12:00:00Z"}`))
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	defer h.Queries.DeleteWorkspaceGitlabConnection(ctx, parseUUID(testWorkspaceID))

	seedGitlabWriteThroughFixture(t, h)

	issueID := seedGitlabConnectedIssue(t, 500, 42)
	defer testPool.Exec(ctx, `DELETE FROM comment WHERE issue_id = $1`, issueID)
	defer testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)

	body, _ := json.Marshal(map[string]any{"content": "hello"})
	req := httptest.NewRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req = withURLParam(req, "id", issueID)
	rec := httptest.NewRecorder()

	h.CreateComment(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if capturedMethod != http.MethodPost {
		t.Errorf("method = %s", capturedMethod)
	}
	if capturedPath != "/api/v4/projects/42/issues/500/notes" {
		t.Errorf("path = %s", capturedPath)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(capturedBody), &parsed); err != nil {
		t.Fatalf("parse captured body: %v (raw=%q)", err, capturedBody)
	}
	if parsed["body"] != "hello" {
		t.Errorf("GitLab body = %v, want %q (no prefix)", parsed["body"], "hello")
	}

	var count int
	_ = testPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM comment WHERE issue_id = $1 AND gitlab_note_id = 8801`,
		issueID).Scan(&count)
	if count != 1 {
		t.Errorf("cache row not present, count = %d", count)
	}
}

// TestCreateComment_WriteThroughAgentPrefixesBody verifies that an
// agent-authored comment (X-Agent-ID header) on a GitLab-connected workspace
// sends the note body with the canonical "**[agent:<slug>]** " prefix so the
// webhook round-trip parses the authorship back out.
func TestCreateComment_WriteThroughAgentPrefixesBody(t *testing.T) {
	ctx := context.Background()

	// Look up the seeded Handler Test Agent. Its slug is lowercased name with
	// hyphens — "handler-test-agent".
	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, "Handler Test Agent",
	).Scan(&agentID); err != nil {
		t.Fatalf("look up test agent: %v", err)
	}

	var capturedBody string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		capturedBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":8802,"body":"**[agent:handler-test-agent]** go","author":{"id":9},"created_at":"2026-04-17T12:00:00Z","updated_at":"2026-04-17T12:00:00Z"}`))
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	defer h.Queries.DeleteWorkspaceGitlabConnection(ctx, parseUUID(testWorkspaceID))

	seedGitlabWriteThroughFixture(t, h)

	issueID := seedGitlabConnectedIssue(t, 501, 42)
	defer testPool.Exec(ctx, `DELETE FROM comment WHERE issue_id = $1`, issueID)
	defer testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)

	body, _ := json.Marshal(map[string]any{"content": "go"})
	req := httptest.NewRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req.Header.Set("X-Agent-ID", agentID)
	req = withURLParam(req, "id", issueID)
	rec := httptest.NewRecorder()

	h.CreateComment(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(capturedBody), &parsed); err != nil {
		t.Fatalf("parse captured body: %v (raw=%q)", err, capturedBody)
	}
	got, _ := parsed["body"].(string)
	want := "**[agent:handler-test-agent]** go"
	if got != want {
		t.Errorf("GitLab body = %q, want %q", got, want)
	}

	// Cache row must be present; the stored content must be the stripped body
	// (TranslateNote strips the prefix before upsert).
	var gotContent string
	if err := testPool.QueryRow(ctx,
		`SELECT content FROM comment WHERE issue_id = $1 AND gitlab_note_id = 8802`,
		issueID).Scan(&gotContent); err != nil {
		t.Fatalf("query cache row: %v", err)
	}
	if gotContent != "go" {
		t.Errorf("cache content = %q, want %q", gotContent, "go")
	}
}

// TestCreateComment_WriteThroughGitLabErrorReturns502 verifies that a
// GitLab error on the write-through branch aborts the request and does NOT
// leave an orphaned cache row behind.
func TestCreateComment_WriteThroughGitLabErrorReturns502(t *testing.T) {
	ctx := context.Background()

	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"forbidden"}`))
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	defer h.Queries.DeleteWorkspaceGitlabConnection(ctx, parseUUID(testWorkspaceID))

	// We still need the resolver wired so the handler actually attempts the
	// GitLab call — seed the fixture with the service PAT.
	encrypted, _ := h.Secrets.Encrypt([]byte("svc-token-xyz"))
	testPool.Exec(ctx, `
		INSERT INTO workspace_gitlab_connection (
			workspace_id, gitlab_project_id, gitlab_project_path,
			service_token_encrypted, service_token_user_id, connection_status
		) VALUES ($1, 42, 'g/a', $2, 1, 'connected')
		ON CONFLICT (workspace_id) DO UPDATE SET
			gitlab_project_id = EXCLUDED.gitlab_project_id,
			service_token_encrypted = EXCLUDED.service_token_encrypted,
			service_token_user_id = EXCLUDED.service_token_user_id
	`, testWorkspaceID, encrypted)
	h.SetGitlabResolver(gitlabsync.NewResolver(h.Queries, func(_ context.Context, b []byte) (string, error) {
		plain, err := h.Secrets.Decrypt(b)
		if err != nil {
			return "", err
		}
		return string(plain), nil
	}))

	issueID := seedGitlabConnectedIssue(t, 502, 42)
	defer testPool.Exec(ctx, `DELETE FROM comment WHERE issue_id = $1`, issueID)
	defer testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)

	body, _ := json.Marshal(map[string]any{"content": "please"})
	req := httptest.NewRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req = withURLParam(req, "id", issueID)
	rec := httptest.NewRecorder()

	h.CreateComment(rec, req)

	if rec.Code < 400 {
		t.Fatalf("expected >=400, got %d, body=%s", rec.Code, rec.Body.String())
	}
	if rec.Code != http.StatusBadGateway {
		// Allow the handler to use a different 5xx status if the error class is
		// server-side, but 502 is the canonical choice for upstream failures.
		if !strings.Contains(rec.Body.String(), "gitlab") {
			t.Errorf("status = %d, want 502 (body=%s)", rec.Code, rec.Body.String())
		}
	}

	var count int
	_ = testPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM comment WHERE issue_id = $1`,
		issueID).Scan(&count)
	if count != 0 {
		t.Errorf("expected 0 comment rows, got %d (write-through must not leave orphan cache rows)", count)
	}
}

// TestUpdateComment_WriteThroughSendsPUT verifies that on a GitLab-connected
// workspace, editing a comment PUTs to GitLab's notes endpoint with the new
// body, and the cache row is updated from the returned representation.
func TestUpdateComment_WriteThroughSendsPUT(t *testing.T) {
	ctx := context.Background()

	var capturedMethod, capturedPath string
	var capturedBody map[string]any
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":8810,"body":"edited","author":{"id":9},"created_at":"2026-04-17T12:00:00Z","updated_at":"2026-04-17T13:00:00Z"}`))
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	defer h.Queries.DeleteWorkspaceGitlabConnection(ctx, parseUUID(testWorkspaceID))

	seedGitlabWriteThroughFixture(t, h)

	issueID := seedGitlabConnectedIssue(t, 510, 42)
	defer testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)

	// Seed an existing human-authored comment with a known gitlab_note_id.
	commentID := uuid.New().String()
	if _, err := testPool.Exec(ctx,
		`INSERT INTO comment (id, workspace_id, issue_id, author_type, author_id, content, type,
			gitlab_note_id, external_updated_at)
		 VALUES ($1, $2, $3, 'member', $4, 'original', 'comment', 8810, '2026-04-17T12:00:00Z')`,
		commentID, parseUUID(testWorkspaceID), parseUUID(issueID), parseUUID(testUserID)); err != nil {
		t.Fatalf("seed comment: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM comment WHERE id = $1`, commentID)
	})

	body, _ := json.Marshal(map[string]any{"content": "edited"})
	req := httptest.NewRequest(http.MethodPut, "/api/comments/"+commentID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req = withURLParam(req, "commentId", commentID)
	rec := httptest.NewRecorder()

	h.UpdateComment(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if capturedMethod != http.MethodPut {
		t.Errorf("method = %s, want PUT", capturedMethod)
	}
	if capturedPath != "/api/v4/projects/42/issues/510/notes/8810" {
		t.Errorf("path = %s", capturedPath)
	}
	if got, _ := capturedBody["body"].(string); got != "edited" {
		t.Errorf("GitLab body = %v, want %q", capturedBody["body"], "edited")
	}

	var cachedContent string
	if err := testPool.QueryRow(ctx,
		`SELECT content FROM comment WHERE id = $1`, commentID,
	).Scan(&cachedContent); err != nil {
		t.Fatalf("read cache row: %v", err)
	}
	if cachedContent != "edited" {
		t.Errorf("cached content = %q, want %q", cachedContent, "edited")
	}
}

// TestUpdateComment_WriteThroughPreservesAgentPrefix verifies that editing an
// agent-authored comment PUTs the body with the canonical
// "**[agent:<slug>]** " prefix preserved — derived from the cache row's
// existing author_type/author_id, not the request actor.
func TestUpdateComment_WriteThroughPreservesAgentPrefix(t *testing.T) {
	ctx := context.Background()

	// Look up the Handler Test Agent. Slug = "handler-test-agent".
	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, "Handler Test Agent",
	).Scan(&agentID); err != nil {
		t.Fatalf("look up test agent: %v", err)
	}

	var capturedBody map[string]any
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		// Echo the prefixed body back so TranslateNote strips it before the
		// cache upsert — matching the webhook-replay shape.
		_, _ = w.Write([]byte(`{"id":8811,"body":"**[agent:handler-test-agent]** updated","author":{"id":9},"created_at":"2026-04-17T12:00:00Z","updated_at":"2026-04-17T13:00:00Z"}`))
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	defer h.Queries.DeleteWorkspaceGitlabConnection(ctx, parseUUID(testWorkspaceID))

	seedGitlabWriteThroughFixture(t, h)

	issueID := seedGitlabConnectedIssue(t, 511, 42)
	defer testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)

	// Seed an existing agent-authored comment.
	commentID := uuid.New().String()
	if _, err := testPool.Exec(ctx,
		`INSERT INTO comment (id, workspace_id, issue_id, author_type, author_id, content, type,
			gitlab_note_id, external_updated_at)
		 VALUES ($1, $2, $3, 'agent', $4, 'original', 'comment', 8811, '2026-04-17T12:00:00Z')`,
		commentID, parseUUID(testWorkspaceID), parseUUID(issueID), parseUUID(agentID)); err != nil {
		t.Fatalf("seed comment: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM comment WHERE id = $1`, commentID)
	})

	// Edit as the same agent (X-Agent-ID header) so the auth check passes.
	body, _ := json.Marshal(map[string]any{"content": "updated"})
	req := httptest.NewRequest(http.MethodPut, "/api/comments/"+commentID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req.Header.Set("X-Agent-ID", agentID)
	req = withURLParam(req, "commentId", commentID)
	rec := httptest.NewRecorder()

	h.UpdateComment(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	gotBody, _ := capturedBody["body"].(string)
	want := "**[agent:handler-test-agent]** updated"
	if gotBody != want {
		t.Errorf("GitLab body = %q, want %q", gotBody, want)
	}

	// Cache row's content must be the stripped body (TranslateNote strips
	// the prefix before the upsert).
	var cachedContent string
	if err := testPool.QueryRow(ctx,
		`SELECT content FROM comment WHERE id = $1`, commentID,
	).Scan(&cachedContent); err != nil {
		t.Fatalf("read cache row: %v", err)
	}
	if cachedContent != "updated" {
		t.Errorf("cached content = %q, want %q", cachedContent, "updated")
	}
}

// TestUpdateComment_WriteThroughGitLabErrorReturns502 verifies that when
// GitLab returns an error on PUT notes, the handler returns a non-2xx status
// and the cache row content is NOT mutated — no divergence between GitLab
// and Multica on write failure.
func TestUpdateComment_WriteThroughGitLabErrorReturns502(t *testing.T) {
	ctx := context.Background()

	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"forbidden"}`))
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	defer h.Queries.DeleteWorkspaceGitlabConnection(ctx, parseUUID(testWorkspaceID))

	seedGitlabWriteThroughFixture(t, h)

	issueID := seedGitlabConnectedIssue(t, 512, 42)
	defer testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)

	commentID := uuid.New().String()
	if _, err := testPool.Exec(ctx,
		`INSERT INTO comment (id, workspace_id, issue_id, author_type, author_id, content, type,
			gitlab_note_id, external_updated_at)
		 VALUES ($1, $2, $3, 'member', $4, 'original', 'comment', 8812, '2026-04-17T12:00:00Z')`,
		commentID, parseUUID(testWorkspaceID), parseUUID(issueID), parseUUID(testUserID)); err != nil {
		t.Fatalf("seed comment: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM comment WHERE id = $1`, commentID)
	})

	body, _ := json.Marshal(map[string]any{"content": "edited"})
	req := httptest.NewRequest(http.MethodPut, "/api/comments/"+commentID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req = withURLParam(req, "commentId", commentID)
	rec := httptest.NewRecorder()

	h.UpdateComment(rec, req)

	if rec.Code < 400 {
		t.Fatalf("expected >=400, got %d, body=%s", rec.Code, rec.Body.String())
	}
	if rec.Code != http.StatusBadGateway {
		if !strings.Contains(rec.Body.String(), "gitlab") {
			t.Errorf("status = %d, want 502 (body=%s)", rec.Code, rec.Body.String())
		}
	}

	// Cache row must NOT have been mutated — content still "original".
	var cachedContent string
	if err := testPool.QueryRow(ctx,
		`SELECT content FROM comment WHERE id = $1`, commentID,
	).Scan(&cachedContent); err != nil {
		t.Fatalf("read cache row: %v", err)
	}
	if cachedContent != "original" {
		t.Errorf("cached content = %q, want %q (write-through must not mutate cache on GitLab error)", cachedContent, "original")
	}
}

// TestDeleteComment_WriteThroughSendsDELETE verifies that on a GitLab-connected
// workspace, deleting a comment DELETEs GitLab's notes endpoint first and
// then tears down the Multica cache row.
func TestDeleteComment_WriteThroughSendsDELETE(t *testing.T) {
	ctx := context.Background()

	var capturedMethod, capturedPath string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	t.Cleanup(func() {
		h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))
	})

	seedGitlabWriteThroughFixture(t, h)

	issueID := seedGitlabConnectedIssue(t, 520, 42)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	commentID := uuid.New().String()
	if _, err := testPool.Exec(ctx,
		`INSERT INTO comment (id, workspace_id, issue_id, author_type, author_id, content, type,
			gitlab_note_id, external_updated_at)
		 VALUES ($1, $2, $3, 'member', $4, 'x', 'comment', 8820, '2026-04-17T12:00:00Z')`,
		commentID, parseUUID(testWorkspaceID), parseUUID(issueID), parseUUID(testUserID)); err != nil {
		t.Fatalf("seed comment: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM comment WHERE id = $1`, commentID)
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/comments/"+commentID, nil)
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req = withURLParam(req, "commentId", commentID)
	rec := httptest.NewRecorder()

	h.DeleteComment(rec, req)

	if rec.Code != http.StatusNoContent && rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if capturedMethod != http.MethodDelete {
		t.Errorf("method = %s, want DELETE", capturedMethod)
	}
	if capturedPath != "/api/v4/projects/42/issues/520/notes/8820" {
		t.Errorf("path = %s, want /api/v4/projects/42/issues/520/notes/8820", capturedPath)
	}

	var count int
	if err := testPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM comment WHERE id = $1`, commentID,
	).Scan(&count); err != nil {
		t.Fatalf("count cache: %v", err)
	}
	if count != 0 {
		t.Errorf("cache row not deleted, count = %d", count)
	}
}

// TestDeleteComment_WriteThroughGitLab404IsIdempotent verifies that when
// GitLab returns 404 on DELETE notes (note already gone), Client.DeleteNote
// swallows the 404 and the handler proceeds to clean up the cache row —
// matching the idempotent behaviour of Client.DeleteIssue.
func TestDeleteComment_WriteThroughGitLab404IsIdempotent(t *testing.T) {
	ctx := context.Background()

	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"404 Not Found"}`))
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	t.Cleanup(func() {
		h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))
	})

	seedGitlabWriteThroughFixture(t, h)

	issueID := seedGitlabConnectedIssue(t, 521, 42)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	commentID := uuid.New().String()
	if _, err := testPool.Exec(ctx,
		`INSERT INTO comment (id, workspace_id, issue_id, author_type, author_id, content, type,
			gitlab_note_id, external_updated_at)
		 VALUES ($1, $2, $3, 'member', $4, 'x', 'comment', 8821, '2026-04-17T12:00:00Z')`,
		commentID, parseUUID(testWorkspaceID), parseUUID(issueID), parseUUID(testUserID)); err != nil {
		t.Fatalf("seed comment: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM comment WHERE id = $1`, commentID)
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/comments/"+commentID, nil)
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req = withURLParam(req, "commentId", commentID)
	rec := httptest.NewRecorder()

	h.DeleteComment(rec, req)

	if rec.Code != http.StatusNoContent && rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 204/200 (GitLab 404 is idempotent), body = %s",
			rec.Code, rec.Body.String())
	}

	var count int
	if err := testPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM comment WHERE id = $1`, commentID,
	).Scan(&count); err != nil {
		t.Fatalf("count cache: %v", err)
	}
	if count != 0 {
		t.Errorf("cache row not cleaned up on 404, count = %d", count)
	}
}

// TestDeleteComment_WriteThroughGitLab403PreservesCache verifies the
// authoritative guarantee: on non-404 GitLab failure the handler returns a
// non-2xx status AND the cache row remains intact (no fallback to legacy
// direct-DB delete).
func TestDeleteComment_WriteThroughGitLab403PreservesCache(t *testing.T) {
	ctx := context.Background()

	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"forbidden"}`))
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	t.Cleanup(func() {
		h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))
	})

	seedGitlabWriteThroughFixture(t, h)

	issueID := seedGitlabConnectedIssue(t, 522, 42)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	commentID := uuid.New().String()
	if _, err := testPool.Exec(ctx,
		`INSERT INTO comment (id, workspace_id, issue_id, author_type, author_id, content, type,
			gitlab_note_id, external_updated_at)
		 VALUES ($1, $2, $3, 'member', $4, 'original', 'comment', 8822, '2026-04-17T12:00:00Z')`,
		commentID, parseUUID(testWorkspaceID), parseUUID(issueID), parseUUID(testUserID)); err != nil {
		t.Fatalf("seed comment: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM comment WHERE id = $1`, commentID)
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/comments/"+commentID, nil)
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req = withURLParam(req, "commentId", commentID)
	rec := httptest.NewRecorder()

	h.DeleteComment(rec, req)

	if rec.Code < 400 {
		t.Fatalf("status = %d, want >=400, body = %s", rec.Code, rec.Body.String())
	}

	// Cache row must NOT have been deleted on GitLab error.
	var count int
	if err := testPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM comment WHERE id = $1`, commentID,
	).Scan(&count); err != nil {
		t.Fatalf("count cache: %v", err)
	}
	if count != 1 {
		t.Errorf("cache row deleted on GitLab error, count = %d (must be preserved)", count)
	}
}
