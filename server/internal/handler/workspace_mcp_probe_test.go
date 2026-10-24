package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func mcpProbeServer(t *testing.T, name string) string {
	t.Helper()
	return dbfx.Insert(t, "workspace_mcp_server", testutil.Cols{
		"workspace_id": testWorkspaceID,
		"name":         name,
		"config":       testutil.Raw(`'{"url":"https://linear.example","headers":{"Authorization":"Bearer ` + workspaceMcpTestSecret + `"}}'::jsonb`),
	})
}

func capableRuntime(t *testing.T, name string) string {
	t.Helper()
	return dbfx.Runtime(t, name, testutil.Cols{
		"daemon_id": "probe-daemon",
		"metadata":  testutil.Raw(`'{"capabilities":["` + protocol.DaemonCapabilityMcpProbeV1 + `"]}'::jsonb`),
	})
}

func TestProbeWorkspaceMcpServer_DeniesMember(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	serverID := mcpProbeServer(t, "probe-member")
	_, _, memberID := runtimeVisibilityFixture(t)

	var errBody map[string]any
	req := testutil.WithURLParams(
		newRequestAs(memberID, http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/mcp-servers/"+serverID+"/probe", map[string]any{}),
		"id", testWorkspaceID, "serverId", serverID,
	)
	testutil.Call(t, testHandler.ProbeWorkspaceMcpServer, req).Want(http.StatusForbidden).JSON(&errBody)
	if got, _ := errBody["error"].(string); got == "" {
		t.Fatalf("expected an error message, got %#v", errBody)
	}
}

func TestProbeWorkspaceMcpServer_DeniesAgentActor(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	serverID := mcpProbeServer(t, "probe-agent")
	caller := createHandlerTestAgent(t, "ws-mcp-probe-actor", nil)
	taskID := insertHandlerTestTask(t, caller)

	req := testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/mcp-servers/"+serverID+"/probe", map[string]any{}),
		"id", testWorkspaceID, "serverId", serverID,
	)
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Agent-ID", caller)
	req.Header.Set("X-Task-ID", taskID)
	testutil.Call(t, testHandler.ProbeWorkspaceMcpServer, req).Want(http.StatusForbidden)
}

func TestProbeWorkspaceMcpServer_UnknownServer(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	missing := "00000000-0000-4000-8000-000000000099"
	req := testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/mcp-servers/"+missing+"/probe", map[string]any{}),
		"id", testWorkspaceID, "serverId", missing,
	)
	testutil.Call(t, testHandler.ProbeWorkspaceMcpServer, req).Want(http.StatusNotFound)
}

func TestProbeWorkspaceMcpServer_MultipleOwnedRuntimesRequireID(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	serverID := mcpProbeServer(t, "probe-multi")
	capableRuntime(t, "probe-extra-rt")

	req := testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/mcp-servers/"+serverID+"/probe", map[string]any{}),
		"id", testWorkspaceID, "serverId", serverID,
	)
	body := testutil.Call(t, testHandler.ProbeWorkspaceMcpServer, req).Want(http.StatusConflict).Map()
	if got, _ := body["error"].(string); got != "runtime_id is required when more than one of your runtimes is online" {
		t.Fatalf("error = %q", got)
	}
}

func TestProbeWorkspaceMcpServer_UnsupportedDaemon(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	serverID := mcpProbeServer(t, "probe-old-daemon")
	req := testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/mcp-servers/"+serverID+"/probe", map[string]any{
			"runtime_id": testRuntimeID,
		}),
		"id", testWorkspaceID, "serverId", serverID,
	)
	body := testutil.Call(t, testHandler.ProbeWorkspaceMcpServer, req).Want(http.StatusConflict).Map()
	if got, _ := body["error"].(string); !strings.Contains(got, "does not support MCP probes") {
		t.Fatalf("error = %q", got)
	}

	_, _, raw := listWorkspaceMcpServersForTest(t, nil)
	if strings.Contains(raw, workspaceMcpTestSecret) {
		t.Fatalf("list echoed a secret: %s", raw)
	}
}

func TestProbeWorkspaceMcpServer_RejectsRuntimeOwnedBySomeoneElse(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	serverID := mcpProbeServer(t, "probe-foreign")
	runtimeID, _, _ := runtimeVisibilityFixture(t)
	dbfx.Exec(t, `UPDATE agent_runtime SET visibility = 'public', metadata = $2::jsonb WHERE id = $1`,
		runtimeID, []byte(`{"capabilities":["`+protocol.DaemonCapabilityMcpProbeV1+`"]}`))

	req := testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/mcp-servers/"+serverID+"/probe", map[string]any{
			"runtime_id": runtimeID,
		}),
		"id", testWorkspaceID, "serverId", serverID,
	)
	testutil.Call(t, testHandler.ProbeWorkspaceMcpServer, req).Want(http.StatusNotFound)
}

