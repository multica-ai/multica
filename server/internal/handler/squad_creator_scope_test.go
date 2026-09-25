package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// squadScopeReq builds a request as the given user (empty = workspace owner)
// with the chi URL params the squad handlers read (workspaceId + optional id).
// The squad handlers resolve the workspace from workspaceIDFromURL, which reads
// the chi route context, not the query string — so tests must inject the params
// here rather than on the path.
func squadScopeReq(userID, method, path string, body any, params map[string]string) *http.Request {
	var req *http.Request
	if userID == "" {
		req = newRequest(method, path, body)
	} else {
		req = newRequestAs(userID, method, path, body)
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("workspaceId", testWorkspaceID)
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// createSquadAs creates a squad through the handler as the given user and
// returns the decoded response. Registers cleanup for the squad + its members.
func createSquadAs(t *testing.T, userID, name, leaderID string) SquadResponse {
	t.Helper()
	w := httptest.NewRecorder()
	r := squadScopeReq(userID, "POST", "/api/squads", map[string]any{
		"name":      name,
		"leader_id": leaderID,
	}, nil)
	testHandler.CreateSquad(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateSquad(%s): expected 201, got %d: %s", name, w.Code, w.Body.String())
	}
	var resp SquadResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad_member WHERE squad_id = $1`, resp.ID)
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, resp.ID)
	})
	return resp
}

// TestCreateSquad_PlainMemberBecomesCreator verifies the gate change: a plain
// workspace member (not owner/admin) can create a squad and is recorded as its
// creator.
func TestCreateSquad_PlainMemberBecomesCreator(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	memberID := createPlainMember(t, "squad-creator@multica.test")
	leaderID := createHandlerTestAgent(t, "squad-creator-leader", nil)

	squad := createSquadAs(t, memberID, "Member Owned Squad", leaderID)
	if squad.CreatorID != memberID {
		t.Fatalf("expected creator_id=%s, got %s", memberID, squad.CreatorID)
	}
}

// TestManageSquad_CreatorCanManageOwn verifies a creator can update, add a
// member to, and archive their own squad.
func TestManageSquad_CreatorCanManageOwn(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	memberID := createPlainMember(t, "squad-owner-manage@multica.test")
	leaderID := createHandlerTestAgent(t, "squad-owner-manage-leader", nil)
	worker := createHandlerTestAgent(t, "squad-owner-manage-worker", nil)

	squad := createSquadAs(t, memberID, "Manage Own Squad", leaderID)

	// Update name.
	w := httptest.NewRecorder()
	testHandler.UpdateSquad(w, squadScopeReq(memberID, "PATCH", "/api/squads", map[string]any{
		"name": "Renamed By Creator",
	}, map[string]string{"id": squad.ID}))
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateSquad as creator: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Add a public agent worker.
	w = httptest.NewRecorder()
	testHandler.AddSquadMember(w, squadScopeReq(memberID, "POST", "/api/squads/members", map[string]any{
		"member_type": "agent",
		"member_id":   worker,
	}, map[string]string{"id": squad.ID}))
	if w.Code != http.StatusCreated {
		t.Fatalf("AddSquadMember as creator: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Archive.
	w = httptest.NewRecorder()
	testHandler.DeleteSquad(w, squadScopeReq(memberID, "DELETE", "/api/squads", nil,
		map[string]string{"id": squad.ID}))
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteSquad as creator: expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

// TestManageSquad_StrangerMemberForbidden verifies a plain member who did not
// create the squad cannot manage it, while a workspace admin/owner still can.
func TestManageSquad_StrangerMemberForbidden(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	creatorID := createPlainMember(t, "squad-stranger-creator@multica.test")
	strangerID := createPlainMember(t, "squad-stranger-other@multica.test")
	leaderID := createHandlerTestAgent(t, "squad-stranger-leader", nil)

	squad := createSquadAs(t, creatorID, "Stranger Test Squad", leaderID)

	// Stranger member: update denied.
	w := httptest.NewRecorder()
	testHandler.UpdateSquad(w, squadScopeReq(strangerID, "PATCH", "/api/squads", map[string]any{
		"name": "Hijacked",
	}, map[string]string{"id": squad.ID}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("UpdateSquad as stranger: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	// Stranger member: archive denied.
	w = httptest.NewRecorder()
	testHandler.DeleteSquad(w, squadScopeReq(strangerID, "DELETE", "/api/squads", nil,
		map[string]string{"id": squad.ID}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("DeleteSquad as stranger: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	// Workspace owner (testUserID): update allowed — admin management unchanged.
	w = httptest.NewRecorder()
	testHandler.UpdateSquad(w, squadScopeReq("", "PATCH", "/api/squads", map[string]any{
		"name": "Renamed By Admin",
	}, map[string]string{"id": squad.ID}))
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateSquad as workspace owner: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// TestAddSquadMember_CreatorAgentAccessGate verifies the comment-#2 rule: a
// non-admin creator may add a public agent (invocable) but not a private agent
// they cannot @-trigger. The workspace owner may add the same private agent —
// admin wiring is unrestricted.
func TestAddSquadMember_CreatorAgentAccessGate(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	privateAgentID, _, memberID := privateAgentTestFixture(t)
	publicLeaderID := createHandlerTestAgent(t, "squad-gate-leader", nil)
	publicWorkerID := createHandlerTestAgent(t, "squad-gate-worker", nil)

	squad := createSquadAs(t, memberID, "Agent Gate Squad", publicLeaderID)

	// Creator adds a public (invocable) worker — allowed.
	w := httptest.NewRecorder()
	testHandler.AddSquadMember(w, squadScopeReq(memberID, "POST", "/api/squads/members", map[string]any{
		"member_type": "agent",
		"member_id":   publicWorkerID,
	}, map[string]string{"id": squad.ID}))
	if w.Code != http.StatusCreated {
		t.Fatalf("AddSquadMember public agent: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Creator adds a private agent they cannot invoke — denied.
	w = httptest.NewRecorder()
	testHandler.AddSquadMember(w, squadScopeReq(memberID, "POST", "/api/squads/members", map[string]any{
		"member_type": "agent",
		"member_id":   privateAgentID,
	}, map[string]string{"id": squad.ID}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("AddSquadMember private agent as creator: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	// Workspace owner adds the same private agent — allowed (admin unchanged).
	w = httptest.NewRecorder()
	testHandler.AddSquadMember(w, squadScopeReq("", "POST", "/api/squads/members", map[string]any{
		"member_type": "agent",
		"member_id":   privateAgentID,
	}, map[string]string{"id": squad.ID}))
	if w.Code != http.StatusCreated {
		t.Fatalf("AddSquadMember private agent as owner: expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateSquad_CreatorPrivateLeaderForbidden verifies a non-admin cannot
// create a squad led by a private agent they cannot @-trigger.
func TestCreateSquad_CreatorPrivateLeaderForbidden(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	privateAgentID, _, memberID := privateAgentTestFixture(t)

	w := httptest.NewRecorder()
	r := squadScopeReq(memberID, "POST", "/api/squads", map[string]any{
		"name":      "Private Leader Squad",
		"leader_id": privateAgentID,
	}, nil)
	testHandler.CreateSquad(w, r)
	if w.Code != http.StatusForbidden {
		// Nothing should have been created; if it slipped through, clean up.
		if w.Code == http.StatusCreated {
			var resp SquadResponse
			if json.NewDecoder(w.Body).Decode(&resp) == nil {
				testPool.Exec(context.Background(), `DELETE FROM squad_member WHERE squad_id = $1`, resp.ID)
				testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, resp.ID)
			}
		}
		t.Fatalf("CreateSquad with private leader: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}
