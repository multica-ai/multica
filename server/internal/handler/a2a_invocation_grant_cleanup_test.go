package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestUnbindAgentsAndDeleteRuntime_CleansSystemAgentGrants pins the NEX-24
// cleanup wiring in the runtime hard-delete path: the runtime's SYSTEM agents
// are hard-deleted by unbindRuntimeForDelete, and their agent_invocation_grant
// rows must go with them — both the rows they own (agent side) and the rows
// that name them as a grantee (grantee side), since the table has no FK to
// agent (repo rule). Rows that reference only surviving user agents must stay.
func TestUnbindAgentsAndDeleteRuntime_CleansSystemAgentGrants(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	runtimeID := createCascadeFixtureRuntime(t, ctx, "A2A Grant Cleanup Runtime")
	userAgent := createCascadeFixtureAgent(t, ctx, runtimeID, "A2A Grant Cleanup User")
	systemAgent := createCascadeFixtureAgent(t, ctx, runtimeID, "A2A Grant Cleanup System")
	if _, err := testPool.Exec(ctx,
		`UPDATE agent SET kind = 'system', system_key = 'a2a_grant_cleanup_probe' WHERE id = $1`,
		systemAgent); err != nil {
		t.Fatalf("make agent a system agent: %v", err)
	}
	otherUser := createCascadeFixtureAgent(t, ctx, runtimeID, "A2A Grant Cleanup Other User")

	// Grant rows referencing the system agent, on both sides of the table.
	addA2ATestGrant(t, systemAgent, userAgent) // system agent owns the whitelist
	addA2ATestGrant(t, userAgent, systemAgent) // system agent named as grantee
	// A grant between two surviving user agents must be untouched.
	addA2ATestGrant(t, userAgent, otherUser)

	// Only the user agent is part of the confirmed plan: system agents never
	// appear in the dialog (ListActiveAgentsByRuntimeForUpdate filters to
	// kind='user'). Both user agents are active on the runtime, so the plan
	// must name them both.
	unbindRuntime(t, ctx, runtimeID, userAgent, otherUser)

	var systemRows int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent WHERE id = $1`, systemAgent).Scan(&systemRows); err != nil {
		t.Fatalf("count system agent: %v", err)
	}
	if systemRows != 0 {
		t.Fatalf("system agent rows = %d, want 0 (still deleted with its runtime)", systemRows)
	}

	grants := func() map[string]int {
		m := map[string]int{}
		for _, q := range []struct {
			agentID  string
			grantee  string
			label    string
		}{
			{systemAgent, userAgent, "system->user"},
			{userAgent, systemAgent, "user->system"},
			{userAgent, otherUser, "user->other"},
		} {
			var n int
			if err := testPool.QueryRow(ctx,
				`SELECT count(*) FROM agent_invocation_grant WHERE agent_id = $1 AND grantee_agent_id = $2`,
				q.agentID, q.grantee).Scan(&n); err != nil {
				t.Fatalf("count grant %s: %v", q.label, err)
			}
			m[q.label] = n
		}
		return m
	}

	got := grants()
	if got["system->user"] != 0 {
		t.Errorf("grant owned by deleted system agent (system->user) survived, count = %d", got["system->user"])
	}
	if got["user->system"] != 0 {
		t.Errorf("grant naming deleted system agent as grantee (user->system) survived, count = %d", got["user->system"])
	}
	if got["user->other"] != 1 {
		t.Errorf("grant between surviving user agents must be untouched (user->other), count = %d", got["user->other"])
	}
}

// TestDeleteChatSession_CleansBuilderCarrierGrants pins the NEX-24 cleanup
// wiring in the chat-session delete path: deleting a builder session hard-
// deletes its hidden `kind='system'` carrier (DeleteSystemAgentByID), and the
// carrier's agent_invocation_grant rows must be cleared in the same tx — both
// the rows it owns (agent side) and the rows that name it as a grantee.
func TestDeleteChatSession_CleansBuilderCarrierGrants(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// A builder carrier is a kind='system' agent whose system_key starts with
	// agent_builder:. DeleteSystemAgentByID guards on exactly that identity.
	runtimeID := seedIsolatedRuntime(t, "Chat Builder Grant Cleanup Runtime")
	carrier := seedAgentOnRuntime(t, runtimeID, "Chat Builder Carrier", false)
	if _, err := testPool.Exec(ctx,
		`UPDATE agent SET kind = 'system', system_key = 'agent_builder:a2a_grant_cleanup_probe' WHERE id = $1`,
		carrier); err != nil {
		t.Fatalf("make carrier a system builder agent: %v", err)
	}
	peer := createA2ATestAgent(t, "a2a-grant-cleanup-peer", "private", a2aModeDefault)
	sessionID := createHandlerTestChatSession(t, carrier)

	addA2ATestGrant(t, carrier, peer) // carrier owns the whitelist
	addA2ATestGrant(t, peer, carrier) // carrier named as grantee

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/chat/sessions/"+sessionID, nil)
	req.Header.Set("X-User-ID", testUserID)
	req = withURLParam(req, "sessionId", sessionID)
	req = withChatTestWorkspaceCtx(t, req)
	testHandler.DeleteChatSession(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteChatSession: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	var carrierRows int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent WHERE id = $1`, carrier).Scan(&carrierRows); err != nil {
		t.Fatalf("count carrier agent: %v", err)
	}
	if carrierRows != 0 {
		t.Fatalf("builder carrier rows = %d, want 0 (hard-deleted with its session)", carrierRows)
	}

	var owned, asGrantee int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_invocation_grant WHERE agent_id = $1`, carrier).Scan(&owned); err != nil {
		t.Fatalf("count grants owned by carrier: %v", err)
	}
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_invocation_grant WHERE grantee_agent_id = $1`, carrier).Scan(&asGrantee); err != nil {
		t.Fatalf("count grants naming carrier as grantee: %v", err)
	}
	if owned != 0 {
		t.Errorf("grant rows owned by deleted carrier survived, count = %d", owned)
	}
	if asGrantee != 0 {
		t.Errorf("grant rows naming deleted carrier as grantee survived, count = %d", asGrantee)
	}
}
