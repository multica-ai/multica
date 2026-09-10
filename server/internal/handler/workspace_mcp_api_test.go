package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const workspaceMcpTestSecret = "sk-live-workspace-should-never-be-echoed"

var workspaceMcpTestEntry = `{"url":"https://linear.example","headers":{"Authorization":"Bearer ` + workspaceMcpTestSecret + `"}}`

// createWorkspaceMcpServerForTest seeds one library entry and removes it (plus
// any bindings) afterwards, so these tests can share the fixture workspace.
func createWorkspaceMcpServerForTest(t *testing.T, name, config string) string {
	t.Helper()

	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO workspace_mcp_server (workspace_id, name, config)
		VALUES ($1, $2, $3)
		RETURNING id
	`, testWorkspaceID, name, config).Scan(&id); err != nil {
		t.Fatalf("create workspace mcp server: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM agent_mcp_server WHERE server_id = $1`, id)
		testPool.Exec(ctx, `DELETE FROM workspace_mcp_server WHERE id = $1`, id)
	})
	return id
}

func listWorkspaceMcpServersForTest(t *testing.T, mutate func(*http.Request)) (int, []WorkspaceMcpServerResponse, string) {
	t.Helper()

	req := newRequest(http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/mcp-servers", nil)
	req = withURLParam(req, "id", testWorkspaceID)
	if mutate != nil {
		mutate(req)
	}
	w := httptest.NewRecorder()
	testHandler.ListWorkspaceMcpServers(w, req)

	var resp []WorkspaceMcpServerResponse
	raw := w.Body.String()
	if w.Code == http.StatusOK {
		if err := json.Unmarshal([]byte(raw), &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
	}
	return w.Code, resp, raw
}

// The stored entry is write-only (GH #6062): the read path returns the
// non-secret summary and nothing else, for EVERY role. Asserted on the raw
// body, not the decoded struct, so adding a secret-bearing field later fails
// here.
func TestListWorkspaceMcpServers_NeverEchoesSecrets(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	createWorkspaceMcpServerForTest(t, "linear", workspaceMcpTestEntry)

	code, resp, raw := listWorkspaceMcpServersForTest(t, nil)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", code, raw)
	}
	// The workspace owner is the most privileged caller there is; if anyone
	// could read the credential back it would be this request.
	if strings.Contains(raw, workspaceMcpTestSecret) {
		t.Fatalf("response echoed the shared credential: %s", raw)
	}
	if strings.Contains(raw, "linear.example") {
		t.Fatalf("response echoed the server URL, which is itself credential material: %s", raw)
	}
	var found *WorkspaceMcpServerResponse
	for i := range resp {
		if resp[i].Name == "linear" {
			found = &resp[i]
		}
	}
	if found == nil {
		t.Fatalf("expected the server in the library listing, got %+v", resp)
	}
	if found.Transport != "http" {
		t.Errorf("transport = %q, want http", found.Transport)
	}
	// The library listing has no binding, so no toggle state to report.
	if found.Enabled != nil {
		t.Errorf("library listing must not carry an enabled flag, got %v", *found.Enabled)
	}
}

func createWorkspaceMcpServerViaAPI(t *testing.T, body any, mutate func(*http.Request)) (int, WorkspaceMcpServerResponse, string) {
	t.Helper()

	req := newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/mcp-servers", body)
	req = withURLParam(req, "id", testWorkspaceID)
	if mutate != nil {
		mutate(req)
	}
	w := httptest.NewRecorder()
	testHandler.CreateWorkspaceMcpServer(w, req)

	var resp WorkspaceMcpServerResponse
	raw := w.Body.String()
	if w.Code == http.StatusCreated {
		if err := json.Unmarshal([]byte(raw), &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		t.Cleanup(func() {
			ctx := context.Background()
			testPool.Exec(ctx, `DELETE FROM agent_mcp_server WHERE server_id = $1`, resp.ID)
			testPool.Exec(ctx, `DELETE FROM workspace_mcp_server WHERE id = $1`, resp.ID)
		})
	}
	return w.Code, resp, raw
}

// The defining property of the redesign: creating a library entry gives it to
// nobody. Adding a server must not touch any agent.
func TestCreateWorkspaceMcpServer_BindsToNoAgent(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "ws-mcp-unbound-agent", nil)

	code, resp, raw := createWorkspaceMcpServerViaAPI(t, map[string]any{
		"name":   "linear",
		"config": json.RawMessage(workspaceMcpTestEntry),
	}, nil)
	if code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", code, raw)
	}
	if strings.Contains(raw, workspaceMcpTestSecret) {
		t.Fatalf("create response echoed the entry: %s", raw)
	}

	var bindings int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM agent_mcp_server WHERE server_id = $1`, resp.ID).Scan(&bindings); err != nil {
		t.Fatalf("count bindings: %v", err)
	}
	if bindings != 0 {
		t.Fatalf("creating a library entry bound it to %d agent(s); it must bind to none", bindings)
	}
	// And the agent sees nothing until someone adds it.
	if servers := listAgentMcpServersForTest(t, agentID); len(servers) != 0 {
		t.Fatalf("agent already has %d MCP server(s) without being given any", len(servers))
	}
}

func TestCreateWorkspaceMcpServer_RejectsBadInput(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	for _, body := range []map[string]any{
		{"name": "", "config": map[string]any{"url": "https://mcp.example"}},
		{"name": "has space", "config": map[string]any{"url": "https://mcp.example"}},
		{"name": "ok", "config": map[string]any{}},
		{"name": "ok", "config": "not-an-object"},
		{"name": "ok"},
	} {
		code, _, raw := createWorkspaceMcpServerViaAPI(t, body, nil)
		if code != http.StatusBadRequest {
			t.Errorf("body %v: expected 400, got %d: %s", body, code, raw)
		}
	}
}

func TestCreateWorkspaceMcpServer_RejectsDuplicateName(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	createWorkspaceMcpServerForTest(t, "linear", workspaceMcpTestEntry)

	code, _, raw := createWorkspaceMcpServerViaAPI(t, map[string]any{
		"name":   "linear",
		"config": map[string]any{"url": "https://other.example"},
	}, nil)
	if code != http.StatusConflict {
		t.Fatalf("expected 409 for a duplicate name, got %d: %s", code, raw)
	}
}

// Curating the library stays a human admin action: a task token running under
// an owner's PAT must not be able to add a shared server.
func TestCreateWorkspaceMcpServer_DeniesAgentActor(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	caller := createHandlerTestAgent(t, "ws-mcp-lib-writer", nil)
	taskID := insertHandlerTestTask(t, caller)

	code, _, raw := createWorkspaceMcpServerViaAPI(t, map[string]any{
		"name":   "linear",
		"config": map[string]any{"url": "https://mcp.example"},
	}, func(req *http.Request) {
		req.Header.Set("X-Actor-Source", "task_token")
		req.Header.Set("X-Agent-ID", caller)
		req.Header.Set("X-Task-ID", taskID)
	})
	if code != http.StatusForbidden {
		t.Fatalf("expected 403 for an agent actor, got %d: %s", code, raw)
	}
}

// Renaming is safe in this model precisely because bindings key off the id.
func TestUpdateWorkspaceMcpServer_RenameKeepsBindings(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	serverID := createWorkspaceMcpServerForTest(t, "linear", workspaceMcpTestEntry)
	agentID := createHandlerTestAgent(t, "ws-mcp-rename-agent", nil)
	addAgentMcpServerForTest(t, agentID, serverID)

	req := newRequest(http.MethodPut, "/api/workspaces/"+testWorkspaceID+"/mcp-servers/"+serverID,
		map[string]any{"name": "linear-v2"})
	req = withURLParams(req, "id", testWorkspaceID, "serverId", serverID)
	w := httptest.NewRecorder()
	testHandler.UpdateWorkspaceMcpServer(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	servers := listAgentMcpServersForTest(t, agentID)
	if len(servers) != 1 || servers[0].Name != "linear-v2" {
		t.Fatalf("binding did not follow the rename: %+v", servers)
	}
}

// Deleting a library entry has to sweep its bindings: the junction carries no
// foreign key, so an orphan would keep pointing at a server that is gone.
func TestDeleteWorkspaceMcpServer_SweepsBindings(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	serverID := createWorkspaceMcpServerForTest(t, "linear", workspaceMcpTestEntry)
	agentID := createHandlerTestAgent(t, "ws-mcp-delete-agent", nil)
	addAgentMcpServerForTest(t, agentID, serverID)

	req := newRequest(http.MethodDelete, "/api/workspaces/"+testWorkspaceID+"/mcp-servers/"+serverID, nil)
	req = withURLParams(req, "id", testWorkspaceID, "serverId", serverID)
	w := httptest.NewRecorder()
	testHandler.DeleteWorkspaceMcpServer(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	var bindings int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM agent_mcp_server WHERE server_id = $1`, serverID).Scan(&bindings); err != nil {
		t.Fatalf("count bindings: %v", err)
	}
	if bindings != 0 {
		t.Fatalf("delete left %d orphaned binding(s)", bindings)
	}
}

