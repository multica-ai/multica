package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestManagedMcpClaimHistoryThroughRouter(t *testing.T) {
	ctx := context.Background()
	fx := workspaceFixture(t)
	runtimeID := fx.Runtime(t, "MCP history runtime", testutil.Cols{"provider": "integration_test_runtime"})
	agentID := fx.Agent(t, "MCP history agent", runtimeID, testutil.Cols{
		"visibility": "workspace", "permission_mode": "public_to",
		"mcp_config": []byte(`{"mcpServers":{"own":{"url":"https://own.example/ROUTER_SECRET_CANARY"}}}`),
	})
	fx.InsertNoID(t, "agent_invocation_target", testutil.Cols{"agent_id": agentID, "target_type": "workspace", "target_id": testWorkspaceID}, "agent_id=$1", agentID)
	issueID := fx.Issue(t, "MCP source history", testutil.Cols{"status": "todo"})
	serverID := fx.Insert(t, "workspace_mcp_server", testutil.Cols{"workspace_id": testWorkspaceID, "name": "workspace", "config": []byte(`{"url":"https://workspace.example","headers":{"Authorization":"ROUTER_SECRET_CANARY"}}`)})
	fx.InsertNoID(t, "agent_mcp_server", testutil.Cols{"agent_id": agentID, "server_id": serverID}, "agent_id=$1", agentID)
	taskID := fx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID})
	claimResp := authRequest(t, "POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil)
	if claimResp.StatusCode != 200 {
		body, _ := io.ReadAll(claimResp.Body)
		claimResp.Body.Close()
		t.Fatalf("normal authorized claim: %d %s", claimResp.StatusCode, body)
	}
	var claim struct {
		Task *handler.AgentTaskResponse `json:"task"`
	}
	readJSON(t, claimResp, &claim)
	if claim.Task == nil || claim.Task.ID != taskID || claim.Task.ManagedMcpSources == nil || len(*claim.Task.ManagedMcpSources) != 2 {
		t.Fatalf("claim source snapshot missing")
	}
	fx.Exec(t, `UPDATE agent SET mcp_config='{}' WHERE id=$1`, agentID)
	fx.Exec(t, `DELETE FROM agent_mcp_server WHERE agent_id=$1`, agentID)
	response := authRequest(t, "GET", "/api/issues/"+issueID+"/task-runs", nil)
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || strings.Contains(string(body), "ROUTER_SECRET_CANARY") || strings.Contains(string(body), "https:") {
		t.Fatalf("unsafe ordinary response: %d %s", response.StatusCode, body)
	}
	var runs []handler.AgentTaskResponse
	if err := json.Unmarshal(body, &runs); err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].ManagedMcpSources == nil || len(*runs[0].ManagedMcpSources) != 2 {
		t.Fatal("historical sources disappeared after configuration edits")
	}
	sources := *runs[0].ManagedMcpSources
	if sources[0].Name != "own" || sources[0].Source != "agent" || sources[1].Name != "workspace" || sources[1].Source != "workspace" {
		t.Fatalf("unexpected historical sources: %+v", sources)
	}
	memberID := fx.User(t, "Source history reader", "mcp-history-reader@example.test")
	fx.Member(t, testWorkspaceID, memberID, "member")
	memberToken, err := generateTestJWT(memberID, "mcp-history-reader@example.test", "Source history reader")
	if err != nil {
		t.Fatal(err)
	}
	memberReq, err := http.NewRequest("GET", testServer.URL+"/api/issues/"+issueID+"/task-runs", nil)
	if err != nil {
		t.Fatal(err)
	}
	memberReq.Header.Set("Authorization", "Bearer "+memberToken)
	memberReq.Header.Set("X-Workspace-ID", testWorkspaceID)
	memberResponse, err := http.DefaultClient.Do(memberReq)
	if err != nil {
		t.Fatal(err)
	}
	memberBody, err := io.ReadAll(memberResponse.Body)
	memberResponse.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if memberResponse.StatusCode != 200 || !strings.Contains(string(memberBody), "managed_mcp_sources") || strings.Contains(string(memberBody), "ROUTER_SECRET_CANARY") {
		t.Fatalf("ordinary authorized member history: %d %s", memberResponse.StatusCode, memberBody)
	}

	// The workspace selected by an ordinary member request remains the gate.
	foreign := fx.Workspace(t, "Foreign source workspace", "foreign-mcp-source-workspace")
	req, err := http.NewRequest("GET", testServer.URL+"/api/issues/"+issueID+"/task-runs", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("X-Workspace-ID", foreign)
	denied, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer denied.Body.Close()
	deniedBody, _ := io.ReadAll(denied.Body)
	if denied.StatusCode < 400 || strings.Contains(string(deniedBody), "managed_mcp_sources") {
		t.Fatalf("cross-workspace read was allowed: %d", denied.StatusCode)
	}
	var stored []byte
	if err := testPool.QueryRow(ctx, `SELECT managed_mcp_snapshot FROM agent_task_queue WHERE id=$1`, taskID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stored), "ROUTER_SECRET_CANARY") {
		t.Fatal("secret persisted in source snapshot")
	}
}
