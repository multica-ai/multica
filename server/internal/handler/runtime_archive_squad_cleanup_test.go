package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
)

// These tests cover the squad-cleanup step added to DeleteAgentRuntime:
// archived agents on a torn-down runtime can still be referenced
// as leaders of squads (including archived ones), and the squad.leader_id
// FK is ON DELETE RESTRICT, so the subsequent DELETE FROM agent would fail.
// The fix runs DeleteSquadsByArchivedAgentsOnRuntime first to drop archived
// squads referencing archived leaders, unblocking the agent delete in the
// originally reported case.

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

// TestDeleteSquadsByArchivedAgentsOnRuntime_Query exercises the new query in
// isolation. It has to delete archived squads whose leader is an archived
// agent on the target runtime, AND only those — active squads, squads led by
// active agents, or squads led by archived agents on a different runtime,
// must be left alone.
func TestDeleteSquadsByArchivedAgentsOnRuntime_Query(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// Two distinct runtimes so we can prove scoping by runtime_id.
	runtimeA := seedIsolatedRuntime(t, "Squad Cleanup Query Runtime A")
	runtimeB := seedIsolatedRuntime(t, "Squad Cleanup Query Runtime B")

	// Leaders:
	//  - archivedOnA: archived agent on runtime A      -> its squads must be deleted
	//  - activeOnA:   active agent on runtime A         -> its squad must survive
	//  - archivedOnB: archived agent on runtime B       -> its squad must survive
	archivedOnA := seedAgentOnRuntime(t, runtimeA, "ArchivedOnA", true)
	activeOnA := seedAgentOnRuntime(t, runtimeA, "ActiveOnA", false)
	archivedOnB := seedAgentOnRuntime(t, runtimeB, "ArchivedOnB", true)

	// Squads:
	//  - activeSquadA: archived leader on A, squad itself active     -> keep
	//  - archivedSquadA: archived leader on A, squad itself archived -> delete (the bug)
	//  - keptActiveLeader: active leader on A                        -> keep
	//  - keptDifferentRuntime: archived leader but on B              -> keep
	activeSquadOnA := seedSquad(t, archivedOnA, "Active Squad On Runtime A", false)
	archivedSquadOnA := seedSquad(t, archivedOnA, "Archived Squad On Runtime A", true)
	keptActiveLeader := seedSquad(t, activeOnA, "Squad With Active Leader", false)
	keptDifferentRuntime := seedSquad(t, archivedOnB, "Squad On Runtime B", false)

	// Run the query against runtime A.
	if err := testHandler.Queries.DeleteSquadsByArchivedAgentsOnRuntime(
		ctx, util.MustParseUUID(runtimeA),
	); err != nil {
		t.Fatalf("DeleteSquadsByArchivedAgentsOnRuntime: %v", err)
	}

	if !squadExists(t, activeSquadOnA) {
		t.Errorf("active squad with archived leader on target runtime must NOT be deleted")
	}
	if squadExists(t, archivedSquadOnA) {
		t.Errorf("archived squad with archived leader on target runtime should be deleted (this is the bug case)")
	}
	if !squadExists(t, keptActiveLeader) {
		t.Errorf("squad with non-archived leader must NOT be deleted")
	}
	if !squadExists(t, keptDifferentRuntime) {
		t.Errorf("squad whose leader is on a different runtime must NOT be deleted")
	}

	// Run the query a second time — it must be idempotent / safe to call when
	// nothing matches anymore.
	if err := testHandler.Queries.DeleteSquadsByArchivedAgentsOnRuntime(
		ctx, util.MustParseUUID(runtimeA),
	); err != nil {
		t.Fatalf("re-running DeleteSquadsByArchivedAgentsOnRuntime should be a no-op, got: %v", err)
	}

	// And running it against a runtime with no archived agents at all must
	// also be a no-op (the test workspace's seeded runtime has only one
	// active agent and no archived ones).
	if err := testHandler.Queries.DeleteSquadsByArchivedAgentsOnRuntime(
		ctx, util.MustParseUUID(testRuntimeID),
	); err != nil {
		t.Fatalf("no-archived-agents runtime: expected no-op, got: %v", err)
	}
}

