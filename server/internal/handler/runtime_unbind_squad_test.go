package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// These tests cover what a runtime teardown does to squads whose leader lives
// on that runtime.
//
// The problem they used to work around is gone. Because a runtime delete
// hard-deleted its archived agents, and squad.leader_id REFERENCES agent(id) ON
// DELETE RESTRICT, a squad led by such an agent blocked the delete with a 500 —
// so the handler deleted archived squads first and refused (409) whenever an
// ACTIVE squad still pointed at an archived leader, leaving the runtime
// undeletable until the user archived the squad or replaced its leader.
//
// Since MUL-5559 the leader is not deleted at all: it is unbound and keeps
// everything, so the RESTRICT FK is never challenged. Both the squad delete and
// the 409 guard are therefore removed, and these tests pin the new contract —
// squads and their leaders survive, and the runtime deletes cleanly.

// seedIsolatedRuntime creates a fresh runtime in the shared test workspace
// (so the seeded test user is owner/admin and passes canEditRuntime), and
// returns its UUID. The runtime is auto-cleaned via t.Cleanup; tests that
// successfully drive DeleteAgentRuntime through to the end will have already
// deleted it, in which case the cleanup is a no-op.
func seedIsolatedRuntime(t *testing.T, name string) string {
	t.Helper()
	ctx := context.Background()
	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at
		)
		VALUES ($1, NULL, $2, 'cloud', 'isolated_test', 'online', 'isolated test runtime', '{}'::jsonb, now())
		RETURNING id
	`, testWorkspaceID, name).Scan(&runtimeID); err != nil {
		t.Fatalf("seed runtime %q: %v", name, err)
	}
	t.Cleanup(func() {
		// Best-effort cascading cleanup; ignore errors because the handler
		// may have already removed the row in the happy path.
		testPool.Exec(ctx, `DELETE FROM agent WHERE runtime_id = $1`, runtimeID)
		testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})
	return runtimeID
}

// seedAgentOnRuntime creates an agent on the given runtime. If archived is
// true the row is created with archived_at = now().
func seedAgentOnRuntime(t *testing.T, runtimeID, name string, archived bool) string {
	t.Helper()
	ctx := context.Background()
	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4)
		RETURNING id
	`, testWorkspaceID, name, runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("seed agent %q: %v", name, err)
	}
	if archived {
		if _, err := testPool.Exec(ctx,
			`UPDATE agent SET archived_at = now(), archived_by = $1 WHERE id = $2`,
			testUserID, agentID,
		); err != nil {
			t.Fatalf("archive agent %q: %v", name, err)
		}
	}
	t.Cleanup(func() {
		// Squad rows referencing this agent could block a plain DELETE; nuke
		// them first. Tests that complete through the handler will already
		// have done this.
		testPool.Exec(ctx, `DELETE FROM squad WHERE leader_id = $1`, agentID)
		testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, agentID)
	})
	return agentID
}

// seedSquad creates a squad with the given leader. If archived is true the
// row is created with archived_at = now() (the case the user originally hit
// — `multica squad list` filters out archived squads, hiding the FK
// blocker).
func seedSquad(t *testing.T, leaderID, name string, archived bool) string {
	t.Helper()
	ctx := context.Background()
	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, $2, '', $3, $4)
		RETURNING id
	`, testWorkspaceID, name, leaderID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("seed squad %q: %v", name, err)
	}
	if archived {
		if _, err := testPool.Exec(ctx,
			`UPDATE squad SET archived_at = now(), archived_by = $1 WHERE id = $2`,
			testUserID, squadID,
		); err != nil {
			t.Fatalf("archive squad %q: %v", name, err)
		}
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM squad WHERE id = $1`, squadID)
	})
	return squadID
}

func squadExists(t *testing.T, squadID string) bool {
	t.Helper()
	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM squad WHERE id = $1`, squadID,
	).Scan(&count); err != nil {
		t.Fatalf("count squad %s: %v", squadID, err)
	}
	return count == 1
}

func agentExists(t *testing.T, agentID string) bool {
	t.Helper()
	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM agent WHERE id = $1`, agentID,
	).Scan(&count); err != nil {
		t.Fatalf("count agent %s: %v", agentID, err)
	}
	return count == 1
}

