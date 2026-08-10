package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestA2aInvocationAllowed_Pure exercises the pure A2A invoke-gate predicate
// (NEX-24) with injected stub lookups and NO database. It locks the four-mode
// semantics plus the fail-closed edges:
//   - empty mode  -> false for every actor (status-quo fail-closed)
//   - any_agent   -> agent actors admitted; system NEVER admitted
//   - squad_leaders -> only agent actors that lead a squad; system never matches
//   - specific_agents -> only whitelisted agent actors; system never matches
//   - lookup errors and malformed actor ids -> false
func TestA2aInvocationAllowed_Pure(t *testing.T) {
	ctx := context.Background()
	leaderID := "11111111-1111-1111-1111-111111111111"
	otherID := "22222222-2222-2222-2222-222222222222"
	wsID := "33333333-3333-3333-3333-333333333333"

	squadStub := func(result bool, err error) func(context.Context, pgtype.UUID, pgtype.UUID) (bool, error) {
		return func(context.Context, pgtype.UUID, pgtype.UUID) (bool, error) { return result, err }
	}
	grantStub := func(result bool, err error) func(context.Context, pgtype.UUID, pgtype.UUID) (bool, error) {
		return func(context.Context, pgtype.UUID, pgtype.UUID) (bool, error) { return result, err }
	}
	agentWithMode := func(mode string) db.Agent {
		return db.Agent{A2aInvocationMode: mode}
	}

	cases := []struct {
		name      string
		agent     db.Agent
		actorType string
		actorID   string
		isLeader  func(context.Context, pgtype.UUID, pgtype.UUID) (bool, error)
		hasGrant  func(context.Context, pgtype.UUID, pgtype.UUID) (bool, error)
		want      bool
	}{
		// Empty (unset) = status-quo fail-closed: never widens.
		{"empty mode: agent actor denied", agentWithMode(""), "agent", leaderID, squadStub(false, nil), grantStub(false, nil), false},
		{"empty mode: system actor denied", agentWithMode(""), "system", "", squadStub(false, nil), grantStub(false, nil), false},
		{"unknown mode: agent actor denied", agentWithMode("bogus"), "agent", leaderID, squadStub(false, nil), grantStub(false, nil), false},

		// any_agent admits agent actors only — NEVER system (CEO ruling).
		{"any_agent: agent actor allowed", agentWithMode(a2aModeAnyAgent), "agent", leaderID, squadStub(false, nil), grantStub(false, nil), true},
		{"any_agent: system actor denied", agentWithMode(a2aModeAnyAgent), "system", "", squadStub(false, nil), grantStub(false, nil), false},
		{"any_agent: member actor denied", agentWithMode(a2aModeAnyAgent), "member", leaderID, squadStub(false, nil), grantStub(false, nil), false},

		// squad_leaders: only an agent actor that leads a squad.
		{"squad_leaders: leader agent allowed", agentWithMode(a2aModeSquadLeaders), "agent", leaderID, squadStub(true, nil), grantStub(false, nil), true},
		{"squad_leaders: non-leader agent denied", agentWithMode(a2aModeSquadLeaders), "agent", otherID, squadStub(false, nil), grantStub(false, nil), false},
		{"squad_leaders: system actor denied", agentWithMode(a2aModeSquadLeaders), "system", "", squadStub(true, nil), grantStub(false, nil), false},
		{"squad_leaders: empty actor id denied", agentWithMode(a2aModeSquadLeaders), "agent", "", squadStub(true, nil), grantStub(false, nil), false},
		{"squad_leaders: malformed actor id denied", agentWithMode(a2aModeSquadLeaders), "agent", "not-a-uuid", squadStub(true, nil), grantStub(false, nil), false},
		{"squad_leaders: lookup error denied", agentWithMode(a2aModeSquadLeaders), "agent", leaderID, squadStub(false, context.DeadlineExceeded), grantStub(false, nil), false},

		// specific_agents: only a whitelisted agent actor.
		{"specific_agents: whitelisted agent allowed", agentWithMode(a2aModeSpecificAgents), "agent", leaderID, squadStub(false, nil), grantStub(true, nil), true},
		{"specific_agents: non-whitelisted agent denied", agentWithMode(a2aModeSpecificAgents), "agent", otherID, squadStub(false, nil), grantStub(false, nil), false},
		{"specific_agents: system actor denied", agentWithMode(a2aModeSpecificAgents), "system", "", squadStub(false, nil), grantStub(true, nil), false},
		{"specific_agents: empty actor id denied", agentWithMode(a2aModeSpecificAgents), "agent", "", squadStub(false, nil), grantStub(true, nil), false},
		{"specific_agents: lookup error denied", agentWithMode(a2aModeSpecificAgents), "agent", leaderID, squadStub(false, nil), grantStub(false, context.DeadlineExceeded), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := a2aInvocationAllowed(ctx, tc.agent, tc.actorType, tc.actorID, wsID, tc.isLeader, tc.hasGrant)
			if got != tc.want {
				t.Fatalf("a2aInvocationAllowed(mode=%q, actorType=%s, actorID=%q) = %v, want %v",
					tc.agent.A2aInvocationMode, tc.actorType, tc.actorID, got, tc.want)
			}
		})
	}
}

