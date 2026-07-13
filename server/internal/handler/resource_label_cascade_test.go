package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

// Resource-label junction tables (agent_to_label / skill_to_label) deliberately
// carry no foreign keys, so every bulk hard-delete entry point that removes the
// owning agents/skills must clear their label links in the same transaction.
// These tests pin that cleanup on the four batch paths that never pass through a
// per-entity delete: runtime delete (strict + cascade), runtime-profile delete,
// and workspace delete. Without the sweep, a labelled agent/skill leaves a
// permanent, invisible orphan row once resource labels are enabled.

// insertLabelRow creates a real issue_label so the seeded junction row is valid
// regardless of whether a given database still carries the pre-release label_id
// foreign key. Registers cleanup.
func insertLabelRow(t *testing.T, ctx context.Context, workspaceID, resourceType string) string {
	t.Helper()
	var labelID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue_label (workspace_id, resource_type, name, color)
		VALUES ($1, $2, $3, '#3b82f6')
		RETURNING id
	`, workspaceID, resourceType, resourceType+"-"+uuid.NewString()[:8]).Scan(&labelID); err != nil {
		t.Fatalf("insert issue_label: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_label WHERE id = $1`, labelID)
	})
	return labelID
}

func seedAgentLabel(t *testing.T, ctx context.Context, workspaceID, agentID string) {
	t.Helper()
	labelID := insertLabelRow(t, ctx, workspaceID, "agent")
	if _, err := testPool.Exec(ctx,
		`INSERT INTO agent_to_label (agent_id, label_id) VALUES ($1, $2)`,
		agentID, labelID); err != nil {
		t.Fatalf("seed agent_to_label: %v", err)
	}
}

func seedSkillLabel(t *testing.T, ctx context.Context, workspaceID, skillID string) {
	t.Helper()
	labelID := insertLabelRow(t, ctx, workspaceID, "skill")
	if _, err := testPool.Exec(ctx,
		`INSERT INTO skill_to_label (skill_id, label_id) VALUES ($1, $2)`,
		skillID, labelID); err != nil {
		t.Fatalf("seed skill_to_label: %v", err)
	}
}

func countAgentLabelAssignments(t *testing.T, ctx context.Context, agentID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_to_label WHERE agent_id = $1`, agentID).Scan(&n); err != nil {
		t.Fatalf("count agent_to_label: %v", err)
	}
	return n
}

func countSkillLabelAssignments(t *testing.T, ctx context.Context, skillID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM skill_to_label WHERE skill_id = $1`, skillID).Scan(&n); err != nil {
		t.Fatalf("count skill_to_label: %v", err)
	}
	return n
}

// TestDeleteAgentRuntime_CleansAgentLabelAssignments: the strict runtime delete
// hard-deletes the archived agent bound to the runtime; its label link must go
// with it in the same transaction.
func TestDeleteAgentRuntime_CleansAgentLabelAssignments(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	runtimeID := seedIsolatedRuntime(t, "Label Cleanup Runtime")
	agentID := seedAgentOnRuntime(t, runtimeID, "Label Cleanup Archived Agent", true)
	seedAgentLabel(t, ctx, testWorkspaceID, agentID)

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/runtimes/"+runtimeID, nil)
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.DeleteAgentRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("DeleteAgentRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if agentExists(t, agentID) {
		t.Fatalf("archived agent should have been hard-deleted with its runtime")
	}
	if n := countAgentLabelAssignments(t, ctx, agentID); n != 0 {
		t.Fatalf("agent_to_label rows survived runtime delete: %d (orphaned once resource labels ship)", n)
	}
}

// TestArchiveAgentsAndDeleteRuntime_CleansAgentLabelAssignments: the cascade
// endpoint archives the active agent and hard-deletes it with the runtime, so
// the label link must be swept too.
func TestArchiveAgentsAndDeleteRuntime_CleansAgentLabelAssignments(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	runtimeID := createCascadeFixtureRuntime(t, ctx, "Label Cascade Runtime")
	agentID := createCascadeFixtureAgent(t, ctx, runtimeID, "Label Cascade Agent")
	seedAgentLabel(t, ctx, testWorkspaceID, agentID)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/runtimes/"+runtimeID+"/archive-agents-and-delete",
		map[string]any{"expected_active_agent_ids": []string{agentID}})
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.ArchiveAgentsAndDeleteRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ArchiveAgentsAndDeleteRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if n := countAgentLabelAssignments(t, ctx, agentID); n != 0 {
		t.Fatalf("agent_to_label rows survived cascade runtime delete: %d", n)
	}
}

