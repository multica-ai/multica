package main

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/auth"
)

// PR writes and the personal GitHub account act as the member on GitHub, so
// an agent's task token must be refused before any of them runs (MUL-8433).
func TestGitHubUserRoutesRejectTaskTokens(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()
	var agentID string
	if err := testPool.QueryRow(ctx, `
		SELECT id::text FROM agent WHERE workspace_id = $1 ORDER BY created_at LIMIT 1
	`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load integration-test agent: %v", err)
	}
	taskID := ensureAgentTask(t, agentID)
	token, err := auth.GenerateAgentTaskToken()
	if err != nil {
		t.Fatalf("generate task token: %v", err)
	}
	var tokenID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO task_token (token_hash, task_id, agent_id, workspace_id, user_id, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id::text
	`, auth.HashToken(token), taskID, agentID, testWorkspaceID, testUserID, time.Now().Add(time.Hour)).Scan(&tokenID); err != nil {
		t.Fatalf("insert task token: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM task_token WHERE id = $1`, tokenID)
	})

	do := func(t *testing.T, method, path string) int {
		t.Helper()
		req, err := http.NewRequest(method, testServer.URL+path, bytes.NewReader([]byte(`{}`)))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	const pr = "/api/issues/00000000-0000-0000-0000-000000000097/pull-requests/00000000-0000-0000-0000-000000000096"
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/me/github"},
		{http.MethodGet, "/api/me/github/connect"},
		{http.MethodPost, "/api/me/github/complete"},
		{http.MethodDelete, "/api/me/github"},
		{http.MethodGet, pr + "/merge-options"},
		{http.MethodPost, pr + "/merge"},
		{http.MethodPut, pr + "/auto-merge"},
		{http.MethodDelete, pr + "/auto-merge"},
		{http.MethodPost, pr + "/update-branch"},
		{http.MethodPost, pr + "/rerun-failed-checks"},
		{http.MethodPost, pr + "/ready-for-review"},
		{http.MethodPost, pr + "/convert-to-draft"},
		{http.MethodPost, pr + "/approve"},
		{http.MethodGet, pr + "/reviewer-candidates"},
		{http.MethodPost, pr + "/request-review"},
		{http.MethodPost, pr + "/close"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			if status := do(t, tc.method, tc.path); status != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", status)
			}
		})
	}

	t.Run("sibling PR list stays outside the human-only guard", func(t *testing.T) {
		if status := do(t, http.MethodGet, "/api/issues/00000000-0000-0000-0000-000000000097/pull-requests"); status != http.StatusNotFound {
			t.Fatalf("status = %d, want the handler's 404 for a missing issue", status)
		}
	})
}

// GitHub's browser redirect carries no Multica credential; the callback must
// be public and always answer with a redirect back to the app.
func TestGitHubUserCallbackIsPublic(t *testing.T) {
	if testServer == nil {
		t.Skip("no test server")
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(testServer.URL + "/api/github/user/callback?code=x&state=y")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound || !strings.Contains(resp.Header.Get("Location"), "github_account_error=") {
		t.Fatalf("status = %d location = %q", resp.StatusCode, resp.Header.Get("Location"))
	}
}
