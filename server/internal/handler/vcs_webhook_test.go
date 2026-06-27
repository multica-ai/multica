package handler

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func withVCSBox(t *testing.T) *secretbox.Box {
	t.Helper()
	box, err := secretbox.New(bytes.Repeat([]byte("k"), 32))
	if err != nil {
		t.Fatalf("secretbox.New: %v", err)
	}
	prev := testHandler.VCSSecretBox
	testHandler.VCSSecretBox = box
	t.Cleanup(func() { testHandler.VCSSecretBox = prev })
	return box
}

const vcsTestSecret = "vcs-webhook-secret"

func seedVCSConnection(t *testing.T, ctx context.Context, box *secretbox.Box, provider, instanceURL string) string {
	t.Helper()
	sealed, err := box.Seal([]byte(vcsTestSecret))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	tokenSealed, _ := box.Seal([]byte("tok"))
	conn, err := testHandler.Queries.UpsertVCSConnection(ctx, db.UpsertVCSConnectionParams{
		WorkspaceID:            parseUUID(testWorkspaceID),
		Provider:               provider,
		InstanceUrl:            instanceURL,
		AccountLogin:           "acme",
		AccessTokenEncrypted:   base64.StdEncoding.EncodeToString(tokenSealed),
		WebhookSecretEncrypted: base64.StdEncoding.EncodeToString(sealed),
		ConnectedByID:          pgtype.UUID{},
	})
	if err != nil {
		t.Fatalf("UpsertVCSConnection: %v", err)
	}
	return uuidToString(conn.ID)
}

func cleanupVCS(ctx context.Context, issueID string) {
	testPool.Exec(ctx, `DELETE FROM issue_vcs_pull_request WHERE issue_id = $1`, issueID)
	testPool.Exec(ctx, `DELETE FROM vcs_commit_status cs USING vcs_connection c WHERE cs.connection_id = c.id AND c.workspace_id = $1`, testWorkspaceID)
	testPool.Exec(ctx, `DELETE FROM vcs_pull_request WHERE workspace_id = $1`, testWorkspaceID)
	testPool.Exec(ctx, `DELETE FROM vcs_connection WHERE workspace_id = $1`, testWorkspaceID)
	if issueID != "" {
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	}
}

func newVCSIssue(t *testing.T, title string) IssueResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": title, "status": "in_progress",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)
	return created
}

func vcsWebhookReq(connID string, headers map[string]string, raw []byte) *http.Request {
	req := httptest.NewRequest("POST", "/api/webhooks/vcs/"+connID, bytes.NewReader(raw))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("connectionId", connID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func giteaSig(raw []byte) string {
	mac := hmac.New(sha256.New, []byte(vcsTestSecret))
	mac.Write(raw)
	return hex.EncodeToString(mac.Sum(nil))
}

func TestVCSWebhook_ForgejoMirrorsAndCloses(t *testing.T) {
	ctx := context.Background()
	box := withVCSBox(t)
	connID := seedVCSConnection(t, ctx, box, "forgejo", "https://forgejo.test")
	issue := newVCSIssue(t, "Forgejo PR auto-merge")
	t.Cleanup(func() { cleanupVCS(ctx, issue.ID) })

	raw, _ := json.Marshal(map[string]any{
		"action": "closed",
		"pull_request": map[string]any{
			"number": 7, "html_url": "https://forgejo.test/acme/widget/pulls/7",
			"title": "Fix login " + issue.Identifier, "body": "Closes " + issue.Identifier,
			"state": "closed", "merged": true,
			"merged_at": "2026-04-29T00:00:00Z", "closed_at": "2026-04-29T00:00:00Z",
			"created_at": "2026-04-28T00:00:00Z", "updated_at": "2026-04-29T00:00:00Z",
			"head": map[string]any{"ref": "fix/login", "sha": "abc"},
			"user": map[string]any{"username": "octo"},
		},
		"repository": map[string]any{"name": "widget", "owner": map[string]any{"username": "acme"}},
	})
	w := httptest.NewRecorder()
	testHandler.HandleVCSWebhook(w, vcsWebhookReq(connID, map[string]string{
		"X-Gitea-Event": "pull_request", "X-Gitea-Signature": giteaSig(raw),
	}, raw))
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d (%s)", w.Code, w.Body.String())
	}

	rows, err := testHandler.Queries.ListVCSPullRequestsByIssue(ctx, parseUUID(issue.ID))
	if err != nil {
		t.Fatalf("ListVCSPullRequestsByIssue: %v", err)
	}
	if len(rows) != 1 || rows[0].State != "merged" || rows[0].Provider != "forgejo" {
		t.Fatalf("unexpected rows: %+v", rows)
	}
	updated, _ := testHandler.Queries.GetIssue(ctx, parseUUID(issue.ID))
	if updated.Status != "done" {
		t.Errorf("expected issue done, got %q", updated.Status)
	}
}

func TestVCSWebhook_GitlabMergeRequest(t *testing.T) {
	ctx := context.Background()
	box := withVCSBox(t)
	connID := seedVCSConnection(t, ctx, box, "gitlab", "https://gitlab.test")
	issue := newVCSIssue(t, "GitLab MR test")
	t.Cleanup(func() { cleanupVCS(ctx, issue.ID) })

	raw, _ := json.Marshal(map[string]any{
		"object_kind": "merge_request",
		"user":        map[string]any{"username": "alice"},
		"project":     map[string]any{"path_with_namespace": "acme/widget"},
		"object_attributes": map[string]any{
			"iid": 42, "title": "Add " + issue.Identifier, "description": "Closes " + issue.Identifier,
			"state": "merged", "action": "merge", "source_branch": "feat",
			"url":         "https://gitlab.test/acme/widget/-/merge_requests/42",
			"last_commit": map[string]any{"id": "deadbeef"},
		},
	})
	// GitLab authenticates by plaintext X-Gitlab-Token, not HMAC.
	w := httptest.NewRecorder()
	testHandler.HandleVCSWebhook(w, vcsWebhookReq(connID, map[string]string{
		"X-Gitlab-Event": "Merge Request Hook", "X-Gitlab-Token": vcsTestSecret,
	}, raw))
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d (%s)", w.Code, w.Body.String())
	}

	rows, err := testHandler.Queries.ListVCSPullRequestsByIssue(ctx, parseUUID(issue.ID))
	if err != nil {
		t.Fatalf("ListVCSPullRequestsByIssue: %v", err)
	}
	if len(rows) != 1 || rows[0].Provider != "gitlab" || rows[0].RepoOwner != "acme" || rows[0].PrNumber != 42 {
		t.Fatalf("unexpected rows: %+v", rows)
	}
	if rows[0].State != "merged" {
		t.Errorf("expected merged, got %q", rows[0].State)
	}
	updated, _ := testHandler.Queries.GetIssue(ctx, parseUUID(issue.ID))
	if updated.Status != "done" {
		t.Errorf("expected issue done, got %q", updated.Status)
	}
}

