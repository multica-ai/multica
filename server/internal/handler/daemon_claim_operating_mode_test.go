package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestClaimTaskByRuntimeCarriesOperatingMode(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Operating mode claim runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Operating mode claim agent")
	dbfx.Exec(t, `UPDATE agent SET operating_mode = 'hybrid' WHERE id = $1`, agentID)
	taskID := createDispatchedClaimFixtureTask(t, ctx, agentID, runtimeID, issueID, "120 seconds", false)

	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil,
		testWorkspaceID, "operating-mode-claim")
	req = withURLParam(req, "runtimeId", runtimeID)
	w := testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK)

	var response struct {
		Task *struct {
			ID    string `json:"id"`
			Agent *struct {
				OperatingMode string `json:"operating_mode"`
			} `json:"agent"`
		} `json:"task"`
	}
	w.JSON(&response)
	if response.Task == nil || response.Task.ID != taskID {
		t.Fatalf("claimed task = %+v, want %s", response.Task, taskID)
	}
	if response.Task.Agent == nil || response.Task.Agent.OperatingMode != "hybrid" {
		t.Fatalf("claimed agent = %+v, want hybrid operating mode", response.Task.Agent)
	}
}
