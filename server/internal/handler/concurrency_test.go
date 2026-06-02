package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestGetConcurrencyStats_EmptyWorkspace verifies an empty workspace returns
// zero counts across all fields and an empty agent_details slice (not null).
func TestGetConcurrencyStats_EmptyWorkspace(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	// Create a fresh workspace with no agents or tasks.
	ctx := context.Background()
	var wsID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Concurrency Empty Test', 'concurrency-empty', 'test', 'CET')
		RETURNING id
	`).Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	defer testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, wsID)

	// Add membership so auth passes.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, wsID, testUserID); err != nil {
		t.Fatalf("create member: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+wsID+"/concurrency", nil)
	req.Header.Set("X-User-ID", testUserID)

	// Chi URL params need to be set manually in tests.
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("workspaceId", wsID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	testHandler.GetConcurrencyStats(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp ConcurrencyStatsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if resp.WorkspaceID != wsID {
		t.Errorf("workspace_id: got %q, want %q", resp.WorkspaceID, wsID)
	}
	if resp.ActiveCount != 0 {
		t.Errorf("active_count: got %d, want 0", resp.ActiveCount)
	}
	if resp.QueuedCount != 0 {
		t.Errorf("queued_count: got %d, want 0", resp.QueuedCount)
	}
	if resp.CompletedLastH != 0 {
		t.Errorf("completed_last_hour: got %d, want 0", resp.CompletedLastH)
	}
	if resp.FailedLastH != 0 {
		t.Errorf("failed_last_hour: got %d, want 0", resp.FailedLastH)
	}
	if resp.AgentDetails == nil {
		t.Error("agent_details should be empty slice, not nil")
	}
	if len(resp.AgentDetails) != 0 {
		t.Errorf("agent_details length: got %d, want 0", len(resp.AgentDetails))
	}
}

// TestGetConcurrencyStats_AgentAtCapacity verifies that at_capacity is true
// when running_count >= max_concurrent_tasks.
func TestGetConcurrencyStats_AgentAtCapacity(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()

	// Create workspace + agent with max_concurrent_tasks=1 + a running task.
	var wsID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Concurrency Cap Test', 'concurrency-cap', 'test', 'CCT')
		RETURNING id
	`).Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	defer testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, wsID)

	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, wsID, testUserID); err != nil {
		t.Fatalf("create member: %v", err)
	}

	// Create a runtime.
	var rtID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at)
		VALUES ($1, NULL, 'CapTest RT', 'cloud', 'cap_test', 'online', 'test', '{}'::jsonb, now())
		RETURNING id
	`, wsID).Scan(&rtID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	defer testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = $1`, rtID)

	// Create an agent with max_concurrent_tasks=1.
	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id)
		VALUES ($1, 'CapAgent', '', 'cloud', '{}'::jsonb, $2, 'private', 1, $3)
		RETURNING id
	`, wsID, rtID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	defer testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, agentID)

	// Create a running task for this agent.
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority)
		VALUES ($1, $2, 'running', 0)
		RETURNING id
	`, agentID, rtID).Scan(&taskID); err != nil {
		t.Fatalf("create task: %v", err)
	}
	defer testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID)

	// Hit the endpoint.
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+wsID+"/concurrency", nil)
	req.Header.Set("X-User-ID", testUserID)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("workspaceId", wsID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	testHandler.GetConcurrencyStats(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp ConcurrencyStatsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.ActiveCount != 1 {
		t.Errorf("active_count: got %d, want 1", resp.ActiveCount)
	}
	if len(resp.AgentDetails) != 1 {
		t.Fatalf("agent_details length: got %d, want 1", len(resp.AgentDetails))
	}

	agent := resp.AgentDetails[0]
	if agent.AgentName != "CapAgent" {
		t.Errorf("agent_name: got %q, want %q", agent.AgentName, "CapAgent")
	}
	if agent.MaxConcurrentTasks != 1 {
		t.Errorf("max_concurrent_tasks: got %d, want 1", agent.MaxConcurrentTasks)
	}
	if agent.RunningCount != 1 {
		t.Errorf("running_count: got %d, want 1", agent.RunningCount)
	}
	if !agent.AtCapacity {
		t.Error("at_capacity: got false, want true (1 running >= 1 max)")
	}
}

// TestGetConcurrencyStats_InvalidWorkspaceID verifies a bad UUID returns 400.
func TestGetConcurrencyStats_InvalidWorkspaceID(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler not initialized")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/not-a-uuid/concurrency", nil)
	req.Header.Set("X-User-ID", testUserID)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("workspaceId", "not-a-uuid")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	testHandler.GetConcurrencyStats(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// TestGetConcurrencyStats_NoAuth verifies unauthenticated requests are rejected.
func TestGetConcurrencyStats_NoAuth(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler not initialized")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/concurrency", nil)
	// No X-User-ID header.

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("workspaceId", testWorkspaceID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	testHandler.GetConcurrencyStats(w, req)

	// Should be 401 or 403 (depending on auth middleware).
	if w.Code == http.StatusOK {
		t.Error("expected non-200 for unauthenticated request")
	}
}
