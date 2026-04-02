package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestUpdateRuntimeVisibility verifies toggling visibility between workspace and private.
func TestUpdateRuntimeVisibility(t *testing.T) {
	ctx := context.Background()

	// Create a runtime owned by testUser.
	var runtimeID string
	err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, visibility, last_seen_at
		)
		VALUES ($1, 'update-vis-daemon', 'Update Vis Runtime', 'local', 'claude', 'online',
			'test', '{}'::jsonb, $2, 'workspace', now())
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&runtimeID)
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	// Toggle to private.
	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/runtimes/"+runtimeID, map[string]any{
		"visibility": "private",
	})
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.UpdateAgentRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAgentRuntime to private: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var updated AgentRuntimeResponse
	json.NewDecoder(w.Body).Decode(&updated)
	if updated.Visibility != "private" {
		t.Fatalf("expected visibility 'private', got '%s'", updated.Visibility)
	}

	// Toggle back to workspace.
	w = httptest.NewRecorder()
	req = newRequest("PATCH", "/api/runtimes/"+runtimeID, map[string]any{
		"visibility": "workspace",
	})
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.UpdateAgentRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAgentRuntime to workspace: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	json.NewDecoder(w.Body).Decode(&updated)
	if updated.Visibility != "workspace" {
		t.Fatalf("expected visibility 'workspace', got '%s'", updated.Visibility)
	}
}

// TestUpdateRuntimeVisibilityInvalidValue verifies that an invalid visibility value returns 400.
func TestUpdateRuntimeVisibilityInvalidValue(t *testing.T) {
	ctx := context.Background()

	var runtimeID string
	err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, visibility, last_seen_at
		)
		VALUES ($1, 'invalid-vis-daemon', 'Invalid Vis Runtime', 'local', 'claude', 'online',
			'test', '{}'::jsonb, $2, 'workspace', now())
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&runtimeID)
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/runtimes/"+runtimeID, map[string]any{
		"visibility": "public",
	})
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.UpdateAgentRuntime(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("UpdateAgentRuntime with invalid visibility: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestUpdateRuntimeByNonOwnerFails verifies that a non-owner cannot update a runtime.
func TestUpdateRuntimeByNonOwnerFails(t *testing.T) {
	ctx := context.Background()

	// Create another user.
	var otherUserID string
	err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('update-rt-other', 'update-rt-other@multica.ai')
		RETURNING id
	`).Scan(&otherUserID)
	if err != nil {
		t.Fatalf("create otherUser: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, otherUserID)
	})

	_, err = testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, testWorkspaceID, otherUserID)
	if err != nil {
		t.Fatalf("add otherUser as member: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM member WHERE user_id = $1 AND workspace_id = $2`, otherUserID, testWorkspaceID)
	})

	// Create a runtime owned by testUser.
	var runtimeID string
	err = testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, visibility, last_seen_at
		)
		VALUES ($1, 'non-owner-daemon', 'Non-Owner Runtime', 'local', 'claude', 'online',
			'test', '{}'::jsonb, $2, 'workspace', now())
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&runtimeID)
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	// otherUser tries to update — should get 403.
	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/runtimes/"+runtimeID, map[string]any{
		"visibility": "private",
	})
	req.Header.Set("X-User-ID", otherUserID)
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.UpdateAgentRuntime(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("UpdateAgentRuntime by non-owner: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}
