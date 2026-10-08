package handler

import (
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// assertDenialReason decodes a rejection body and compares its `error` sentence
// to the one the CALLER states it expects, then checks a property that holds for
// every invoke denial: the sentence must not name the target's permission mode.
//
// The expected sentence stays an argument on purpose — the test making the claim
// keeps it. What the helper contributes is the leak scan, which is the same
// property in every case and is exactly what a status-only assertion missed:
// before MUL-6380 these endpoints answered "cannot assign to private agent",
// disclosing the mode to a caller just told they may not use the target, and
// mislabelling a `public_to` agent scoped to specific people as private.
func assertDenialReason(t *testing.T, resp *testutil.Response, want string) {
	t.Helper()
	var body struct {
		Error string `json:"error"`
	}
	resp.JSON(&body)
	if body.Error != want {
		t.Fatalf("denial reason = %q, want %q", body.Error, want)
	}
	for _, mode := range []string{"private", "public_to", "allow-list"} {
		if strings.Contains(strings.ToLower(body.Error), mode) {
			t.Fatalf("denial reason names the target's permission mode (%q): %q", mode, body.Error)
		}
	}
}

// TestAssignAgent_DenialReasonNamesPermissionNotMode pins the response BODY of
// the assignment invoke gate across every principal that can be refused by it.
// The sibling tests in agent_access_test.go / squad_private_leader_test.go pin
// the status code; a 403 alone does not stop the reason sentence from drifting
// back into describing the target instead of the caller's missing permission.
func TestAssignAgent_DenialReasonNamesPermissionNotMode(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	privateAgentID, agentOwnerID, plainMemberID := privateAgentTestFixture(t)
	const wantAgentReason = "you do not have permission to assign work to this agent"

	assignBody := func(agentID string) map[string]any {
		return map[string]any{
			"title":         "invoke-denial body test",
			"status":        "todo",
			"priority":      "medium",
			"assignee_type": "agent",
			"assignee_id":   agentID,
		}
	}

	// A workspace OWNER who does not own the agent. Management access is not
	// invoke access (MUL-3963), and the refusal must not tell them why in terms
	// of the agent's configuration.
	t.Run("workspace owner is refused without naming the mode", func(t *testing.T) {
		resp := testutil.Call(t, testHandler.CreateIssue,
			newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, assignBody(privateAgentID)),
		).Want(http.StatusForbidden)
		assertDenialReason(t, resp, wantAgentReason)
	})

	t.Run("plain member is refused without naming the mode", func(t *testing.T) {
		resp := testutil.Call(t, testHandler.CreateIssue,
			newRequestAs(plainMemberID, "POST", "/api/issues?workspace_id="+testWorkspaceID, assignBody(privateAgentID)),
		).Want(http.StatusForbidden)
		assertDenialReason(t, resp, wantAgentReason)
	})

	// An AGENT actor with no resolvable human originator (no X-Task-ID). A2A is
	// judged by the top of the chain, so this fails closed like any other
	// principal — there is no unconditional agent-to-agent bypass.
	t.Run("agent actor with no originator is refused without naming the mode", func(t *testing.T) {
		callerAgentID := createHandlerTestAgent(t, "assign-denial-caller-agent", nil)
		req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, assignBody(privateAgentID))
		req.Header.Set("X-Agent-ID", callerAgentID)
		resp := testutil.Call(t, testHandler.CreateIssue, req).Want(http.StatusForbidden)
		assertDenialReason(t, resp, wantAgentReason)
	})

	// A `public_to` agent whose allow-list names someone else. This is the case
	// the old wording got factually wrong: the agent is not private, so calling
	// it that told the caller something untrue about the target.
	t.Run("public_to non-target member is refused without calling the agent private", func(t *testing.T) {
		scopedAgentID := dbfx.Agent(t, "assign-denial-scoped-agent", handlerTestRuntimeID(t), testutil.Cols{
			"visibility":      "private",
			"permission_mode": "public_to",
			"owner_id":        agentOwnerID,
			"instructions":    "",
			"custom_env":      testutil.Raw("'{}'::jsonb"),
			"custom_args":     testutil.Raw("'[]'::jsonb"),
		})
		// Granted to the agent owner only — the plain member is deliberately
		// NOT on the list.
		dbfx.Exec(t, `
			INSERT INTO agent_invocation_target (agent_id, target_type, target_id)
			VALUES ($1, 'member', $2)
			ON CONFLICT (agent_id, target_type, target_id) DO NOTHING
		`, scopedAgentID, agentOwnerID)
		// No FK/cascade by project rule, so the grant outlives the agent row
		// dbfx deletes — drop it explicitly.
		dbfx.Cleanup(t, `DELETE FROM agent_invocation_target WHERE agent_id = $1`, scopedAgentID)

		resp := testutil.Call(t, testHandler.CreateIssue,
			newRequestAs(plainMemberID, "POST", "/api/issues?workspace_id="+testWorkspaceID, assignBody(scopedAgentID)),
		).Want(http.StatusForbidden)
		assertDenialReason(t, resp, wantAgentReason)
	})

	// The squad branch has its own sentence, and must not disclose the leader's
	// mode either — "this squad" is all the caller gets.
	t.Run("private-leader squad is refused without naming the leader's mode", func(t *testing.T) {
		squadID := dbfx.Squad(t, "Assign Denial Private Leader", privateAgentID)
		resp := testutil.Call(t, testHandler.CreateIssue,
			newRequestAs(plainMemberID, "POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
				"title":         "invoke-denial squad body test",
				"assignee_type": "squad",
				"assignee_id":   squadID,
			}),
		).Want(http.StatusForbidden)
		assertDenialReason(t, resp, "you do not have permission to assign work to this squad")
	})
}