// createA2ATestAgent inserts an agent with the given permission_mode and
// a2a_invocation_mode owned by testUserID, returning its id.
func createA2ATestAgent(t *testing.T, name, permissionMode, a2aMode string) string {
	t.Helper()
	ctx := context.Background()
	var id string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, permission_mode, a2a_invocation_mode,
			max_concurrent_tasks, owner_id, instructions, custom_env, custom_args
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'private', $4, $5, 1, $6, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id
	`, testWorkspaceID, name, handlerTestRuntimeID(t), permissionMode, a2aMode, testUserID).Scan(&id); err != nil {
		t.Fatalf("create a2a test agent %s: %v", name, err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, id) })
	return id
}

// createA2ATestSquad inserts a squad whose leader is the given agent, returning
// the squad id.
func createA2ATestSquad(t *testing.T, leaderID string) string {
	t.Helper()
	ctx := context.Background()
	var id string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, $2, '', $3, $4)
		RETURNING id
	`, testWorkspaceID, "a2a-test-squad-"+leaderID[:8], leaderID, testUserID).Scan(&id); err != nil {
		t.Fatalf("create a2a test squad: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, id) })
	return id
}

// addA2ATestGrant seeds a specific_agents whitelist row (agent -> grantee).
func addA2ATestGrant(t *testing.T, agentID, granteeID string) {
	t.Helper()
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_invocation_grant (agent_id, grantee_agent_id)
		VALUES ($1, $2)
	`, agentID, granteeID); err != nil {
		t.Fatalf("seed a2a grant (%s -> %s): %v", agentID, granteeID, err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_invocation_grant WHERE agent_id = $1 AND grantee_agent_id = $2`, agentID, granteeID)
	})
}

// canAgentActorInvokeWithoutOriginator loads the agent and asks canInvokeAgent
// with an AGENT actor and NO top-of-chain human originator — exactly the NEX-23
// run_only autopilot scenario this feature targets.
func canAgentActorInvokeWithoutOriginator(t *testing.T, agentID, actorID string) bool {
	t.Helper()
	agent, err := testHandler.Queries.GetAgent(context.Background(), util.MustParseUUID(agentID))
	if err != nil {
		t.Fatalf("load agent %s: %v", agentID, err)
	}
	return testHandler.canInvokeAgent(context.Background(), agent, "agent", actorID, "", testWorkspaceID)
}