func listAgentMcpServersForTest(t *testing.T, agentID string) []WorkspaceMcpServerResponse {
	t.Helper()

	req := newRequest(http.MethodGet, "/api/agents/"+agentID+"/mcp-servers", nil)
	req = withURLParam(req, "id", agentID)
	w := httptest.NewRecorder()
	testHandler.ListAgentMcpServers(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListAgentMcpServers: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp []WorkspaceMcpServerResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp
}

func addAgentMcpServerForTest(t *testing.T, agentID, serverID string) []WorkspaceMcpServerResponse {
	t.Helper()

	req := newRequest(http.MethodPost, "/api/agents/"+agentID+"/mcp-servers",
		map[string]any{"server_id": serverID})
	req = withURLParam(req, "id", agentID)
	w := httptest.NewRecorder()
	testHandler.AddAgentMcpServer(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("AddAgentMcpServer: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp []WorkspaceMcpServerResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp
}

func TestAgentMcpServerBinding_AddToggleRemove(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	serverID := createWorkspaceMcpServerForTest(t, "linear", workspaceMcpTestEntry)
	agentID := createHandlerTestAgent(t, "ws-mcp-binding-agent", nil)

	// Add — the response is the resulting binding list, so the client never
	// has to guess.
	servers := addAgentMcpServerForTest(t, agentID, serverID)
	if len(servers) != 1 || servers[0].ID != serverID {
		t.Fatalf("expected the server to be bound, got %+v", servers)
	}
	if servers[0].Enabled == nil || !*servers[0].Enabled {
		t.Fatalf("a freshly added server must be enabled, got %+v", servers[0])
	}
	// Adding twice is idempotent rather than an error.
	if servers = addAgentMcpServerForTest(t, agentID, serverID); len(servers) != 1 {
		t.Fatalf("re-adding duplicated the binding: %+v", servers)
	}

	// Toggle off — the binding survives so turning it back on is one click.
	req := newRequest(http.MethodPut, "/api/agents/"+agentID+"/mcp-servers/"+serverID+"/enabled",
		map[string]any{"enabled": false})
	req = withURLParams(req, "id", agentID, "serverId", serverID)
	w := httptest.NewRecorder()
	testHandler.SetAgentMcpServerEnabled(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("SetAgentMcpServerEnabled: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	servers = listAgentMcpServersForTest(t, agentID)
	if len(servers) != 1 || servers[0].Enabled == nil || *servers[0].Enabled {
		t.Fatalf("expected the binding to survive as disabled, got %+v", servers)
	}

	// Remove — the library entry itself is untouched.
	req = newRequest(http.MethodDelete, "/api/agents/"+agentID+"/mcp-servers/"+serverID, nil)
	req = withURLParams(req, "id", agentID, "serverId", serverID)
	w = httptest.NewRecorder()
	testHandler.RemoveAgentMcpServer(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("RemoveAgentMcpServer: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if servers = listAgentMcpServersForTest(t, agentID); len(servers) != 0 {
		t.Fatalf("expected no bindings after removal, got %+v", servers)
	}
	var libraryRows int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM workspace_mcp_server WHERE id = $1`, serverID).Scan(&libraryRows); err != nil {
		t.Fatalf("count library rows: %v", err)
	}
	if libraryRows != 1 {
		t.Fatalf("removing a binding deleted the library entry")
	}
}

// A server id from another workspace must not bind across the tenant boundary.
func TestAddAgentMcpServer_RejectsServerFromAnotherWorkspace(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	var otherWorkspaceID, otherServerID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Other WS', 'ws-mcp-other', '', 'OTH')
		RETURNING id
	`).Scan(&otherWorkspaceID); err != nil {
		t.Fatalf("create other workspace: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, otherWorkspaceID) })
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace_mcp_server (workspace_id, name, config)
		VALUES ($1, 'foreign', '{"url":"https://foreign.example"}')
		RETURNING id
	`, otherWorkspaceID).Scan(&otherServerID); err != nil {
		t.Fatalf("create foreign server: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace_mcp_server WHERE id = $1`, otherServerID)
	})

	agentID := createHandlerTestAgent(t, "ws-mcp-tenant-agent", nil)
	req := newRequest(http.MethodPost, "/api/agents/"+agentID+"/mcp-servers",
		map[string]any{"server_id": otherServerID})
	req = withURLParam(req, "id", agentID)
	w := httptest.NewRecorder()
	testHandler.AddAgentMcpServer(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for a server from another workspace, got %d: %s", w.Code, w.Body.String())
	}
}

// The agent's binding list is secret-free too, and an agent actor may not
// change its own assignments.
func TestAgentMcpServerBinding_ResponseIsSecretFreeAndAgentActorsCannotWrite(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	serverID := createWorkspaceMcpServerForTest(t, "linear", workspaceMcpTestEntry)
	agentID := createHandlerTestAgent(t, "ws-mcp-actor-agent", nil)

	req := newRequest(http.MethodGet, "/api/agents/"+agentID+"/mcp-servers", nil)
	req = withURLParam(req, "id", agentID)
	w := httptest.NewRecorder()
	testHandler.ListAgentMcpServers(w, req)
	if strings.Contains(w.Body.String(), workspaceMcpTestSecret) {
		t.Fatalf("agent binding list echoed the entry: %s", w.Body.String())
	}

	taskID := insertHandlerTestTask(t, agentID)
	req = newRequest(http.MethodPost, "/api/agents/"+agentID+"/mcp-servers",
		map[string]any{"server_id": serverID})
	req = withURLParam(req, "id", agentID)
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	w = httptest.NewRecorder()
	testHandler.AddAgentMcpServer(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for an agent actor, got %d: %s", w.Code, w.Body.String())
	}
}