func runtimeExists(t *testing.T, runtimeID string) bool {
	t.Helper()
	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM agent_runtime WHERE id = $1`, runtimeID,
	).Scan(&count); err != nil {
		t.Fatalf("count runtime %s: %v", runtimeID, err)
	}
	return count == 1
}

// TestDeleteAgentRuntime_KeepsSquadsLedByUnboundAgents is the end-to-end
// regression: a runtime whose only agents are archived squad leaders must delete
// cleanly, and neither the squads nor the leaders may be destroyed.
//
// The archived squad in this fixture is the case originally reported: it is
// invisible to `multica squad list`, so a user could not see what was blocking
// the delete — and the old fix resolved that by deleting the squad.
func TestDeleteAgentRuntime_KeepsSquadsLedByUnboundAgents(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID := seedIsolatedRuntime(t, "Runtime With Archived Squad Leader")
	archivedLeader := seedAgentOnRuntime(t, runtimeID, "Archived Squad Leader Agent", true)
	archivedSquad := seedSquad(t, archivedLeader, "Archived Squad For Runtime Delete", true)
	activeSquad := seedSquad(t, archivedLeader, "Active Squad For Runtime Delete", false)

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/runtimes/"+runtimeID, nil)
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.DeleteAgentRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("DeleteAgentRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if !squadExists(t, archivedSquad) {
		t.Errorf("archived squad must survive: its leader is unbound, not deleted")
	}
	if !squadExists(t, activeSquad) {
		t.Errorf("active squad must survive: its leader is unbound, not deleted")
	}
	if !agentExists(t, archivedLeader) {
		t.Errorf("archived leader must survive its runtime as an unbound agent")
	}
	if runtimeExists(t, runtimeID) {
		t.Errorf("runtime should have been deleted")
	}
	if bound := agentRuntimeBound(t, archivedLeader); bound {
		t.Errorf("surviving leader must be unbound (runtime_id IS NULL)")
	}
}

// TestDeleteAgentRuntime_ActiveSquadWithArchivedLeaderNoLongerConflicts is the
// direct inversion of the old guard: this used to be a 409 telling the user to
// archive the squad or assign a new leader. Nothing needs to be given up now.
func TestDeleteAgentRuntime_ActiveSquadWithArchivedLeaderNoLongerConflicts(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID := seedIsolatedRuntime(t, "Runtime With Active Squad And Archived Leader")
	archivedLeader := seedAgentOnRuntime(t, runtimeID, "Archived Leader Formerly Blocking Delete", true)
	activeSquad := seedSquad(t, archivedLeader, "Active Squad Formerly Blocking Delete", false)
	var autopilotID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO autopilot (
			workspace_id, title, assignee_type, assignee_id,
			created_by_type, created_by_id, status, execution_mode
		)
		VALUES ($1, 'squad runtime pause', 'squad', $2, 'member', $3, 'active', 'run_only')
		RETURNING id
	`, testWorkspaceID, activeSquad, testUserID).Scan(&autopilotID); err != nil {
		t.Fatalf("seed squad autopilot: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, autopilotID)
	})

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/runtimes/"+runtimeID, nil)
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.DeleteAgentRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("DeleteAgentRuntime: expected 200 (guard removed), got %d: %s", w.Code, w.Body.String())
	}

	if !squadExists(t, activeSquad) {
		t.Errorf("active squad must survive the runtime delete")
	}
	if !agentExists(t, archivedLeader) {
		t.Errorf("archived leader must survive the runtime delete")
	}
	if runtimeExists(t, runtimeID) {
		t.Errorf("runtime should have been deleted")
	}
	var status, pauseReason string
	if err := testPool.QueryRow(context.Background(),
		`SELECT status, pause_reason FROM autopilot WHERE id = $1`, autopilotID,
	).Scan(&status, &pauseReason); err != nil {
		t.Fatalf("read squad autopilot: %v", err)
	}
	if status != "paused" || pauseReason != string(ReasonAgentRuntimeRequired) {
		t.Fatalf("squad autopilot = (%q, %q), want (paused, agent_runtime_required)", status, pauseReason)
	}
}

// TestDeleteAgentRuntime_NoSquadsRegression confirms the common case: an
// archived agent with no squad references survives as an unbound agent and the
// runtime is gone.
func TestDeleteAgentRuntime_NoSquadsRegression(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID := seedIsolatedRuntime(t, "Runtime With No Squad References")
	archivedAgent := seedAgentOnRuntime(t, runtimeID, "Archived Agent No Squad", true)

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/runtimes/"+runtimeID, nil)
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.DeleteAgentRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("DeleteAgentRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if !agentExists(t, archivedAgent) {
		t.Errorf("archived agent must survive its runtime as an unbound agent")
	}
	if agentRuntimeBound(t, archivedAgent) {
		t.Errorf("surviving agent must be unbound (runtime_id IS NULL)")
	}
	if runtimeExists(t, runtimeID) {
		t.Errorf("runtime should have been deleted")
	}
}