// TestCanInvokeAgent_A2AInvocationModes is the integration test for the A2A
// invocation axis (NEX-24): a private target + an agent/system caller with no
// human originator, exercised across all four modes. It locks:
//   - empty mode = status-quo fail-closed (the NEX-23 scenario stays denied)
//   - any_agent admits AGENT callers only; system stays fail-closed
//   - squad_leaders admits only an actual squad leader
//   - specific_agents admits only a whitelisted agent
//   - the owner can always invoke their own agent regardless of mode
func TestCanInvokeAgent_A2AInvocationModes(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	leaderAgentID := createA2ATestAgent(t, "a2a-leader", "private", "")
	createA2ATestSquad(t, leaderAgentID)
	otherAgentID := createA2ATestAgent(t, "a2a-other", "private", "")

	// 1. Empty mode on a private target: agent-to-agent with no originator is
	//    DENIED — the pre-NEX-24 behavior, unchanged (regression lock).
	emptyModeAgent := createA2ATestAgent(t, "a2a-empty-mode", "private", "")
	if canAgentActorInvokeWithoutOriginator(t, emptyModeAgent, otherAgentID) {
		t.Error("empty mode private agent must FAIL CLOSED for an unattributed agent caller")
	}

	// 2. any_agent: agent actors are admitted; system is NOT (A2A axis only
	//    governs agent callers — system stays fail-closed on a private target).
	anyAgent := createA2ATestAgent(t, "a2a-any-agent", "private", a2aModeAnyAgent)
	if !canAgentActorInvokeWithoutOriginator(t, anyAgent, otherAgentID) {
		t.Error("any_agent must admit an unattributed agent caller")
	}
	anyAgentRow, err := testHandler.Queries.GetAgent(ctx, util.MustParseUUID(anyAgent))
	if err != nil {
		t.Fatalf("load any_agent row: %v", err)
	}
	if testHandler.canInvokeAgent(ctx, anyAgentRow, "system", "", "", testWorkspaceID) {
		t.Error("any_agent must NOT admit a system caller (fail-closed on private target)")
	}

	// 3. squad_leaders: only an actual squad leader is admitted.
	squadLeadersAgent := createA2ATestAgent(t, "a2a-squad-leaders", "private", a2aModeSquadLeaders)
	if !canAgentActorInvokeWithoutOriginator(t, squadLeadersAgent, leaderAgentID) {
		t.Error("squad_leaders must admit the squad leader agent")
	}
	if canAgentActorInvokeWithoutOriginator(t, squadLeadersAgent, otherAgentID) {
		t.Error("squad_leaders must deny a non-leader agent")
	}
	squadLeadersRow, err := testHandler.Queries.GetAgent(ctx, util.MustParseUUID(squadLeadersAgent))
	if err != nil {
		t.Fatalf("load squad_leaders row: %v", err)
	}
	if testHandler.canInvokeAgent(ctx, squadLeadersRow, "system", "", "", testWorkspaceID) {
		t.Error("squad_leaders must deny a system caller (no agent identity)")
	}

	// 4. specific_agents: only a whitelisted agent is admitted.
	specificAgent := createA2ATestAgent(t, "a2a-specific", "private", a2aModeSpecificAgents)
	addA2ATestGrant(t, specificAgent, leaderAgentID)
	if !canAgentActorInvokeWithoutOriginator(t, specificAgent, leaderAgentID) {
		t.Error("specific_agents must admit a whitelisted agent")
	}
	if canAgentActorInvokeWithoutOriginator(t, specificAgent, otherAgentID) {
		t.Error("specific_agents must deny a non-whitelisted agent")
	}
	specificRow, err := testHandler.Queries.GetAgent(ctx, util.MustParseUUID(specificAgent))
	if err != nil {
		t.Fatalf("load specific_agents row: %v", err)
	}
	if testHandler.canInvokeAgent(ctx, specificRow, "system", "", "", testWorkspaceID) {
		t.Error("specific_agents must deny a system caller (no agent identity)")
	}

	// 5. The owner may always invoke their own agent, whatever the mode.
	for _, agentID := range []string{emptyModeAgent, anyAgent, squadLeadersAgent, specificAgent} {
		row, err := testHandler.Queries.GetAgent(ctx, util.MustParseUUID(agentID))
		if err != nil {
			t.Fatalf("load agent %s: %v", agentID, err)
		}
		if !testHandler.canInvokeAgent(ctx, row, "member", testUserID, testUserID, testWorkspaceID) {
			t.Errorf("owner must always invoke their own %s agent", agentID)
		}
	}
}

