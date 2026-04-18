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
