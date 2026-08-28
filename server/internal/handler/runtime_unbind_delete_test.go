package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// parseExpectedActiveAgentIDs is the cascade endpoint's input validator.
// Empty list is a valid plan ("no active agents" — cascade just deletes the
// runtime); malformed UUIDs must surface as 400 so a bug in the front-end
// can't silently dilute the plan check.
func TestParseExpectedActiveAgentIDs(t *testing.T) {
	t.Run("empty list returns empty set, ok", func(t *testing.T) {
		got, ok := parseExpectedActiveAgentIDs(nil)
		if !ok {
			t.Fatalf("expected ok for nil input")
		}
		if len(got) != 0 {
			t.Fatalf("expected empty set, got %d entries", len(got))
		}
	})

	t.Run("valid uuids are accepted and deduplicated by set semantics", func(t *testing.T) {
		ids := []string{
			"11111111-1111-1111-1111-111111111111",
			"22222222-2222-2222-2222-222222222222",
			"11111111-1111-1111-1111-111111111111", // dup is intentional
		}
		got, ok := parseExpectedActiveAgentIDs(ids)
		if !ok {
			t.Fatalf("expected ok for valid uuid list")
		}
		if len(got) != 2 {
			t.Fatalf("expected dedup set of 2, got %d", len(got))
		}
		for _, want := range []string{
			"11111111-1111-1111-1111-111111111111",
			"22222222-2222-2222-2222-222222222222",
		} {
			if _, ok := got[want]; !ok {
				t.Fatalf("expected %s in set", want)
			}
		}
	})

	t.Run("any malformed entry fails the whole list", func(t *testing.T) {
		ids := []string{
			"11111111-1111-1111-1111-111111111111",
			"not-a-uuid",
		}
		_, ok := parseExpectedActiveAgentIDs(ids)
		if ok {
			t.Fatal("expected !ok for list containing malformed uuid")
		}
	})
}

// activeAgentSetMatches drives the runtime_delete_plan_changed branch: it
// must report mismatch for any divergence — extra agent, missing agent, or
// substituted agent — and accept order-insensitive set equality.
func TestActiveAgentSetMatches(t *testing.T) {
	mkAgent := func(id string) db.Agent {
		u, err := uuidFromString(id)
		if err != nil {
			t.Fatalf("uuidFromString: %v", err)
		}
		return db.Agent{ID: u}
	}
	a1 := mkAgent("11111111-1111-1111-1111-111111111111")
	a2 := mkAgent("22222222-2222-2222-2222-222222222222")
	a3 := mkAgent("33333333-3333-3333-3333-333333333333")

	t.Run("equal sets match regardless of order", func(t *testing.T) {
		expected := map[string]struct{}{
			"11111111-1111-1111-1111-111111111111": {},
			"22222222-2222-2222-2222-222222222222": {},
		}
		if !activeAgentSetMatches([]db.Agent{a2, a1}, expected) {
			t.Fatal("expected match for set-equal inputs")
		}
	})

	t.Run("missing agent is a mismatch", func(t *testing.T) {
		expected := map[string]struct{}{
			"11111111-1111-1111-1111-111111111111": {},
			"22222222-2222-2222-2222-222222222222": {},
		}
		if activeAgentSetMatches([]db.Agent{a1}, expected) {
			t.Fatal("expected mismatch when an agent disappeared")
		}
	})

	t.Run("extra agent is a mismatch", func(t *testing.T) {
		expected := map[string]struct{}{
			"11111111-1111-1111-1111-111111111111": {},
		}
		if activeAgentSetMatches([]db.Agent{a1, a2}, expected) {
			t.Fatal("expected mismatch when a new agent appeared")
		}
	})

	t.Run("substituted agent is a mismatch", func(t *testing.T) {
		expected := map[string]struct{}{
			"11111111-1111-1111-1111-111111111111": {},
			"22222222-2222-2222-2222-222222222222": {},
		}
		if activeAgentSetMatches([]db.Agent{a1, a3}, expected) {
			t.Fatal("expected mismatch when one agent was swapped for another")
		}
	})

	t.Run("both empty matches", func(t *testing.T) {
		if !activeAgentSetMatches(nil, map[string]struct{}{}) {
			t.Fatal("expected empty/empty to match")
		}
	})
}

func uuidFromString(s string) (pgtype.UUID, error) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return pgtype.UUID{}, err
	}
	return u, nil
}