func TestProbeWorkspaceMcpServer_NoOnlineRuntime(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	wsID := dbfx.Workspace(t, "Empty Probe WS", "empty-probe-ws")
	dbfx.Member(t, wsID, testUserID, "owner")
	serverID := dbfx.Insert(t, "workspace_mcp_server", testutil.Cols{
		"workspace_id": wsID,
		"name":         "lonely",
		"config":       testutil.Raw(`'{"url":"https://lonely.example"}'::jsonb`),
	})

	req := testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/workspaces/"+wsID+"/mcp-servers/"+serverID+"/probe", map[string]any{}),
		"id", wsID, "serverId", serverID,
	)
	req.Header.Set("X-Workspace-ID", wsID)
	body := testutil.Call(t, testHandler.ProbeWorkspaceMcpServer, req).Want(http.StatusServiceUnavailable).Map()
	if got, _ := body["error"].(string); got != "no online runtime you own" {
		t.Fatalf("error = %q", got)
	}
}

func TestProbeWorkspaceMcpServer_EnqueuesAndDaemonJobIsSecret(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	serverID := mcpProbeServer(t, "probe-enqueue")
	rtID := capableRuntime(t, "probe-capable")

	req := testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/mcp-servers/"+serverID+"/probe", map[string]any{
			"runtime_id": rtID,
		}),
		"id", testWorkspaceID, "serverId", serverID,
	)
	var queued McpProbeRequest
	testutil.Call(t, testHandler.ProbeWorkspaceMcpServer, req).Want(http.StatusOK).JSON(&queued)
	if queued.Status != McpProbePending || queued.RuntimeID != rtID || queued.ServerID != serverID {
		t.Fatalf("queued = %+v", queued)
	}

	userJob := testutil.WithURLParams(
		newRequest(http.MethodGet, "/api/daemon/runtimes/"+rtID+"/mcp-probes/"+queued.ID, nil),
		"runtimeId", rtID, "requestId", queued.ID,
	)
	testutil.Call(t, testHandler.GetDaemonMcpProbeJob, userJob).WantOneOf(http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound)
}

func TestGetDaemonMcpProbeJob_AllowsDaemonToken(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	serverID := mcpProbeServer(t, "probe-daemon-job")
	rtID := capableRuntime(t, "probe-daemon-capable")

	enqueue := testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/mcp-servers/"+serverID+"/probe", map[string]any{
			"runtime_id": rtID,
		}),
		"id", testWorkspaceID, "serverId", serverID,
	)
	var queued McpProbeRequest
	testutil.Call(t, testHandler.ProbeWorkspaceMcpServer, enqueue).Want(http.StatusOK).JSON(&queued)

	daemonJob := testutil.WithURLParams(
		newDaemonTokenRequest(http.MethodGet, "/api/daemon/runtimes/"+rtID+"/mcp-probes/"+queued.ID, nil, testWorkspaceID, "probe-daemon"),
		"runtimeId", rtID, "requestId", queued.ID,
	)
	var job struct {
		RequestID  string         `json:"request_id"`
		ServerName string         `json:"server_name"`
		Config     map[string]any `json:"config"`
	}
	resp := testutil.Call(t, testHandler.GetDaemonMcpProbeJob, daemonJob).Want(http.StatusOK).JSON(&job)
	if job.RequestID != queued.ID || job.ServerName != "probe-daemon-job" {
		t.Fatalf("job = %+v", job)
	}
	if !strings.Contains(resp.Text(), workspaceMcpTestSecret) {
		t.Fatalf("daemon job omitted the write-only secret: %s", resp.Text())
	}
}

