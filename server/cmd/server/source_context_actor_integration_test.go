package main

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/auth"
)

func TestSourceContextRoutesRejectAuthoritativeTaskToken(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	ctx := context.Background()
	var agentID string
	if err := testPool.QueryRow(ctx, `
		SELECT id::text
		FROM agent
		WHERE workspace_id = $1
		ORDER BY created_at
		LIMIT 1
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
		if _, err := testPool.Exec(context.Background(), `DELETE FROM task_token WHERE id = $1`, tokenID); err != nil {
			t.Logf("delete task token fixture: %v", err)
		}
	})

	do := func(t *testing.T, method, path string) *http.Response {
		t.Helper()
		var body *bytes.Reader
		if method == http.MethodPost || method == http.MethodPut {
			body = bytes.NewReader([]byte(`{}`))
		} else {
			body = bytes.NewReader(nil)
		}
		req, err := http.NewRequest(method, testServer.URL+path, body)
		if err != nil {
			t.Fatalf("create request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		// Auth must discard both spoofed values and stamp the token's
		// authoritative actor source and workspace before route guards run.
		req.Header.Set("X-Actor-Source", "member")
		req.Header.Set("X-Workspace-ID", "00000000-0000-0000-0000-000000000099")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("perform request: %v", err)
		}
		return resp
	}

	const commentPath = "/api/comments/00000000-0000-0000-0000-000000000098"
	for _, tc := range []struct {
		name   string
		method string
		path   string
	}{
		{name: "preview", method: http.MethodGet, path: commentPath + "/sub-issue-preview"},
		{name: "create", method: http.MethodPost, path: commentPath + "/sub-issues"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := do(t, tc.method, tc.path)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", resp.StatusCode)
			}
		})
	}

	t.Run("sibling comment route remains outside human-only guard", func(t *testing.T) {
		resp := do(t, http.MethodPut, commentPath+"/")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want downstream 404 proving the route-level guard did not wrap sibling routes", resp.StatusCode)
		}
	})
}