// TestDeleteAgentRuntime_StructuredConflict covers the new 409 shape: the
// strict DELETE refuses with `runtime_has_active_agents` and the body carries
// the live active-agent list so the front-end can pivot to the cascade dialog
// without a second round-trip.
func TestDeleteAgentRuntime_StructuredConflict(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	runtimeID := createCascadeFixtureRuntime(t, ctx, "Cascade 409 Runtime")
	agentID := createCascadeFixtureAgent(t, ctx, runtimeID, "Cascade 409 Agent")
	_ = agentID

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/runtimes/"+runtimeID, nil)
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.DeleteAgentRuntime(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}

	var body struct {
		Error        string          `json:"error"`
		Code         string          `json:"code"`
		ActiveAgents []AgentResponse `json:"active_agents"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != "runtime_has_active_agents" {
		t.Fatalf("expected code runtime_has_active_agents, got %q", body.Code)
	}
	if len(body.ActiveAgents) != 1 || body.ActiveAgents[0].ID != agentID {
		t.Fatalf("expected one active agent %s, got %+v", agentID, body.ActiveAgents)
	}
}

func TestDeleteAgentRuntime_WorkspaceMismatchReturnsStructuredConflict(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createCascadeFixtureRuntime(t, ctx, "Runtime Workspace Mismatch")
	agentID := createArchivedWorkspaceMismatchedAgent(t, runtimeID, "Runtime Workspace Mismatch Agent")

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/runtimes/"+runtimeID, nil)
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.DeleteAgentRuntime(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != "runtime_delete_workspace_mismatch" {
		t.Fatalf("code = %q, want runtime_delete_workspace_mismatch", body.Code)
	}

	var runtimeRows int
	var boundRuntime string
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&runtimeRows); err != nil {
		t.Fatalf("count runtime rows: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&boundRuntime); err != nil {
		t.Fatalf("read mismatched agent binding: %v", err)
	}
	if runtimeRows != 1 || boundRuntime != runtimeID {
		t.Fatalf("workspace mismatch mutated data: runtime rows=%d agent runtime=%q", runtimeRows, boundRuntime)
	}
}

func TestRuntimeDeleteLockBlocksConcurrentAgentBinding(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createCascadeFixtureRuntime(t, ctx, "Runtime Delete Lock")

	deleteTx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin delete transaction: %v", err)
	}
	defer deleteTx.Rollback(ctx)
	if _, err := testHandler.Queries.WithTx(deleteTx).LockAgentRuntime(
		ctx,
		parseUUID(runtimeID),
	); err != nil {
		t.Fatalf("lock runtime for delete: %v", err)
	}

	bindTx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin bind transaction: %v", err)
	}
	defer bindTx.Rollback(ctx)
	if _, err := bindTx.Exec(ctx, `SET LOCAL lock_timeout = '50ms'`); err != nil {
		t.Fatalf("set lock timeout: %v", err)
	}
	if _, err := bindTx.Exec(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, 'concurrent bind probe', '', 'cloud', '{}'::jsonb,
			$2, 'workspace', 1, $3)
	`, testWorkspaceID, runtimeID, testUserID); err == nil {
		t.Fatal("agent bind unexpectedly bypassed the runtime delete lock")
	}
}

func TestAutopilotAssignmentLockBlocksRuntimeTeardown(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createCascadeFixtureRuntime(t, ctx, "Autopilot Assignment Lock")
	agentID := createCascadeFixtureAgent(t, ctx, runtimeID, "Autopilot Assignment Agent")

	assignmentTx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin assignment transaction: %v", err)
	}
	defer assignmentTx.Rollback(ctx)
	if _, err := testHandler.Queries.WithTx(assignmentTx).LockAgentForAutopilotAssignment(
		ctx,
		db.LockAgentForAutopilotAssignmentParams{
			ID:          parseUUID(agentID),
			WorkspaceID: parseUUID(testWorkspaceID),
		},
	); err != nil {
		t.Fatalf("lock Agent for Autopilot assignment: %v", err)
	}

	deleteTx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin delete transaction: %v", err)
	}
	defer deleteTx.Rollback(ctx)
	if _, err := deleteTx.Exec(ctx, `SET LOCAL lock_timeout = '50ms'`); err != nil {
		t.Fatalf("set lock timeout: %v", err)
	}
	if _, err := testHandler.Queries.WithTx(deleteTx).ListUserAgentsByRuntimeForUpdate(
		ctx,
		parseUUID(runtimeID),
	); err == nil {
		t.Fatal("runtime teardown unexpectedly bypassed the Autopilot assignment lock")
	}
}

func TestDeleteAgentRuntime_CustomProfileInstanceRefusesDirectDelete(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	runtimeID, _ := createProfileBackedRuntime(t, ctx, "Custom Instance Delete Guard")

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/runtimes/"+runtimeID, nil)
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.DeleteAgentRuntime(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}

	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != "runtime_profile_instance_delete_unsupported" {
		t.Fatalf("expected runtime_profile_instance_delete_unsupported, got %q", body.Code)
	}

	var rtRows int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&rtRows); err != nil {
		t.Fatalf("count runtime rows: %v", err)
	}
	if rtRows != 1 {
		t.Fatalf("expected custom runtime instance to survive refusal, count=%d", rtRows)
	}
}