// TestDeleteAgentRuntime_RemovesArchivedSquadsLedByArchivedAgents is the end-to-end
// regression test: a runtime whose only agents are archived but
// still referenced as squad leaders must now delete cleanly.
//
// Before this fix the handler returned 500 "failed to clean up archived
// agents" because squad.leader_id REFERENCES agent(id) ON DELETE RESTRICT
// blocked the DELETE FROM agent step. With the squad-cleanup step in front
// of the agent-cleanup, the delete succeeds.
func TestDeleteAgentRuntime_RemovesArchivedSquadsLedByArchivedAgents(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID := seedIsolatedRuntime(t, "Runtime With Archived Squad Leader")
	archivedLeader := seedAgentOnRuntime(t, runtimeID, "Archived Squad Leader Agent", true)
	// Use an *archived* squad — that's the case the user originally hit, and
	// it's the one that's invisible from `multica squad list`.
	archivedSquad := seedSquad(t, archivedLeader, "Archived Squad For Runtime Delete", true)

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/runtimes/"+runtimeID, nil)
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.DeleteAgentRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("DeleteAgentRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if squadExists(t, archivedSquad) {
		t.Errorf("squad led by archived agent on the runtime should have been deleted")
	}
	if agentExists(t, archivedLeader) {
		t.Errorf("archived agent on the runtime should have been deleted")
	}
	if runtimeExists(t, runtimeID) {
		t.Errorf("runtime should have been deleted")
	}
}

// TestDeleteAgentRuntime_ActiveSquadWithArchivedLeaderReturnsConflict covers
// the remaining reachable state from the public API: an active squad led by
// an archived agent. Runtime delete must fail cleanly with a 409 before the
// later archived-agent delete trips the RESTRICT FK and leaks a 500.
func TestDeleteAgentRuntime_ActiveSquadWithArchivedLeaderReturnsConflict(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID := seedIsolatedRuntime(t, "Runtime With Active Squad And Archived Leader")
	archivedLeader := seedAgentOnRuntime(t, runtimeID, "Archived Leader Blocking Runtime Delete", true)
	activeSquad := seedSquad(t, archivedLeader, "Active Squad Blocking Runtime Delete", false)

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/runtimes/"+runtimeID, nil)
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.DeleteAgentRuntime(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("DeleteAgentRuntime: expected 409 archived-leader squad guard, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "active squads led by archived agents") {
		t.Fatalf("DeleteAgentRuntime: expected actionable archived-leader squad message, got body %s", w.Body.String())
	}

	if !squadExists(t, activeSquad) {
		t.Errorf("active squad must NOT have been deleted by a refused runtime delete")
	}
	if !agentExists(t, archivedLeader) {
		t.Errorf("archived leader must NOT have been deleted by a refused runtime delete")
	}
	if !runtimeExists(t, runtimeID) {
		t.Errorf("runtime must NOT have been deleted by a refused delete")
	}
}

// TestDeleteAgentRuntime_ArchivedAndActiveSquadsReturnConflictWithoutDeletes
// pins the combination case from review: if the same archived leader is
// referenced by both an archived squad and an active squad on the runtime, the
// handler must return 409 before deleting either squad.
func TestDeleteAgentRuntime_ArchivedAndActiveSquadsReturnConflictWithoutDeletes(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID := seedIsolatedRuntime(t, "Runtime With Archived And Active Squads")
	archivedLeader := seedAgentOnRuntime(t, runtimeID, "Archived Leader With Mixed Squads", true)
	archivedSquad := seedSquad(t, archivedLeader, "Archived Squad On Refused Delete", true)
	activeSquad := seedSquad(t, archivedLeader, "Active Squad On Refused Delete", false)

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/runtimes/"+runtimeID, nil)
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.DeleteAgentRuntime(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("DeleteAgentRuntime: expected 409 archived-leader squad guard, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "active squads led by archived agents") {
		t.Fatalf("DeleteAgentRuntime: expected actionable archived-leader squad message, got body %s", w.Body.String())
	}

	if !squadExists(t, archivedSquad) {
		t.Errorf("archived squad must NOT have been deleted by a refused runtime delete")
	}
	if !squadExists(t, activeSquad) {
		t.Errorf("active squad must NOT have been deleted by a refused runtime delete")
	}
	if !agentExists(t, archivedLeader) {
		t.Errorf("archived leader must NOT have been deleted by a refused runtime delete")
	}
	if !runtimeExists(t, runtimeID) {
		t.Errorf("runtime must NOT have been deleted by a refused delete")
	}
}

// TestDeleteAgentRuntime_NoSquadsRegression confirms the new pre-cleanup
// step is a safe no-op when the runtime's archived agents were never squad
// leaders. Without this, the fix could regress the common case.
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

	if agentExists(t, archivedAgent) {
		t.Errorf("archived agent should have been deleted")
	}
	if runtimeExists(t, runtimeID) {
		t.Errorf("runtime should have been deleted")
	}
}

// TestDeleteAgentRuntime_StillBlockedByActiveAgents preserves the existing
// 409 contract: even with the new squad-cleanup step in place, a runtime
// with at least one *active* agent must still refuse to be deleted, because
// the squad-cleanup only targets squads led by archived agents and would
// silently delete nothing here. Without this guard, a careless reorder of
// the handler steps could let active agents get cascaded away.
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
		t.Errorf("active agent must NOT have been deleted by a refused runtime delete")
	}
	if !runtimeExists(t, runtimeID) {
		t.Errorf("runtime must NOT have been deleted by a refused delete")
	}
}
