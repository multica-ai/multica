package handler

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func patchSecretary(t *testing.T, wsID string, body map[string]any) *testutil.Response {
	t.Helper()
	req := withURLParam(newRequest("PATCH", "/api/workspaces/"+wsID, body), "id", wsID)
	return testutil.Call(t, testHandler.UpdateWorkspace, req)
}

func TestUpdateWorkspace_SecretaryAgent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	wsID := dbfx.Workspace(t, "Secretary setting", "secretary-"+uuid.NewString())
	dbfx.Member(t, wsID, testUserID, "owner")

	agentID := dbfx.Agent(t, "Secretary agent "+uuid.NewString(), handlerTestRuntimeID(t), testutil.Cols{"workspace_id": wsID})
	// Belongs to the shared test workspace, so it is foreign to wsID.
	foreignAgent := createHandlerTestAgent(t, "Foreign secretary "+uuid.NewString(), nil)

	var resp WorkspaceResponse
	patchSecretary(t, wsID, map[string]any{"secretary_agent_id": agentID}).Want(http.StatusOK).JSON(&resp)
	if resp.SecretaryAgentID == nil || *resp.SecretaryAgentID != agentID {
		t.Fatalf("secretary_agent_id = %v, want %s", resp.SecretaryAgentID, agentID)
	}

	// An update that does not mention the field leaves it alone.
	patchSecretary(t, wsID, map[string]any{"description": "still has a secretary"}).Want(http.StatusOK).JSON(&resp)
	if resp.SecretaryAgentID == nil || *resp.SecretaryAgentID != agentID {
		t.Fatalf("an unrelated update changed the secretary: %v", resp.SecretaryAgentID)
	}

	// An agent of another workspace, a malformed id and a non-string are refused.
	patchSecretary(t, wsID, map[string]any{"secretary_agent_id": foreignAgent}).Want(http.StatusBadRequest)
	patchSecretary(t, wsID, map[string]any{"secretary_agent_id": "not-a-uuid"}).Want(http.StatusBadRequest)
	patchSecretary(t, wsID, map[string]any{"secretary_agent_id": 7}).Want(http.StatusBadRequest)

	// An archived agent is not a live agent.
	dbfx.Exec(t, `UPDATE agent SET archived_at = now() WHERE id = $1`, agentID)
	patchSecretary(t, wsID, map[string]any{"secretary_agent_id": agentID}).Want(http.StatusBadRequest)

	// null unsets it.
	patchSecretary(t, wsID, map[string]any{"secretary_agent_id": nil}).Want(http.StatusOK).JSON(&resp)
	if resp.SecretaryAgentID != nil {
		t.Fatalf("secretary_agent_id after null = %v, want nil", *resp.SecretaryAgentID)
	}
}