func TestInMemoryMcpProbeStore_PendingTimeout(t *testing.T) {
	store := NewInMemoryMcpProbeStore()
	req, err := store.Create(context.Background(), McpProbeCreate{
		WorkspaceID: "ws", ServerID: "srv", RuntimeID: "rt", RuntimeName: "box",
	})
	if err != nil {
		t.Fatal(err)
	}
	req.CreatedAt = req.CreatedAt.Add(-mcpProbePendingTimeout - 1)
	store.mu.Lock()
	store.requests[req.ID].CreatedAt = req.CreatedAt
	store.mu.Unlock()
	got, err := store.Get(context.Background(), req.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != McpProbeTimeout || got.ErrorCode != protocol.McpProbeCodeTimeout {
		t.Fatalf("got %+v", got)
	}
}

func TestSanitizeMcpProbeError_StripsNewlines(t *testing.T) {
	got := sanitizeMcpProbeError("failed env=SECRET=tok\nrest")
	if got != "probe failed" {
		t.Fatalf("leaked trailing dump: %q", got)
	}
}

func TestMcpProbe_HeartbeatReportAndPoll(t *testing.T) {
	serverID := mcpProbeServer(t, "probe-roundtrip")
	runtimeID := capableRuntime(t, "probe-roundtrip-runtime")
	enqueue := testutil.WithURLParams(newRequest(http.MethodPost, "/probe", map[string]any{
		"runtime_id": runtimeID,
	}), "id", testWorkspaceID, "serverId", serverID)
	var queued McpProbeRequest
	testutil.Call(t, testHandler.ProbeWorkspaceMcpServer, enqueue).Want(http.StatusOK).JSON(&queued)
	ack, _, err := testHandler.processHeartbeat(context.Background(), runtimeID, false)
	if err != nil {
		t.Fatal(err)
	}
	if ack.PendingMcpProbe == nil || ack.PendingMcpProbe.ID != queued.ID {
		t.Fatalf("heartbeat did not claim probe: %+v", ack.PendingMcpProbe)
	}
	report := testutil.WithURLParams(newDaemonTokenRequest(http.MethodPost, "/result", map[string]any{
		"status": "completed", "elapsed_ms": 123,
		"tools": []string{"search", "search", "env=SECRET=tok\nrest", "fetch"},
	}, testWorkspaceID, "probe-daemon"), "runtimeId", runtimeID, "requestId", queued.ID)
	testutil.Call(t, testHandler.ReportDaemonMcpProbeResult, report).Want(http.StatusOK)
	poll := testutil.WithURLParams(newRequest(http.MethodGet, "/poll", nil),
		"id", testWorkspaceID, "serverId", serverID, "requestId", queued.ID)
	var result McpProbeRequest
	resp := testutil.Call(t, testHandler.GetWorkspaceMcpProbe, poll).Want(http.StatusOK).JSON(&result)
	if result.Status != McpProbeCompleted || result.ElapsedMs != 123 || strings.Join(result.Tools, ",") != "search,fetch" {
		t.Fatalf("unexpected probe result: %+v", result)
	}
	if strings.Contains(resp.Text(), workspaceMcpTestSecret) || strings.Contains(resp.Text(), "SECRET") {
		t.Fatal("probe result leaked configuration or diagnostics")
	}

	otherDaemon := testutil.WithURLParams(newDaemonTokenRequest(http.MethodGet, "/job", nil,
		testWorkspaceID, "another-daemon"), "runtimeId", runtimeID, "requestId", queued.ID)
	testutil.Call(t, testHandler.GetDaemonMcpProbeJob, otherDaemon).Want(http.StatusNotFound)
}

func TestMcpProbe_ConfigChangeInvalidatesQueuedJobAndResult(t *testing.T) {
	serverID := mcpProbeServer(t, "probe-config-change")
	runtimeID := capableRuntime(t, "probe-config-change-runtime")
	enqueue := testutil.WithURLParams(newRequest(http.MethodPost, "/probe", map[string]any{
		"runtime_id": runtimeID,
	}), "id", testWorkspaceID, "serverId", serverID)
	var queued McpProbeRequest
	testutil.Call(t, testHandler.ProbeWorkspaceMcpServer, enqueue).Want(http.StatusOK).JSON(&queued)
	dbfx.Exec(t, `UPDATE workspace_mcp_server SET updated_at = NOW() + INTERVAL '1 second' WHERE id = $1`, serverID)
	job := testutil.WithURLParams(newDaemonTokenRequest(http.MethodGet, "/job", nil,
		testWorkspaceID, "probe-daemon"), "runtimeId", runtimeID, "requestId", queued.ID)
	testutil.Call(t, testHandler.GetDaemonMcpProbeJob, job).Want(http.StatusConflict)
	poll := testutil.WithURLParams(newRequest(http.MethodGet, "/poll", nil),
		"id", testWorkspaceID, "serverId", serverID, "requestId", queued.ID)
	testutil.Call(t, testHandler.GetWorkspaceMcpProbe, poll).Want(http.StatusConflict)
}

func TestInMemoryMcpProbeStore_ReturnsSnapshotsAndKeepsTerminalResult(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryMcpProbeStore()
	created, err := store.Create(ctx, McpProbeCreate{RuntimeID: "runtime"})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := store.PopPending(ctx, "runtime")
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != McpProbePending || claimed.Status != McpProbeRunning {
		t.Fatal("claim mutated an earlier snapshot")
	}
	if err := store.Complete(ctx, created.ID, []string{"search"}, 42); err != nil {
		t.Fatal(err)
	}
	if err := store.Fail(ctx, created.ID, "handshake", "secret", 50); err != nil {
		t.Fatal(err)
	}
	result, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != McpProbeCompleted || result.ElapsedMs != 42 {
		t.Fatalf("late report replaced result: %+v", result)
	}
	result.Tools[0] = "changed"
	next, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if next.Tools[0] != "search" {
		t.Fatal("caller mutated stored tool names")
	}
}
