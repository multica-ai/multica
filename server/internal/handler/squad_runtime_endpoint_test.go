package handler

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func setSquadRuntimeRequest(workspaceID, squadID string, body map[string]any) *http.Request {
	// Production resolves the workspace from middleware; a direct handler call
	// reads the workspaceId route param instead.
	req := withURLParams(newRequest("PUT", "/api/squads/"+squadID+"/runtime", body), "id", squadID, "workspaceId", workspaceID)
	req.Header.Set("X-Workspace-ID", workspaceID)
	return req
}

func resultIDs(results []SquadRuntimeAgentResult) map[string]string {
	ids := make(map[string]string, len(results))
	for _, r := range results {
		ids[r.AgentID] = r.Reason
	}
	return ids
}

// A squad-wide change moves the agents it can, leaves the one already on the
// runtime alone, and reports nothing for agents it moved.
func TestSetSquadRuntime_MovesEligibleAgentsAndLeavesTheRest(t *testing.T) {
	e := newDigestEnv(t)
	squad := e.squad(t, "Runtime batch")
	target := dbfx.Runtime(t, "Target "+uuid.NewString(), testutil.Cols{
		"workspace_id": e.WorkspaceID, "provider": "codex", "visibility": "public", "owner_id": testUserID,
	})
	ownedAgent := dbfx.Agent(t, "Owned "+uuid.NewString(), e.RuntimeID, testutil.Cols{"workspace_id": e.WorkspaceID, "owner_id": testUserID})
	alreadyThere := dbfx.Agent(t, "Already "+uuid.NewString(), target, testutil.Cols{"workspace_id": e.WorkspaceID, "owner_id": testUserID})
	for _, id := range []string{e.AgentID, ownedAgent, alreadyThere} {
		dbfx.SquadMember(t, squad, "agent", id)
	}

	var out SetSquadRuntimeResponse
	testutil.Call(t, testHandler.SetSquadRuntime, asUser(setSquadRuntimeRequest(e.WorkspaceID, squad,
		map[string]any{"runtime_id": target}), testUserID)).Want(http.StatusOK).JSON(&out)

	updated := resultIDs(out.Updated)
	for _, id := range []string{e.AgentID, ownedAgent} {
		if _, ok := updated[id]; !ok {
			t.Errorf("agent %s should move: %+v", id, out.Updated)
		}
	}
	if _, ok := resultIDs(out.Unchanged)[alreadyThere]; !ok {
		t.Errorf("agent already on the runtime should be unchanged: %+v", out.Unchanged)
	}
	if len(out.Skipped) != 0 {
		t.Errorf("nothing should be skipped: %+v", out.Skipped)
	}
}

// An agent whose owner cannot use a private runtime is reported and left on its
// current runtime; the rest of the batch still moves.
func TestSetSquadRuntime_SkipsAgentsOwnersCannotUseTheRuntime(t *testing.T) {
	e := newDigestEnv(t)
	squad := e.squad(t, "Runtime access")
	privateTarget := dbfx.Runtime(t, "Private "+uuid.NewString(), testutil.Cols{
		"workspace_id": e.WorkspaceID, "provider": "codex", "visibility": "private", "owner_id": testUserID,
	})
	otherUser := dbfx.User(t, "Other owner", "other-"+uuid.NewString()+"@example.com")
	blocked := dbfx.Agent(t, "Blocked "+uuid.NewString(), e.RuntimeID, testutil.Cols{"workspace_id": e.WorkspaceID, "owner_id": otherUser})
	for _, id := range []string{e.AgentID, blocked} {
		dbfx.SquadMember(t, squad, "agent", id)
	}

	var out SetSquadRuntimeResponse
	testutil.Call(t, testHandler.SetSquadRuntime, asUser(setSquadRuntimeRequest(e.WorkspaceID, squad,
		map[string]any{"runtime_id": privateTarget}), testUserID)).Want(http.StatusOK).JSON(&out)

	if reasons := resultIDs(out.Skipped); reasons[blocked] != "the agent owner has no access to this runtime" {
		t.Fatalf("expected the other owner's agent skipped with the access reason, got %+v", out.Skipped)
	}
	if _, ok := resultIDs(out.Updated)[e.AgentID]; !ok {
		t.Errorf("the leader should still move: %+v", out.Updated)
	}
}

func TestSetSquadRuntime_RefusesQoderAndUnknownRuntimes(t *testing.T) {
	e := newDigestEnv(t)
	squad := e.squad(t, "Runtime refusals")
	qoder := dbfx.Runtime(t, "Qoder "+uuid.NewString(), testutil.Cols{
		"workspace_id": e.WorkspaceID, "provider": "qoder_cloud", "visibility": "public", "owner_id": testUserID,
	})
	for _, runtimeID := range []string{qoder, uuid.NewString()} {
		testutil.Call(t, testHandler.SetSquadRuntime, asUser(setSquadRuntimeRequest(e.WorkspaceID, squad,
			map[string]any{"runtime_id": runtimeID}), testUserID)).Want(http.StatusBadRequest)
	}
}
