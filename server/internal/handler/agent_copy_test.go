package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCopyAgent_SharedWorkspaceAccessAndMatchingRuntime(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	targetWorkspaceID := createOtherTestWorkspace(t)

	var targetSlug string
	if err := testPool.QueryRow(ctx, `SELECT slug FROM workspace WHERE id = $1`, targetWorkspaceID).Scan(&targetSlug); err != nil {
		t.Fatalf("load target workspace slug: %v", err)
	}

	const daemonID = "copy-agent-shared-daemon"
	const provider = "copy_agent_provider"

	var sourceRuntimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, last_seen_at, owner_id, visibility
		)
		VALUES ($1, $2, 'Source Copy Runtime', 'local', $3, 'online', 'copy-agent test', '{}'::jsonb, now(), $4, 'public')
		RETURNING id
	`, testWorkspaceID, daemonID, provider, testUserID).Scan(&sourceRuntimeID); err != nil {
		t.Fatalf("create source runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, sourceRuntimeID)
	})

	var decoyRuntimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, last_seen_at, owner_id, visibility
		)
		VALUES ($1, 'other-daemon', 'Decoy Runtime', 'local', $2, 'online', 'copy-agent test', '{}'::jsonb, now(), $3, 'public')
		RETURNING id
	`, targetWorkspaceID, provider, testUserID).Scan(&decoyRuntimeID); err != nil {
		t.Fatalf("create decoy runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, decoyRuntimeID)
	})

	var matchedRuntimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, last_seen_at, owner_id, visibility
		)
		VALUES ($1, $2, 'Matched Copy Runtime', 'local', $3, 'online', 'copy-agent test', '{}'::jsonb, now(), $4, 'public')
		RETURNING id
	`, targetWorkspaceID, daemonID, provider, testUserID).Scan(&matchedRuntimeID); err != nil {
		t.Fatalf("create matched target runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, matchedRuntimeID)
	})

	sourceAgentID := createAgentOnRuntime(t, "copy-agent-source", sourceRuntimeID, "")
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, sourceAgentID)
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE workspace_id = $1 AND name = 'copy-agent-source'`, targetWorkspaceID)
	})

	body := map[string]any{
		"target_workspace_slug": targetSlug,
	}
	req := withURLParam(newRequest(http.MethodPost, "/api/agents/"+sourceAgentID+"/copy", body), "id", sourceAgentID)
	w := httptest.NewRecorder()
	testHandler.CopyAgent(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CopyAgent: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.PermissionMode != permissionModePublicTo {
		t.Fatalf("permission_mode = %q; want %q", resp.PermissionMode, permissionModePublicTo)
	}
	if resp.Visibility != "workspace" {
		t.Fatalf("visibility = %q; want workspace", resp.Visibility)
	}
	if len(resp.InvocationTargets) != 1 || resp.InvocationTargets[0].TargetType != invocationTargetWorkspace {
		t.Fatalf("invocation_targets = %#v; want one workspace target", resp.InvocationTargets)
	}
	if resp.RuntimeID != matchedRuntimeID {
		t.Fatalf("runtime_id = %q; want matched runtime %q (not decoy %q)", resp.RuntimeID, matchedRuntimeID, decoyRuntimeID)
	}
}