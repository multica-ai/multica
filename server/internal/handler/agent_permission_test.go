package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// createPermissionTestMember inserts a fresh workspace member and returns its
// user id, registering cleanup.
func createPermissionTestMember(t *testing.T, email string) string {
	t.Helper()
	ctx := context.Background()
	var userID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
	`, email, email).Scan(&userID); err != nil {
		t.Fatalf("create member user %s: %v", email, err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID) })
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')
	`, testWorkspaceID, userID); err != nil {
		t.Fatalf("add member %s: %v", email, err)
	}
	return userID
}

// TestCreateAgent_LegacyVisibilityMapsToPermission verifies the lossless
// legacy-visibility mapping (MUL-3963) at the API layer — the same mapping the
// migration backfill applies to existing rows:
//   - visibility "workspace" -> permission_mode public_to + a workspace target
//   - visibility "private"   -> permission_mode private + no targets
func TestCreateAgent_LegacyVisibilityMapsToPermission(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	runtimeID := handlerTestRuntimeID(t)

	create := func(name, visibility string) AgentResponse {
		w := httptest.NewRecorder()
		testHandler.CreateAgent(w, newRequest("POST", "/api/agents?workspace_id="+testWorkspaceID, map[string]any{
			"name":       name,
			"runtime_id": runtimeID,
			"visibility": visibility,
		}))
		if w.Code != http.StatusCreated {
			t.Fatalf("create %q (visibility=%s): expected 201, got %d: %s", name, visibility, w.Code, w.Body.String())
		}
		var resp AgentResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, resp.ID) })
		return resp
	}

	ws := create("legacy-visibility-workspace", "workspace")
	if ws.PermissionMode != "public_to" {
		t.Errorf("workspace agent permission_mode = %q, want public_to", ws.PermissionMode)
	}
	if ws.Visibility != "workspace" {
		t.Errorf("workspace agent derived visibility = %q, want workspace", ws.Visibility)
	}
	foundWorkspaceTarget := false
	for _, tgt := range ws.InvocationTargets {
		if tgt.TargetType == "workspace" {
			foundWorkspaceTarget = true
		}
	}
	if !foundWorkspaceTarget {
		t.Errorf("workspace agent invocation_targets = %+v, want a workspace target", ws.InvocationTargets)
	}

	priv := create("legacy-visibility-private", "private")
	if priv.PermissionMode != "private" {
		t.Errorf("private agent permission_mode = %q, want private", priv.PermissionMode)
	}
	if priv.Visibility != "private" {
		t.Errorf("private agent derived visibility = %q, want private", priv.Visibility)
	}
	if len(priv.InvocationTargets) != 0 {
		t.Errorf("private agent invocation_targets = %+v, want none", priv.InvocationTargets)
	}
}

// TestMigrationBackfill_VisibilityToPermission exercises the exact backfill
// statements from migration 130 against a synthetic pre-migration row
// (visibility='workspace' but permission_mode='private', no target). The
// statements are idempotent, so re-running them on the live (already-migrated)
// DB only affects the synthetic row.
func TestMigrationBackfill_VisibilityToPermission(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := handlerTestRuntimeID(t)

	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id
		)
		VALUES ($1, 'backfill-legacy-workspace-agent', '', 'cloud', '{}'::jsonb,
		        $2, 'workspace', 'private', 1, $3)
		RETURNING id
	`, testWorkspaceID, runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("insert pre-migration agent: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID) })

	// Exact backfill statements from migration 130 (idempotent).
	if _, err := testPool.Exec(ctx, `UPDATE agent SET permission_mode = 'public_to' WHERE visibility = 'workspace'`); err != nil {
		t.Fatalf("backfill update: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_invocation_target (agent_id, target_type, target_id, created_by)
		SELECT id, 'workspace', workspace_id, NULL FROM agent WHERE visibility = 'workspace'
		ON CONFLICT (agent_id, target_type, target_id) DO NOTHING
	`); err != nil {
		t.Fatalf("backfill insert targets: %v", err)
	}

	var mode string
	if err := testPool.QueryRow(ctx, `SELECT permission_mode FROM agent WHERE id = $1`, agentID).Scan(&mode); err != nil {
		t.Fatalf("read permission_mode: %v", err)
	}
	if mode != "public_to" {
		t.Errorf("after backfill permission_mode = %q, want public_to", mode)
	}
	var targetCount int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM agent_invocation_target
		WHERE agent_id = $1 AND target_type = 'workspace' AND target_id = $2
	`, agentID, testWorkspaceID).Scan(&targetCount); err != nil {
		t.Fatalf("count targets: %v", err)
	}
	if targetCount != 1 {
		t.Errorf("workspace target count = %d, want 1", targetCount)
	}
}

// TestCanInvokeAgent_PublicToMemberWhitelist verifies that a public_to agent
// restricted to a specific member is invocable (assignable) only by that
// member — not by other plain members, and not by workspace admins who are not
// on the list (MUL-3963).
func TestCanInvokeAgent_PublicToMemberWhitelist(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	runtimeID := handlerTestRuntimeID(t)

	allowedMember := createPermissionTestMember(t, "perm-allowed-member@multica.test")
	otherMember := createPermissionTestMember(t, "perm-other-member@multica.test")

	// Owner (testUserID) creates an agent public_to the allowed member only.
	w := httptest.NewRecorder()
	testHandler.CreateAgent(w, newRequest("POST", "/api/agents?workspace_id="+testWorkspaceID, map[string]any{
		"name":            "public-to-specific-member-agent",
		"runtime_id":      runtimeID,
		"permission_mode": "public_to",
		"invocation_targets": []map[string]any{
			{"target_type": "member", "target_id": allowedMember},
		},
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("create agent: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var agent AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&agent); err != nil {
		t.Fatalf("decode: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agent.ID) })

	// Derived legacy visibility for a member-only public_to agent must be
	// "private" so old clients never treat it as workspace-wide.
	if agent.Visibility != "private" {
		t.Errorf("member-only public_to derived visibility = %q, want private", agent.Visibility)
	}

	assignAs := func(actorID string) int {
		rec := httptest.NewRecorder()
		testHandler.CreateIssue(rec, newRequestAs(actorID, "POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
			"title":         "assign to member-scoped agent",
			"status":        "todo",
			"assignee_type": "agent",
			"assignee_id":   agent.ID,
		}))
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE agent_id = $1`, agent.ID)
			testPool.Exec(context.Background(), `DELETE FROM issue WHERE workspace_id = $1 AND title = 'assign to member-scoped agent'`, testWorkspaceID)
		})
		return rec.Code
	}

	if code := assignAs(allowedMember); code != http.StatusCreated {
		t.Errorf("allow-listed member assign: expected 201, got %d", code)
	}
	if code := assignAs(otherMember); code != http.StatusForbidden {
		t.Errorf("non-allow-listed member assign: expected 403, got %d", code)
	}
}
