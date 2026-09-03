package handler

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestListAgentsProjectsCLIForInvocableAgentOnPrivateRuntime(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeOwnerID := dbfx.User(t, "CLI Projection Runtime Owner", "cli-projection-owner-"+uuid.NewString()+"@example.com")
	dbfx.Member(t, testWorkspaceID, runtimeOwnerID, "member")
	viewerID := dbfx.User(t, "CLI Projection Viewer", "cli-projection-viewer-"+uuid.NewString()+"@example.com")
	dbfx.Member(t, testWorkspaceID, viewerID, "member")
	runtimeID := dbfx.Runtime(t, "CLI Projection Private Runtime", testutil.Cols{
		"owner_id":   runtimeOwnerID,
		"visibility": "private",
		"metadata":   testutil.Raw(`jsonb_build_object('cli_version', '0.2.99')`),
	})
	agentID := dbfx.Agent(t, "CLI Projection Shared Agent", runtimeID, testutil.Cols{
		"owner_id":        runtimeOwnerID,
		"visibility":      "private",
		"permission_mode": "public_to",
	})
	dbfx.InsertNoID(t, "agent_invocation_target", testutil.Cols{
		"agent_id":    agentID,
		"target_type": "member",
		"target_id":   viewerID,
	}, "agent_id = $1 AND target_type = 'member' AND target_id = $2", agentID, viewerID)

	var runtimes []AgentRuntimeResponse
	testutil.Call(t, testHandler.ListAgentRuntimes,
		newRequestAs(viewerID, http.MethodGet, "/api/runtimes", nil),
	).Want(http.StatusOK).JSON(&runtimes)
	for _, runtime := range runtimes {
		if runtime.ID == runtimeID {
			t.Fatalf("private runtime %s leaked into the regular member's runtime list", runtimeID)
		}
	}

	var agents []AgentResponse
	testutil.Call(t, testHandler.ListAgents,
		newRequestAs(viewerID, http.MethodGet, "/api/agents", nil),
	).Want(http.StatusOK).JSON(&agents)
	for _, agent := range agents {
		if agent.ID != agentID {
			continue
		}
		if agent.RuntimeCLIVersion == nil || *agent.RuntimeCLIVersion != "0.2.99" {
			t.Fatalf("runtime_cli_version = %#v, want 0.2.99", agent.RuntimeCLIVersion)
		}
		return
	}
	t.Fatalf("invocable agent %s missing from regular member's agent list", agentID)
}