func TestUpdateSquad_UnboundLeaderPausesOnlySquadAutopilots(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	boundRuntimeID := seedIsolatedRuntime(t, "Bound Squad Leader Runtime")
	boundLeaderID := seedAgentOnRuntime(t, boundRuntimeID, "Bound Squad Leader", false)
	squadID := seedSquad(t, boundLeaderID, "Squad Leader Rotation", false)

	unboundRuntimeID := seedIsolatedRuntime(t, "Unbound Squad Leader Runtime")
	unboundLeaderID := seedAgentOnRuntime(t, unboundRuntimeID, "Unbound Squad Leader", false)
	if _, err := testPool.Exec(ctx,
		`UPDATE agent SET runtime_id = NULL WHERE id = $1`,
		unboundLeaderID,
	); err != nil {
		t.Fatalf("unbind proposed leader: %v", err)
	}

	var squadAutopilotID, directAutopilotID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO autopilot (
			workspace_id, title, assignee_type, assignee_id,
			created_by_type, created_by_id, status, execution_mode
		)
		VALUES ($1, 'squad leader rotation pause', 'squad', $2,
			'member', $3, 'active', 'run_only')
		RETURNING id
	`, testWorkspaceID, squadID, testUserID).Scan(&squadAutopilotID); err != nil {
		t.Fatalf("seed squad autopilot: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO autopilot (
			workspace_id, title, assignee_type, assignee_id,
			created_by_type, created_by_id, status, execution_mode
		)
		VALUES ($1, 'unrelated direct autopilot', 'agent', $2,
			'member', $3, 'active', 'run_only')
		RETURNING id
	`, testWorkspaceID, unboundLeaderID, testUserID).Scan(&directAutopilotID); err != nil {
		t.Fatalf("seed direct autopilot: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM autopilot WHERE id = ANY($1::uuid[])`,
			[]string{squadAutopilotID, directAutopilotID},
		)
	})

	w := httptest.NewRecorder()
	testHandler.UpdateSquad(w, squadScopeReq(
		"",
		http.MethodPatch,
		"/api/squads/"+squadID,
		map[string]any{"leader_id": unboundLeaderID},
		map[string]string{"id": squadID},
	))
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateSquad: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var squadStatus, pauseReason, directStatus string
	if err := testPool.QueryRow(ctx,
		`SELECT status, pause_reason FROM autopilot WHERE id = $1`,
		squadAutopilotID,
	).Scan(&squadStatus, &pauseReason); err != nil {
		t.Fatalf("read squad autopilot: %v", err)
	}
	if err := testPool.QueryRow(ctx,
		`SELECT status FROM autopilot WHERE id = $1`,
		directAutopilotID,
	).Scan(&directStatus); err != nil {
		t.Fatalf("read direct autopilot: %v", err)
	}
	if squadStatus != "paused" || pauseReason != string(ReasonAgentRuntimeRequired) {
		t.Fatalf("squad autopilot = (%q, %q), want (paused, agent_runtime_required)", squadStatus, pauseReason)
	}
	if directStatus != "active" {
		t.Fatalf("unrelated direct autopilot status = %q, want active", directStatus)
	}
}

// TestDeleteAgentRuntime_StillBlockedByActiveAgents preserves the existing 409
// contract: a runtime with at least one ACTIVE agent must still refuse the
// strict delete, so the user always sees and confirms which agents are about to
// lose their runtime before anything happens.
func TestDeleteAgentRuntime_StillBlockedByActiveAgents(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID := seedIsolatedRuntime(t, "Runtime With Active Agent")
	activeAgent := seedAgentOnRuntime(t, runtimeID, "Active Agent Blocking Delete", false)

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/runtimes/"+runtimeID, nil)
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.DeleteAgentRuntime(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("DeleteAgentRuntime: expected 409 active-agent guard, got %d: %s", w.Code, w.Body.String())
	}

	if !agentExists(t, activeAgent) {
		t.Errorf("active agent must NOT have been touched by a refused runtime delete")
	}
	if !agentRuntimeBound(t, activeAgent) {
		t.Errorf("active agent must still be bound after a refused runtime delete")
	}
	if !runtimeExists(t, runtimeID) {
		t.Errorf("runtime must NOT have been deleted by a refused delete")
	}
}

// agentRuntimeBound reports whether the agent still points at a runtime.
func agentRuntimeBound(t *testing.T, agentID string) bool {
	t.Helper()
	var bound bool
	if err := testPool.QueryRow(context.Background(),
		`SELECT runtime_id IS NOT NULL FROM agent WHERE id = $1`, agentID,
	).Scan(&bound); err != nil {
		t.Fatalf("read runtime_id for agent %s: %v", agentID, err)
	}
	return bound
}