// TestCanInvokeAgent_A2AEmptyModePreservesWorkspaceBroad is the regression lock
// that the NEW A2A axis does not disturb the pre-existing MUL-3963
// workspaceBroad exception: a `public_to workspace` agent stays invocable by
// unattributed agent/system callers even with the A2A mode unset — and system
// stays on that path even when an A2A mode IS set (the axis never touches
// system, CEO ruling).
func TestCanInvokeAgent_A2AEmptyModePreservesWorkspaceBroad(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// A public_to workspace agent with EMPTY A2A mode.
	wsAgentID := createA2ATestAgent(t, "a2a-ws-broad", "public_to", "")
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_invocation_target (agent_id, target_type, target_id)
		VALUES ($1, 'workspace', $2)
		ON CONFLICT (agent_id, target_type, target_id) DO NOTHING
	`, wsAgentID, testWorkspaceID); err != nil {
		t.Fatalf("seed workspace target: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_invocation_target WHERE agent_id = $1`, wsAgentID)
	})

	row, err := testHandler.Queries.GetAgent(ctx, util.MustParseUUID(wsAgentID))
	if err != nil {
		t.Fatalf("load agent: %v", err)
	}
	if !testHandler.canInvokeAgent(ctx, row, "agent", "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "", testWorkspaceID) {
		t.Error("empty A2A mode must NOT remove the workspaceBroad exception for an agent caller")
	}
	if !testHandler.canInvokeAgent(ctx, row, "system", "", "", testWorkspaceID) {
		t.Error("empty A2A mode must NOT remove the workspaceBroad exception for a system caller")
	}

	// The same public_to workspace agent WITH an A2A mode set: system must STILL
	// pass via workspaceBroad — the A2A axis has no effect on system.
	wsAnyAgentID := createA2ATestAgent(t, "a2a-ws-broad-any", "public_to", a2aModeAnyAgent)
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_invocation_target (agent_id, target_type, target_id)
		VALUES ($1, 'workspace', $2)
		ON CONFLICT (agent_id, target_type, target_id) DO NOTHING
	`, wsAnyAgentID, testWorkspaceID); err != nil {
		t.Fatalf("seed workspace target: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_invocation_target WHERE agent_id = $1`, wsAnyAgentID)
	})
	wsAnyRow, err := testHandler.Queries.GetAgent(ctx, util.MustParseUUID(wsAnyAgentID))
	if err != nil {
		t.Fatalf("load agent: %v", err)
	}
	if !testHandler.canInvokeAgent(ctx, wsAnyRow, "system", "", "", testWorkspaceID) {
		t.Error("system must keep the workspaceBroad exception even when an A2A mode is set")
	}
	// And the any_agent mode still admits an unattributed AGENT caller.
	if !testHandler.canInvokeAgent(ctx, wsAnyRow, "agent", "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "", testWorkspaceID) {
		t.Error("any_agent must admit an unattributed agent caller on a public_to workspace agent")
	}
}