// TestDeleteRuntimeProfile_CleansAgentLabelAssignments: deleting a runtime
// profile hard-deletes the archived agents on each of its runtimes; their label
// links must go with them.
func TestDeleteRuntimeProfile_CleansAgentLabelAssignments(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	profileID := insertRuntimeProfileFixture(t, ctx, "Label Cleanup Profile", "codex", "company-codex-label")
	runtimeID := insertProfileRuntimeFixture(t, ctx, profileID, "Label Cleanup Profile Runtime", "codex")
	agentID := createCascadeFixtureAgent(t, ctx, runtimeID, "Label Cleanup Profile Agent")
	if _, err := testPool.Exec(ctx, `UPDATE agent SET archived_at = now() WHERE id = $1`, agentID); err != nil {
		t.Fatalf("archive agent: %v", err)
	}
	seedAgentLabel(t, ctx, testWorkspaceID, agentID)

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/workspaces/"+testWorkspaceID+"/runtime-profiles/"+profileID, nil)
	req = withURLParams(req, "id", testWorkspaceID, "profileId", profileID)
	testHandler.DeleteRuntimeProfile(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteRuntimeProfile: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	if agentExists(t, agentID) {
		t.Fatalf("archived agent should have been hard-deleted with its runtime profile")
	}
	if n := countAgentLabelAssignments(t, ctx, agentID); n != 0 {
		t.Fatalf("agent_to_label rows survived runtime-profile delete: %d", n)
	}
}

func seedWorkspaceResourceLabelFixture(t *testing.T, ctx context.Context, slug string) (string, string, string) {
	t.Helper()
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)

	var wsID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description)
		VALUES ($1, $2, $3)
		RETURNING id
	`, "Handler Test Delete Labels", slug, "resource-label atomic cleanup test").Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
	})
	if _, err := testPool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`,
		wsID, testUserID); err != nil {
		t.Fatalf("create owner member: %v", err)
	}

	// agent.runtime_id is NOT NULL, so the labelled agent needs a runtime in the
	// same workspace. Both cascade away with the workspace.
	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, last_seen_at
		)
		VALUES ($1, NULL, 'ws-label-runtime', 'cloud', 'ws-label-test', 'online', 'dev', '{}'::jsonb, $2, now())
		RETURNING id
	`, wsID, testUserID).Scan(&runtimeID); err != nil {
		t.Fatalf("insert runtime: %v", err)
	}
	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, 'ws-label-agent', '', 'cloud', '{}'::jsonb, $2, 'workspace', 1, $3)
		RETURNING id
	`, wsID, runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("insert agent: %v", err)
	}
	var skillID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO skill (workspace_id, name, description, content, config, created_by)
		VALUES ($1, 'ws-label-skill', 'fixture', '# x', '{}'::jsonb, $2)
		RETURNING id
	`, wsID, testUserID).Scan(&skillID); err != nil {
		t.Fatalf("insert skill: %v", err)
	}
	seedAgentLabel(t, ctx, wsID, agentID)
	seedSkillLabel(t, ctx, wsID, skillID)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_to_label WHERE agent_id = $1`, agentID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM skill_to_label WHERE skill_id = $1`, skillID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
	})

	return wsID, agentID, skillID
}

// TestDeleteWorkspace_CleansResourceLabelAssignments: workspace delete cascades
// away the agents and skills, but the junction tables have no workspace_id and
// no foreign key, so both must be swept before the cascade or they orphan.
func TestDeleteWorkspace_CleansResourceLabelAssignments(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	wsID, agentID, skillID := seedWorkspaceResourceLabelFixture(t, ctx, "handler-tests-delete-labels")

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/workspaces/"+wsID, nil)
	req = withURLParam(req, "id", wsID)
	testHandler.DeleteWorkspace(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteWorkspace: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	if n := countAgentLabelAssignments(t, ctx, agentID); n != 0 {
		t.Fatalf("agent_to_label rows survived workspace delete: %d", n)
	}
	if n := countSkillLabelAssignments(t, ctx, skillID); n != 0 {
		t.Fatalf("skill_to_label rows survived workspace delete: %d", n)
	}
}

// TestDeleteWorkspace_RollsBackResourceLabelCleanup verifies the cleanup and
// final workspace delete share one database statement. A restrictive test-only
// foreign key makes the final delete fail; both junction rows must remain.
func TestDeleteWorkspace_RollsBackResourceLabelCleanup(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	wsID, agentID, skillID := seedWorkspaceResourceLabelFixture(t, ctx, "handler-tests-delete-labels-rollback")

	const guardTable = "workspace_delete_resource_label_rollback_guard"
	_, _ = testPool.Exec(ctx, `DROP TABLE IF EXISTS `+guardTable)
	if _, err := testPool.Exec(ctx, `
		CREATE TABLE `+guardTable+` (
			workspace_id UUID NOT NULL REFERENCES workspace(id)
		)
	`); err != nil {
		t.Fatalf("create workspace delete guard: %v", err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO `+guardTable+` (workspace_id) VALUES ($1)`, wsID); err != nil {
		t.Fatalf("insert workspace delete guard: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DROP TABLE IF EXISTS `+guardTable)
	})

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/workspaces/"+wsID, nil)
	req = withURLParam(req, "id", wsID)
	testHandler.DeleteWorkspace(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("DeleteWorkspace: expected 500, got %d: %s", w.Code, w.Body.String())
	}

	var workspaceExists bool
	if err := testPool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM workspace WHERE id = $1)`, wsID).Scan(&workspaceExists); err != nil {
		t.Fatalf("check workspace after failed delete: %v", err)
	}
	if !workspaceExists {
		t.Fatal("workspace was removed despite the injected delete failure")
	}
	if n := countAgentLabelAssignments(t, ctx, agentID); n != 1 {
		t.Fatalf("agent_to_label rows after failed workspace delete = %d, want 1", n)
	}
	if n := countSkillLabelAssignments(t, ctx, skillID); n != 1 {
		t.Fatalf("skill_to_label rows after failed workspace delete = %d, want 1", n)
	}
}
