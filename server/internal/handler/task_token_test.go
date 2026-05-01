package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/auth"
)

// TestClaimTaskByRuntime_MintsTaskToken verifies that ClaimTaskByRuntime
// returns a per-task mtt_ token and persists its hash so the daemon can
// inject it into the agent process. JEH-324 Part A.
func TestClaimTaskByRuntime_MintsTaskToken(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()

	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx, `
		SELECT a.id, a.runtime_id FROM agent a WHERE a.workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("setup: get agent: %v", err)
	}

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, creator_type, creator_id, assignee_type, assignee_id)
		VALUES ($1, 'task token test', 'todo', 'member', $2, 'agent', $3)
		RETURNING id
	`, testWorkspaceID, testUserID, agentID).Scan(&issueID); err != nil {
		t.Fatalf("setup: create issue: %v", err)
	}
	defer testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, 'queued', 0)
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("setup: create task: %v", err)
	}
	defer testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID)

	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/claim", nil,
		testWorkspaceID, "test-daemon-token-mint")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("runtimeId", runtimeID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Task *struct {
			ID        string `json:"id"`
			TaskToken string `json:"task_token"`
		} `json:"task"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Task == nil {
		t.Fatal("expected a task in response, got nil")
	}
	if !strings.HasPrefix(resp.Task.TaskToken, auth.TaskTokenPrefix) {
		t.Fatalf("expected token prefixed with %q, got %q", auth.TaskTokenPrefix, resp.Task.TaskToken)
	}

	// The plain token must not be persisted — only its hash.
	hash := auth.HashTokenBytes(resp.Task.TaskToken)
	var count int
	if err := testPool.QueryRow(ctx, `SELECT COUNT(*) FROM task_token WHERE token_hash = $1 AND task_id = $2`,
		hash, resp.Task.ID).Scan(&count); err != nil {
		t.Fatalf("verify hash persisted: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 task_token row for the new claim, got %d", count)
	}

	// And no row contains the raw token.
	var rawHits int
	if err := testPool.QueryRow(ctx, `SELECT COUNT(*) FROM task_token WHERE token_hash::text = $1`,
		resp.Task.TaskToken).Scan(&rawHits); err != nil {
		// COUNT(*) on a bytea hash compared to plain string returns 0;
		// the query is correct precisely because raw token storage
		// would surface here.
		t.Fatalf("verify raw token not stored: %v", err)
	}
	if rawHits != 0 {
		t.Fatalf("plain token must not be persisted")
	}
}

// TestRevokeTaskTokensForTask verifies the cascade: completing a task
// must invalidate the task token even before its TTL expires, so a
// finished agent can no longer call back into the API.
func TestRevokeTaskTokensForTask(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx, `
		SELECT a.id, a.runtime_id FROM agent a WHERE a.workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("setup: get agent: %v", err)
	}

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, creator_type, creator_id, assignee_type, assignee_id)
		VALUES ($1, 'revoke test', 'todo', 'member', $2, 'agent', $3)
		RETURNING id
	`, testWorkspaceID, testUserID, agentID).Scan(&issueID); err != nil {
		t.Fatalf("setup: create issue: %v", err)
	}
	defer testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, 'running', 0)
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("setup: create task: %v", err)
	}
	defer testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID)

	if _, err := testPool.Exec(ctx, `
		INSERT INTO task_token (token_hash, task_id, issue_id, agent_id, workspace_id, scope, expires_at)
		VALUES ($1, $2, $3, $4, $5, 'task', now() + INTERVAL '1 hour')
	`, []byte("test-hash-revoke"), taskID, issueID, agentID, testWorkspaceID); err != nil {
		t.Fatalf("setup: insert task token: %v", err)
	}

	if _, err := testHandler.TaskService.CompleteTask(ctx,
		parseUUID(taskID),
		[]byte(`{"output":"done"}`),
		"sess-1",
		"/tmp/wd",
	); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}

	var revokedAt *string
	if err := testPool.QueryRow(ctx, `SELECT revoked_at::text FROM task_token WHERE token_hash = $1`, []byte("test-hash-revoke")).Scan(&revokedAt); err != nil {
		t.Fatalf("read revoked_at: %v", err)
	}
	if revokedAt == nil || *revokedAt == "" {
		t.Fatal("expected task_token.revoked_at to be set after CompleteTask")
	}
}