func TestVCSWebhook_CommitStatusMirrors(t *testing.T) {
	ctx := context.Background()
	box := withVCSBox(t)
	connID := seedVCSConnection(t, ctx, box, "forgejo", "https://forgejo.test")
	issue := newVCSIssue(t, "Forgejo CI status")
	t.Cleanup(func() { cleanupVCS(ctx, issue.ID) })

	prRaw, _ := json.Marshal(map[string]any{
		"action": "opened",
		"pull_request": map[string]any{
			"number": 11, "html_url": "https://forgejo.test/acme/widget/pulls/11",
			"title": issue.Identifier + " feature", "state": "open",
			"created_at": "2026-04-28T00:00:00Z", "updated_at": "2026-04-28T00:00:00Z",
			"head": map[string]any{"ref": "feat", "sha": "deadbeef"},
			"user": map[string]any{"username": "octo"},
		},
		"repository": map[string]any{"name": "widget", "owner": map[string]any{"username": "acme"}},
	})
	w := httptest.NewRecorder()
	testHandler.HandleVCSWebhook(w, vcsWebhookReq(connID, map[string]string{
		"X-Gitea-Event": "pull_request", "X-Gitea-Signature": giteaSig(prRaw),
	}, prRaw))
	if w.Code != http.StatusAccepted {
		t.Fatalf("pr: expected 202, got %d", w.Code)
	}

	stRaw, _ := json.Marshal(map[string]any{"sha": "deadbeef", "context": "ci/woodpecker", "state": "success"})
	w = httptest.NewRecorder()
	testHandler.HandleVCSWebhook(w, vcsWebhookReq(connID, map[string]string{
		"X-Gitea-Event": "status", "X-Gitea-Signature": giteaSig(stRaw),
	}, stRaw))
	if w.Code != http.StatusAccepted {
		t.Fatalf("status: expected 202, got %d", w.Code)
	}

	rows, _ := testHandler.Queries.ListVCSPullRequestsByIssue(ctx, parseUUID(issue.ID))
	if len(rows) != 1 || rows[0].ChecksTotal != 1 || rows[0].ChecksPassed != 1 {
		t.Fatalf("expected 1 passed check, got %+v", rows)
	}
}

func TestVCSWebhook_BadSignature(t *testing.T) {
	ctx := context.Background()
	box := withVCSBox(t)
	connID := seedVCSConnection(t, ctx, box, "forgejo", "https://forgejo.test")
	t.Cleanup(func() { cleanupVCS(ctx, "") })

	raw := []byte(`{"action":"opened","pull_request":{"number":1}}`)
	w := httptest.NewRecorder()
	testHandler.HandleVCSWebhook(w, vcsWebhookReq(connID, map[string]string{
		"X-Gitea-Event": "pull_request", "X-Gitea-Signature": "00",
	}, raw))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestVCSWebhook_UnknownConnection(t *testing.T) {
	withVCSBox(t)
	req := vcsWebhookReq("00000000-0000-0000-0000-000000000000", map[string]string{
		"X-Gitea-Event": "pull_request", "X-Gitea-Signature": "00",
	}, []byte(`{}`))
	w := httptest.NewRecorder()
	testHandler.HandleVCSWebhook(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestVCSWebhook_MalformedTolerated(t *testing.T) {
	ctx := context.Background()
	box := withVCSBox(t)
	connID := seedVCSConnection(t, ctx, box, "forgejo", "https://forgejo.test")
	t.Cleanup(func() { cleanupVCS(ctx, "") })

	raw := []byte(`{"action":"opened","pull_request":"not-an-object"}`)
	w := httptest.NewRecorder()
	testHandler.HandleVCSWebhook(w, vcsWebhookReq(connID, map[string]string{
		"X-Gitea-Event": "pull_request", "X-Gitea-Signature": giteaSig(raw),
	}, raw))
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d (%s)", w.Code, w.Body.String())
	}
	var count int
	testPool.QueryRow(ctx, `SELECT count(*) FROM vcs_pull_request WHERE workspace_id = $1`, testWorkspaceID).Scan(&count)
	if count != 0 {
		t.Errorf("expected no PR rows, got %d", count)
	}
}
