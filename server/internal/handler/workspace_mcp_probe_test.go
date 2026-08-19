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
		"metadata": testutil.Raw(`'{"capabilities":["` + protocol.DaemonCapabilityMcpProbeV1 + `"]}'::jsonb`),
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

func TestProbeWorkspaceMcpServer_MultipleRuntimesRequireID(t *testing.T) {
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
	if got, _ := body["error"].(string); got != "runtime_id is required when multiple runtimes are online" {
		t.Fatalf("error = %q", got)
	}
}

func TestProbeWorkspaceMcpServer_UnsupportedDaemon(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	serverID := mcpProbeServer(t, "probe-old-daemon")
	// The fixture workspace already has one online runtime without mcp-probe-v1.
	// Adding a second capable runtime would force runtime_id; pin the old one.
	req := testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/mcp-servers/"+serverID+"/probe", map[string]any{
			"runtime_id": testRuntimeID,
		}),
		"id", testWorkspaceID, "serverId", serverID,
	)
	testutil.Call(t, testHandler.ProbeWorkspaceMcpServer, req).Want(http.StatusConflict)

	var lastProbe []byte
	dbfx.QueryRow(t, `SELECT last_probe FROM workspace_mcp_server WHERE id = $1`, serverID).Scan(&lastProbe)
	if !strings.Contains(string(lastProbe), protocol.McpProbeCodeUnsupportedDaemon) {
		t.Fatalf("last_probe = %s, want unsupported_daemon", lastProbe)
	}
	_, servers, body := listWorkspaceMcpServersForTest(t, nil)
	if strings.Contains(body, workspaceMcpTestSecret) {
		t.Fatalf("list echoed a secret: %s", body)
	}
	var found *WorkspaceMcpServerResponse
	for i := range servers {
		if servers[i].ID == serverID {
			found = &servers[i]
		}
	}
	if found == nil || found.LastProbe == nil || found.LastProbe.ErrorCode != protocol.McpProbeCodeUnsupportedDaemon {
		t.Fatalf("list last_probe = %+v", found)
	}
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
	testutil.Call(t, testHandler.ProbeWorkspaceMcpServer, req).Want(http.StatusServiceUnavailable)
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

func TestUpdateWorkspaceMcpServer_ConfigClearsLastProbe(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	serverID := mcpProbeServer(t, "probe-clear")
	dbfx.Exec(t, `UPDATE workspace_mcp_server SET last_probe = $2 WHERE id = $1`,
		serverID, []byte(`{"status":"ok","probed_at":"2026-01-01T00:00:00Z","runtime_id":"r","runtime_name":"box","elapsed_ms":1,"tools":["a"]}`))

	rename := testutil.WithURLParams(
		newRequest(http.MethodPut, "/api/workspaces/"+testWorkspaceID+"/mcp-servers/"+serverID, map[string]any{"name": "probe-cleared"}),
		"id", testWorkspaceID, "serverId", serverID,
	)
	var renamed WorkspaceMcpServerResponse
	testutil.Call(t, testHandler.UpdateWorkspaceMcpServer, rename).Want(http.StatusOK).JSON(&renamed)
	if renamed.LastProbe == nil || renamed.LastProbe.Status != "ok" {
		t.Fatalf("rename dropped last_probe: %+v", renamed.LastProbe)
	}

	replace := testutil.WithURLParams(
		newRequest(http.MethodPut, "/api/workspaces/"+testWorkspaceID+"/mcp-servers/"+serverID, map[string]any{
			"config": map[string]any{"url": "https://other.example"},
		}),
		"id", testWorkspaceID, "serverId", serverID,
	)
	var updated WorkspaceMcpServerResponse
	testutil.Call(t, testHandler.UpdateWorkspaceMcpServer, replace).Want(http.StatusOK).JSON(&updated)
	if updated.LastProbe != nil {
		t.Fatalf("config replace kept last_probe: %+v", updated.LastProbe)
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
	if strings.Contains(got, "\n") || strings.Contains(got, "rest") {
		t.Fatalf("leaked trailing dump: %q", got)
	}
}
