package handler

import (
	"context"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestQoderDisconnectRemovesConnectionAndUnbindsAgents(t *testing.T) {
	withVCSBox(t)
	ctx := context.Background()
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM daemon_token WHERE id IN (SELECT token_id FROM cloud_agent_connection WHERE provider='qoder_cloud' AND workspace_id=$1)`, testWorkspaceID)
		_, _ = testPool.Exec(ctx, `DELETE FROM cloud_agent_connection WHERE provider='qoder_cloud' AND workspace_id=$1`, testWorkspaceID)
	})
	input := map[string]any{"name": "QCA", "base_url": qoderAPIURL, "environment_id": "env_test", "qoder_token": "fixture-token"}
	testutil.Call(t, testHandler.SaveQoderConnection, vcsHandlerRequest("PUT", "/qoder", input, "")).Want(200)
	runtimeID := dbfx.Runtime(t, "QCA", testutil.Cols{"provider": "qoder_cloud", "runtime_mode": "cloud"})
	agentID := dbfx.Agent(t, "QCA agent", runtimeID)

	deleteReq := newRequest("DELETE", "/api/runtimes/"+runtimeID, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("runtimeId", runtimeID)
	deleteReq = deleteReq.WithContext(context.WithValue(deleteReq.Context(), chi.RouteCtxKey, rctx))
	var managed map[string]any
	testutil.Call(t, testHandler.DeleteAgentRuntime, deleteReq).Want(409).JSON(&managed)
	if managed["code"] != "runtime_managed_by_connection" {
		t.Fatalf("generic delete of a Qoder runtime: %v", managed)
	}

	var changed map[string]any
	testutil.Call(t, testHandler.DisconnectQoderConnection, vcsHandlerRequest("DELETE", "/qoder", map[string]any{"expected_active_agent_ids": []string{}}, "")).Want(409).JSON(&changed)
	if changed["code"] != "runtime_delete_plan_changed" {
		t.Fatalf("unconfirmed agents should refuse with the latest plan: %v", changed)
	}

	body := map[string]any{"expected_active_agent_ids": []string{agentID}}
	testutil.Call(t, testHandler.DisconnectQoderConnection, vcsHandlerRequest("DELETE", "/qoder", body, "")).Want(200)

	var connections, runtimes, agents int
	var bound bool
	dbfx.QueryRow(t, `SELECT count(*) FROM cloud_agent_connection WHERE provider='qoder_cloud' AND workspace_id=$1`, testWorkspaceID).Scan(&connections)
	dbfx.QueryRow(t, `SELECT count(*) FROM agent_runtime WHERE id=$1`, runtimeID).Scan(&runtimes)
	dbfx.QueryRow(t, `SELECT count(*), bool_or(runtime_id IS NOT NULL) FROM agent WHERE id=$1`, agentID).Scan(&agents, &bound)
	if connections != 0 || runtimes != 0 || agents != 1 || bound {
		t.Fatalf("disconnect: connections=%d runtimes=%d agents=%d bound=%v", connections, runtimes, agents, bound)
	}
	testutil.Call(t, testHandler.DisconnectQoderConnection, vcsHandlerRequest("DELETE", "/qoder", body, "")).Want(404)
}