func TestDeleteAgentRuntime_OrphanedProfileAllowsDirectDelete(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	runtimeID, profileID := createProfileBackedRuntime(t, ctx, "Orphaned Custom Instance Delete")
	if _, err := testPool.Exec(ctx, `DELETE FROM runtime_profile WHERE id = $1`, profileID); err != nil {
		t.Fatalf("delete profile row: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/runtimes/"+runtimeID, nil)
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.DeleteAgentRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var rtRows int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&rtRows); err != nil {
		t.Fatalf("count runtime rows: %v", err)
	}
	if rtRows != 0 {
		t.Fatalf("expected orphaned custom runtime instance to be deleted, count=%d", rtRows)
	}
}

// TestUnbindAgentsAndDeleteRuntime_HappyPath exercises the confirmed endpoint
// end-to-end: with the correct expected_active_agent_ids snapshot, it must
// unbind the active agent, delete the runtime row, and respond 200 with the
// counts.
func TestUnbindAgentsAndDeleteRuntime_HappyPath(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	runtimeID := createCascadeFixtureRuntime(t, ctx, "Cascade Happy Runtime")
	agentID := createCascadeFixtureAgent(t, ctx, runtimeID, "Cascade Happy Agent")

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/runtimes/"+runtimeID+"/unbind-agents-and-delete",
		map[string]any{"expected_active_agent_ids": []string{agentID}})
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.UnbindAgentsAndDeleteRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Runtime row must be gone.
	var rtRows int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&rtRows); err != nil {
		t.Fatalf("count runtime rows: %v", err)
	}
	if rtRows != 0 {
		t.Fatalf("expected runtime row to be deleted, found %d", rtRows)
	}
	// The agent must SURVIVE, unbound and un-archived. This is the whole point
	// of MUL-5559: the old flow archived it and then hard-deleted the row, taking
	// its chat sessions with it, while the dialog promised an archive.
	var (
		agentRows  int
		hasRuntime bool
		archived   bool
	)
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent WHERE id = $1`, agentID).Scan(&agentRows); err != nil {
		t.Fatalf("count agent rows: %v", err)
	}
	if agentRows != 1 {
		t.Fatalf("expected the agent to survive its runtime, found %d rows", agentRows)
	}
	if err := testPool.QueryRow(ctx,
		`SELECT runtime_id IS NOT NULL, archived_at IS NOT NULL FROM agent WHERE id = $1`,
		agentID).Scan(&hasRuntime, &archived); err != nil {
		t.Fatalf("read agent binding: %v", err)
	}
	if hasRuntime {
		t.Fatalf("expected the surviving agent to be unbound (runtime_id IS NULL)")
	}
	if archived {
		t.Fatalf("unbinding must not archive the agent: unbound and archived are orthogonal")
	}

	var body struct {
		AgentsUnbound  int `json:"agents_unbound"`
		AgentsArchived int `json:"agents_archived"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.AgentsUnbound != 1 {
		t.Fatalf("agents_unbound = %d, want 1", body.AgentsUnbound)
	}
	// Deprecated mirror kept for installed clients built against the
	// archive-and-delete contract.
	if body.AgentsArchived != 1 {
		t.Fatalf("agents_archived mirror = %d, want 1", body.AgentsArchived)
	}
}

func TestUnbindAgentsAndDeleteRuntime_CustomProfileInstanceRefusesDirectDelete(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	runtimeID, _ := createProfileBackedRuntime(t, ctx, "Custom Instance Cascade Guard")

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/runtimes/"+runtimeID+"/unbind-agents-and-delete",
		map[string]any{"expected_active_agent_ids": []string{}})
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.UnbindAgentsAndDeleteRuntime(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}

	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != "runtime_profile_instance_delete_unsupported" {
		t.Fatalf("expected runtime_profile_instance_delete_unsupported, got %q", body.Code)
	}

	var rtRows int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&rtRows); err != nil {
		t.Fatalf("count runtime rows: %v", err)
	}
	if rtRows != 1 {
		t.Fatalf("expected custom runtime instance to survive refusal, count=%d", rtRows)
	}
}

func TestUnbindAgentsAndDeleteRuntime_OrphanedProfileAllowsCascade(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	runtimeID, profileID := createProfileBackedRuntime(t, ctx, "Orphaned Custom Instance Cascade")
	if _, err := testPool.Exec(ctx, `DELETE FROM runtime_profile WHERE id = $1`, profileID); err != nil {
		t.Fatalf("delete profile row: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/runtimes/"+runtimeID+"/unbind-agents-and-delete",
		map[string]any{"expected_active_agent_ids": []string{}})
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.UnbindAgentsAndDeleteRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var rtRows int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&rtRows); err != nil {
		t.Fatalf("count runtime rows: %v", err)
	}
	if rtRows != 0 {
		t.Fatalf("expected orphaned custom runtime instance to be deleted, count=%d", rtRows)
	}
}

