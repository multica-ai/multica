package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// claimAgentMcpConfigForTest runs one claim for the given runtime and returns
// the mcp_config the daemon would receive for the claimed task's agent.
func claimAgentMcpConfigForTest(t *testing.T, runtimeID string) json.RawMessage {
	t.Helper()

	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "ws-mcp-daemon")
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var claimResp struct {
		Task *AgentTaskResponse `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &claimResp); err != nil {
		t.Fatalf("decode claim: %v", err)
	}
	if claimResp.Task == nil || claimResp.Task.Agent == nil {
		t.Fatalf("missing task agent in claim response: %s", w.Body.String())
	}
	return claimResp.Task.Agent.McpConfig
}

// setupWorkspaceMcpClaimFixture creates a runtime, an agent with the given
// saved mcp_config, and one queued task ready to claim.
func setupWorkspaceMcpClaimFixture(t *testing.T, ctx context.Context, name, agentMcpConfig string) string {
	t.Helper()

	runtimeID := createClaimReclaimRuntime(t, ctx, name+" runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, name+" agent")

	var stored []byte
	if agentMcpConfig != "" {
		stored = []byte(agentMcpConfig)
	}
	if _, err := testPool.Exec(ctx, `UPDATE agent SET mcp_config = $1 WHERE id = $2`, stored, agentID); err != nil {
		t.Fatalf("setup: set agent mcp_config: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, 'queued', 0)
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("setup: create task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	return runtimeID
}

// The claim payload is where the workspace layer actually reaches an agent, so
// assert the resolved document on the wire — not just the merge helper.
func TestClaimTaskByRuntime_InheritsWorkspaceMcpConfig(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	setWorkspaceMcpConfigForTest(t, `{"mcpServers":{"shared":{"url":"https://shared.example"}}}`)
	runtimeID := setupWorkspaceMcpClaimFixture(t, ctx, "ws-mcp-inherit", "")

	servers := decodeServers(t, claimAgentMcpConfigForTest(t, runtimeID))
	if len(servers) != 1 || servers["shared"] == nil {
		t.Fatalf("agent with no saved config should inherit the shared server, got %v", serverNames(servers))
	}
}

func TestClaimTaskByRuntime_MergesWorkspaceAndAgentMcpConfig(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	setWorkspaceMcpConfigForTest(t, `{"mcpServers":{"shared":{"url":"https://shared.example"},"linear":{"url":"https://ws-linear.example"}}}`)
	runtimeID := setupWorkspaceMcpClaimFixture(t, ctx, "ws-mcp-merge",
		`{"mcpServers":{"private":{"url":"https://private.example"},"linear":{"url":"https://agent-linear.example"}}}`)

	servers := decodeServers(t, claimAgentMcpConfigForTest(t, runtimeID))
	if len(servers) != 3 {
		t.Fatalf("server set = %v, want shared/linear/private", serverNames(servers))
	}
	linear, _ := servers["linear"].(map[string]any)
	if linear["url"] != "https://agent-linear.example" {
		t.Errorf("agent must win the name collision, got %v", linear["url"])
	}
}

func TestClaimTaskByRuntime_ZeroServerAgentIgnoresWorkspaceMcpConfig(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	setWorkspaceMcpConfigForTest(t, `{"mcpServers":{"shared":{"url":"https://shared.example"}}}`)
	runtimeID := setupWorkspaceMcpClaimFixture(t, ctx, "ws-mcp-optout", `{}`)

	mcpConfig := claimAgentMcpConfigForTest(t, runtimeID)
	if servers := decodeServers(t, mcpConfig); len(servers) != 0 {
		t.Fatalf("opted-out agent received %v", serverNames(servers))
	}
	// Still managed-but-empty, which is what keeps the runtime's own MCP
	// servers suppressed for this agent.
	if string(mcpConfig) != `{}` {
		t.Fatalf("mcp_config = %s, want the agent's own empty document", mcpConfig)
	}
}