// TestEnqueueMentionedAgentTasks_A2AAnyAgentAllowsUnattributedMention is the
// NEX-24 headline scenario: a run_only (no human originator) agent @mentions a
// PRIVATE agent that opted into `any_agent` — the mention must now enqueue.
// The default (empty mode) twin stays denied, locking the fail-closed
// regression.
func TestEnqueueMentionedAgentTasks_A2AAnyAgentAllowsUnattributedMention(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// Author agent (the one doing the @mention) and the two private targets.
	authorAgentID := createA2ATestAgent(t, "a2a-mention-author", "private", "")
	openAgentID := createA2ATestAgent(t, "a2a-mention-open", "private", a2aModeAnyAgent)
	closedAgentID := createA2ATestAgent(t, "a2a-mention-closed", "private", "")

	// Create an issue (no assignee so the ONLY trigger is the explicit mention).
	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number)
		VALUES ($1, 'a2a mention flow test', 'todo', 'medium', 'member', $2,
		        COALESCE((SELECT MAX(number) FROM issue WHERE workspace_id = $1), 0) + 1)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	issue, err := testHandler.Queries.GetIssue(ctx, util.MustParseUUID(issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}

	// One comment mentioning both targets: the open agent (any_agent) and the
	// closed agent (default). Multica's mention format is markdown-linked.
	mention := "[@Open](mention://agent/" + openAgentID + ") [@Closed](mention://agent/" + closedAgentID + ")"
	var commentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content)
		VALUES ($1, $2, 'agent', $3, $4)
		RETURNING id
	`, testWorkspaceID, issueID, authorAgentID, mention).Scan(&commentID); err != nil {
		t.Fatalf("create mention comment: %v", err)
	}
	comment, err := testHandler.Queries.GetComment(ctx, util.MustParseUUID(commentID))
	if err != nil {
		t.Fatalf("load comment: %v", err)
	}

	countTasks := func(agentID string) int {
		t.Helper()
		var n int
		if err := testPool.QueryRow(ctx,
			`SELECT COUNT(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2`,
			issueID, agentID,
		).Scan(&n); err != nil {
			t.Fatalf("count tasks: %v", err)
		}
		return n
	}

	// Author is an AGENT with NO human originator (commentTriggerComputeOptions
	// left empty) — the run_only autopilot shape.
	enqueueMentionedAgentTasksForTest(t, ctx, issue, comment, nil, "agent", authorAgentID)

	if n := countTasks(openAgentID); n != 1 {
		t.Errorf("any_agent private target: expected 1 enqueued task for unattributed agent mention, got %d", n)
	}
	if n := countTasks(closedAgentID); n != 0 {
		t.Errorf("empty-mode private target: expected 0 enqueued tasks (fail-closed), got %d", n)
	}
}

// TestCreateAgent_A2AInvocationModePersists verifies the create handler
// persists a2a_invocation_mode and the specific_agents whitelist, echoes them
// back on the response, and rejects an invalid mode.
func TestCreateAgent_A2AInvocationModePersists(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	granteeAgentID := createA2ATestAgent(t, "a2a-create-grantee", "private", "")

	w := httptest.NewRecorder()
	testHandler.CreateAgent(w, newRequest("POST", "/api/agents?workspace_id="+testWorkspaceID, map[string]any{
		"name":                  "a2a-create-specific",
		"runtime_id":            handlerTestRuntimeID(t),
		"a2a_invocation_mode":   "specific_agents",
		"a2a_invocation_grants": []string{granteeAgentID},
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, resp.ID) })

	if resp.A2aInvocationMode != "specific_agents" {
		t.Errorf("a2a_invocation_mode = %q, want specific_agents", resp.A2aInvocationMode)
	}
	if len(resp.A2aInvocationGrants) != 1 || resp.A2aInvocationGrants[0] != granteeAgentID {
		t.Errorf("a2a_invocation_grants = %v, want [%s]", resp.A2aInvocationGrants, granteeAgentID)
	}
	var n int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_invocation_grant WHERE agent_id = $1`, resp.ID).Scan(&n); err != nil {
		t.Fatalf("count grants: %v", err)
	}
	if n != 1 {
		t.Errorf("agent_invocation_grant rows = %d, want 1", n)
	}

	// Invalid mode is rejected on create.
	w = httptest.NewRecorder()
	testHandler.CreateAgent(w, newRequest("POST", "/api/agents?workspace_id="+testWorkspaceID, map[string]any{
		"name":                "a2a-create-invalid",
		"runtime_id":          handlerTestRuntimeID(t),
		"a2a_invocation_mode": "everyone",
	}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid a2a_invocation_mode: expected 400, got %d", w.Code)
	}
}

