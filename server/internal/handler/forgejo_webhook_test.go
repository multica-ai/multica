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

// withForgejoBox installs a deterministic secret box on the shared test
// handler and restores the prior value on cleanup.
func withForgejoBox(t *testing.T) *secretbox.Box {
	t.Helper()
	box, err := secretbox.New(bytes.Repeat([]byte("k"), 32))
	if err != nil {
		t.Fatalf("secretbox.New: %v", err)
	}
	prev := testHandler.ForgejoSecretBox
	testHandler.ForgejoSecretBox = box
	t.Cleanup(func() { testHandler.ForgejoSecretBox = prev })
	return box
}

// seedForgejoConnection inserts a connection whose webhook secret is sealed
// with the given box, returning the connection id and the plaintext secret.
func seedForgejoConnection(t *testing.T, ctx context.Context, box *secretbox.Box) (string, string) {
	t.Helper()
	const secret = "forgejo-webhook-secret"
	sealed, err := box.Seal([]byte(secret))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	tokenSealed, _ := box.Seal([]byte("tok"))
	conn, err := testHandler.Queries.UpsertForgejoConnection(ctx, db.UpsertForgejoConnectionParams{
		WorkspaceID:            parseUUID(testWorkspaceID),
		InstanceUrl:            "https://forgejo.test",
		AccountLogin:           "acme",
		AccessTokenEncrypted:   base64.StdEncoding.EncodeToString(tokenSealed),
		WebhookSecretEncrypted: base64.StdEncoding.EncodeToString(sealed),
		ConnectedByID:          pgtype.UUID{},
	})
	if err != nil {
		t.Fatalf("UpsertForgejoConnection: %v", err)
	}
	return uuidToString(conn.ID), secret
}

func forgejoWebhookRequest(connID, secret string, raw []byte) *http.Request {
	return forgejoWebhookRequestEvent(connID, secret, "pull_request", raw)
}

func forgejoWebhookRequestEvent(connID, secret, event string, raw []byte) *http.Request {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(raw)
	sig := hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest("POST", "/api/webhooks/forgejo/"+connID, bytes.NewReader(raw))
	req.Header.Set("X-Gitea-Event", event)
	req.Header.Set("X-Gitea-Signature", sig)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("connectionId", connID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestHandleForgejoWebhook_MirrorsAndClosesIssue(t *testing.T) {
	ctx := context.Background()
	box := withForgejoBox(t)
	connID, secret := seedForgejoConnection(t, ctx, box)

	// Seed an issue the merged PR should auto-close.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "Forgejo PR auto-merge test",
		"status": "in_progress",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_forgejo_pull_request WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM forgejo_pull_request WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM forgejo_connection WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, created.ID)
	})

	body := map[string]any{
		"action": "closed",
		"number": 7,
		"pull_request": map[string]any{
			"number":     7,
			"html_url":   "https://forgejo.test/acme/widget/pulls/7",
			"title":      "Fix login " + created.Identifier,
			"body":       "Closes " + created.Identifier,
			"state":      "closed",
			"merged":     true,
			"merged_at":  "2026-04-29T00:00:00Z",
			"closed_at":  "2026-04-29T00:00:00Z",
			"created_at": "2026-04-28T00:00:00Z",
			"updated_at": "2026-04-29T00:00:00Z",
			"additions":  10,
			"deletions":  2,
			"head":       map[string]any{"ref": "fix/login", "sha": "abc"},
			"user":       map[string]any{"username": "octo", "avatar_url": ""},
		},
		"repository": map[string]any{
			"name":  "widget",
			"owner": map[string]any{"username": "acme"},
		},
	}
	raw, _ := json.Marshal(body)

	w = httptest.NewRecorder()
	testHandler.HandleForgejoWebhook(w, forgejoWebhookRequest(connID, secret, raw))
	if w.Code != http.StatusAccepted {
		t.Fatalf("webhook: expected 202, got %d (%s)", w.Code, w.Body.String())
	}

	linked, err := testHandler.Queries.ListForgejoPullRequestsByIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("ListForgejoPullRequestsByIssue: %v", err)
	}
	if len(linked) != 1 {
		t.Fatalf("expected 1 linked Forgejo PR, got %d", len(linked))
	}
	if linked[0].State != "merged" {
		t.Errorf("expected pr state merged, got %q", linked[0].State)
	}
	if got := textToPtr(linked[0].AuthorLogin); got == nil || *got != "octo" {
		t.Errorf("expected author octo, got %v", got)
	}

	updated, err := testHandler.Queries.GetIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if updated.Status != "done" {
		t.Errorf("expected issue status 'done', got %q", updated.Status)
	}
}