// TestUnbindAgentsAndDeleteRuntime_PlanChanged proves the dialog-confirm
// race guard: if the user's snapshot of active agents drifts from the live
// set (somebody added or archived an agent while the dialog was open), the
// cascade endpoint must refuse with 409 + runtime_delete_plan_changed and
// surface the new live snapshot so the dialog can re-prompt.
func TestUnbindAgentsAndDeleteRuntime_PlanChanged(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	runtimeID := createCascadeFixtureRuntime(t, ctx, "Cascade Drift Runtime")
	agent1 := createCascadeFixtureAgent(t, ctx, runtimeID, "Cascade Drift Agent A")
	agent2 := createCascadeFixtureAgent(t, ctx, runtimeID, "Cascade Drift Agent B")

	// User confirmed only agent1 — but the live set is {agent1, agent2}.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/runtimes/"+runtimeID+"/unbind-agents-and-delete",
		map[string]any{"expected_active_agent_ids": []string{agent1}})
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.UnbindAgentsAndDeleteRuntime(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}

	var body struct {
		Code         string          `json:"code"`
		ActiveAgents []AgentResponse `json:"active_agents"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != "runtime_delete_plan_changed" {
		t.Fatalf("expected code runtime_delete_plan_changed, got %q", body.Code)
	}
	if len(body.ActiveAgents) != 2 {
		t.Fatalf("expected 2 active agents in fresh snapshot, got %d", len(body.ActiveAgents))
	}
	// Runtime must still exist — the plan-changed branch is non-destructive.
	var rtRows int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&rtRows); err != nil {
		t.Fatalf("count runtime rows: %v", err)
	}
	if rtRows != 1 {
		t.Fatalf("expected runtime to survive plan-changed refusal, count=%d", rtRows)
	}

	_ = agent2
}

// createCascadeFixtureRuntime creates a fresh runtime owned by testUserID
// inside testWorkspaceID and registers cleanup. Each cascade test uses its
// own runtime so the destructive paths don't trample the shared fixture.
func createCascadeFixtureRuntime(t *testing.T, ctx context.Context, name string) string {
	t.Helper()
	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, last_seen_at
		)
		VALUES ($1, NULL, $2, 'cloud', 'cascade-test', 'online', $3, '{}'::jsonb, $4, now())
		RETURNING id
	`, testWorkspaceID, name, name+" device", testUserID).Scan(&runtimeID); err != nil {
		t.Fatalf("insert cascade fixture runtime: %v", err)
	}
	t.Cleanup(func() {
		// Best-effort cleanup. The cascade endpoint deletes the runtime;
		// these statements only matter when the test failed before the
		// cascade ran.
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE runtime_id = $1`, runtimeID)
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})
	return runtimeID
}

func createProfileBackedRuntime(t *testing.T, ctx context.Context, name string) (string, string) {
	t.Helper()
	var profileID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO runtime_profile (
			workspace_id, display_name, protocol_family, command_name,
			fixed_args, visibility, created_by, enabled
		)
		VALUES ($1, $2, 'codex', 'custom-codex', '[]'::jsonb, 'workspace', $3, true)
		RETURNING id
	`, testWorkspaceID, name+" Profile", testUserID).Scan(&profileID); err != nil {
		t.Fatalf("insert runtime profile: %v", err)
	}

	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, profile_id, last_seen_at
		)
		VALUES ($1, $2, $3, 'local', 'codex', 'online', $4, '{}'::jsonb, $5, $6, now())
		RETURNING id
	`, testWorkspaceID, "daemon-"+profileID, name, name+" device", testUserID, profileID).Scan(&runtimeID); err != nil {
		t.Fatalf("insert profile-backed runtime: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE runtime_id = $1`, runtimeID)
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
		testPool.Exec(context.Background(), `DELETE FROM runtime_profile WHERE id = $1`, profileID)
	})
	return runtimeID, profileID
}

func createCascadeFixtureAgent(t *testing.T, ctx context.Context, runtimeID, name string) string {
	t.Helper()
	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'private', 1, $4)
		RETURNING id
	`, testWorkspaceID, name, runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("insert cascade fixture agent: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})
	return agentID
}

func createArchivedWorkspaceMismatchedAgent(t *testing.T, runtimeID, name string) string {
	t.Helper()
	foreignWorkspaceID := dbfx.Workspace(t, name+" Workspace", "runtime-mismatch-"+runtimeID[:8])
	return dbfx.Agent(t, name, runtimeID, testutil.Cols{
		"workspace_id": foreignWorkspaceID,
		"archived_at":  testutil.Raw("now()"),
		"owner_id":     nil,
	})
}