// TestUpdateAgent_A2AInvocationModeOwnerOnly locks the owner-only write
// contract on the A2A axis: a non-owner real change is 403, a no-op resubmit
// and non-permission edits still succeed, and the owner can set / clear /
// switch modes. Changing away from specific_agents clears the whitelist.
func TestUpdateAgent_A2AInvocationModeOwnerOnly(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	granteeAgentID := createA2ATestAgent(t, "a2a-update-grantee", "private", "")
	adminID := createPermissionTestAdmin(t, "a2a-update-admin@multica.test")

	// Owner (testUserID) creates a specific_agents agent.
	w := httptest.NewRecorder()
	testHandler.CreateAgent(w, newRequest("POST", "/api/agents?workspace_id="+testWorkspaceID, map[string]any{
		"name":                  "a2a-update-specific",
		"runtime_id":            handlerTestRuntimeID(t),
		"a2a_invocation_mode":   "specific_agents",
		"a2a_invocation_grants": []string{granteeAgentID},
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	agentID := created.ID
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, agentID) })

	put := func(actorID string, body map[string]any) int {
		rec := httptest.NewRecorder()
		r := newRequestAs(actorID, "PUT", "/api/agents/"+agentID, body)
		r = withURLParam(r, "id", agentID)
		testHandler.UpdateAgent(rec, r)
		return rec.Code
	}

	// Non-owner admin attempting a REAL A2A change → 403.
	if code := put(adminID, map[string]any{"a2a_invocation_mode": "any_agent"}); code != http.StatusForbidden {
		t.Fatalf("admin real A2A change: expected 403, got %d", code)
	}
	// Mode must be unchanged.
	if a, _ := testHandler.Queries.GetAgent(ctx, util.MustParseUUID(agentID)); a.A2aInvocationMode != "specific_agents" {
		t.Errorf("a2a_invocation_mode must be unchanged after rejected admin write, got %q", a.A2aInvocationMode)
	}
	// Non-owner no-op resubmit (PATCH-as-PUT echo) → tolerated.
	if code := put(adminID, map[string]any{
		"a2a_invocation_mode":   "specific_agents",
		"a2a_invocation_grants": []string{granteeAgentID},
	}); code != http.StatusOK {
		t.Errorf("admin no-op A2A resubmit: expected 200, got %d", code)
	}
	// Non-owner grants-only no-op resubmit (mode omitted) → tolerated, no panic.
	if code := put(adminID, map[string]any{
		"a2a_invocation_grants": []string{granteeAgentID},
	}); code != http.StatusOK {
		t.Errorf("admin grants-only no-op resubmit: expected 200, got %d", code)
	}
	// Non-owner editing a non-A2A field still works.
	if code := put(adminID, map[string]any{"description": "admin note"}); code != http.StatusOK {
		t.Errorf("admin editing other fields: expected 200, got %d", code)
	}

	// Owner switches to any_agent → whitelist is cleared.
	if code := put(testUserID, map[string]any{"a2a_invocation_mode": "any_agent"}); code != http.StatusOK {
		t.Fatalf("owner switch to any_agent: expected 200, got %d", code)
	}
	if a, _ := testHandler.Queries.GetAgent(ctx, util.MustParseUUID(agentID)); a.A2aInvocationMode != "any_agent" {
		t.Errorf("a2a_invocation_mode = %q, want any_agent", a.A2aInvocationMode)
	}
	var n int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_invocation_grant WHERE agent_id = $1`, agentID).Scan(&n); err != nil {
		t.Fatalf("count grants: %v", err)
	}
	if n != 0 {
		t.Errorf("whitelist must be cleared after leaving specific_agents, got %d rows", n)
	}

	// Owner clears the axis back to status quo ("").
	if code := put(testUserID, map[string]any{"a2a_invocation_mode": ""}); code != http.StatusOK {
		t.Fatalf("owner clear to empty: expected 200, got %d", code)
	}
	if a, _ := testHandler.Queries.GetAgent(ctx, util.MustParseUUID(agentID)); a.A2aInvocationMode != "" {
		t.Errorf("a2a_invocation_mode = %q, want empty (status quo)", a.A2aInvocationMode)
	}
}