func TestHandleForgejoWebhook_CommitStatusMirrors(t *testing.T) {
	ctx := context.Background()
	box := withForgejoBox(t)
	connID, secret := seedForgejoConnection(t, ctx, box)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "Forgejo CI status test",
		"status": "in_progress",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_forgejo_pull_request WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM forgejo_commit_status WHERE sha = $1`, "deadbeef")
		testPool.Exec(ctx, `DELETE FROM forgejo_pull_request WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM forgejo_connection WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, created.ID)
	})

	// Open a PR with head sha "deadbeef", linked to the issue.
	prBody, _ := json.Marshal(map[string]any{
		"action": "opened",
		"number": 11,
		"pull_request": map[string]any{
			"number":     11,
			"html_url":   "https://forgejo.test/acme/widget/pulls/11",
			"title":      created.Identifier + " add feature",
			"state":      "open",
			"created_at": "2026-04-28T00:00:00Z",
			"updated_at": "2026-04-28T00:00:00Z",
			"head":       map[string]any{"ref": "feat", "sha": "deadbeef"},
			"user":       map[string]any{"username": "octo"},
		},
		"repository": map[string]any{"name": "widget", "owner": map[string]any{"username": "acme"}},
	})
	w = httptest.NewRecorder()
	testHandler.HandleForgejoWebhook(w, forgejoWebhookRequest(connID, secret, prBody))
	if w.Code != http.StatusAccepted {
		t.Fatalf("pr webhook: expected 202, got %d (%s)", w.Code, w.Body.String())
	}

	// Deliver a successful commit status for that head sha.
	statusBody, _ := json.Marshal(map[string]any{
		"sha":     "deadbeef",
		"context": "ci/woodpecker",
		"state":   "success",
	})
	w = httptest.NewRecorder()
	testHandler.HandleForgejoWebhook(w, forgejoWebhookRequestEvent(connID, secret, "status", statusBody))
	if w.Code != http.StatusAccepted {
		t.Fatalf("status webhook: expected 202, got %d (%s)", w.Code, w.Body.String())
	}

	rows, err := testHandler.Queries.ListForgejoPullRequestsByIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("ListForgejoPullRequestsByIssue: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 PR row, got %d", len(rows))
	}
	if rows[0].ChecksTotal != 1 || rows[0].ChecksPassed != 1 {
		t.Errorf("expected 1 passed check, got total=%d passed=%d failed=%d pending=%d",
			rows[0].ChecksTotal, rows[0].ChecksPassed, rows[0].ChecksFailed, rows[0].ChecksPending)
	}
}

func TestHandleForgejoWebhook_BadSignature(t *testing.T) {
	ctx := context.Background()
	box := withForgejoBox(t)
	connID, _ := seedForgejoConnection(t, ctx, box)
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM forgejo_connection WHERE workspace_id = $1`, testWorkspaceID)
	})

	raw := []byte(`{"action":"opened","pull_request":{"number":1}}`)
	req := forgejoWebhookRequest(connID, "wrong-secret", raw)
	w := httptest.NewRecorder()
	testHandler.HandleForgejoWebhook(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for bad signature, got %d", w.Code)
	}
}

func TestHandleForgejoWebhook_UnknownConnection(t *testing.T) {
	withForgejoBox(t)
	raw := []byte(`{"action":"opened"}`)
	// Random (well-formed) UUID that does not exist.
	missing := "00000000-0000-0000-0000-000000000000"
	req := forgejoWebhookRequest(missing, "x", raw)
	w := httptest.NewRecorder()
	testHandler.HandleForgejoWebhook(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown connection, got %d", w.Code)
	}
}

func TestHandleForgejoWebhook_MalformedPayloadTolerated(t *testing.T) {
	ctx := context.Background()
	box := withForgejoBox(t)
	connID, secret := seedForgejoConnection(t, ctx, box)
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM forgejo_connection WHERE workspace_id = $1`, testWorkspaceID)
	})

	// Valid signature over a body that is not a pull_request object. The
	// handler must ack (202) without panicking or writing a PR row.
	raw := []byte(`{"action":"opened","pull_request":"not-an-object"}`)
	w := httptest.NewRecorder()
	testHandler.HandleForgejoWebhook(w, forgejoWebhookRequest(connID, secret, raw))
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202 for malformed payload, got %d (%s)", w.Code, w.Body.String())
	}

	var count int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM forgejo_pull_request WHERE workspace_id = $1`,
		testWorkspaceID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("expected no PR rows for malformed payload, got %d", count)
	}
}
