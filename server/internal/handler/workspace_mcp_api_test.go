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

var workspaceMcpTestDoc = `{"mcpServers":{"linear":{"url":"https://linear.example","headers":{"Authorization":"Bearer ` + workspaceMcpTestSecret + `"}}}}`

// setWorkspaceMcpConfigForTest stores a shared document and restores whatever
// was there afterwards, so these tests can run against the shared fixture
// workspace without leaking state into the rest of the suite.
func setWorkspaceMcpConfigForTest(t *testing.T, doc string) {
	t.Helper()

	ctx := context.Background()
	var previous []byte
	if err := testPool.QueryRow(ctx, `SELECT mcp_config FROM workspace WHERE id = $1`, testWorkspaceID).Scan(&previous); err != nil {
		t.Fatalf("load workspace mcp_config: %v", err)
	}
	// An empty string means "unconfigured" — store SQL NULL, not invalid JSON.
	var next []byte
	if doc != "" {
		next = []byte(doc)
	}
	if _, err := testPool.Exec(ctx, `UPDATE workspace SET mcp_config = $1 WHERE id = $2`, next, testWorkspaceID); err != nil {
		t.Fatalf("set workspace mcp_config: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `UPDATE workspace SET mcp_config = $1 WHERE id = $2`, previous, testWorkspaceID)
	})
}

func storedWorkspaceMcpConfig(t *testing.T) string {
	t.Helper()
	var stored []byte
	if err := testPool.QueryRow(context.Background(), `SELECT mcp_config FROM workspace WHERE id = $1`, testWorkspaceID).Scan(&stored); err != nil {
		t.Fatalf("read back mcp_config: %v", err)
	}
	return string(stored)
}

func getWorkspaceMcpConfigForTest(t *testing.T, mutate func(*http.Request)) (int, WorkspaceMcpConfigResponse, string) {
	t.Helper()

	req := newRequest(http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/mcp-config", nil)
	req = withURLParam(req, "id", testWorkspaceID)
	if mutate != nil {
		mutate(req)
	}
	w := httptest.NewRecorder()
	testHandler.GetWorkspaceMcpConfig(w, req)

	var resp WorkspaceMcpConfigResponse
	raw := w.Body.String()
	if w.Code == http.StatusOK {
		if err := json.Unmarshal([]byte(raw), &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
	}
	return w.Code, resp, raw
}

// The shared document is write-only (GH #6062): the read path returns the
// non-secret inventory and nothing else, for EVERY role. This is the test that
// pins that contract — it asserts on the raw response body, not the decoded
// struct, so adding a secret-bearing field later fails here.
func TestGetWorkspaceMcpConfig_NeverEchoesSecrets(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	setWorkspaceMcpConfigForTest(t, workspaceMcpTestDoc)

	code, resp, raw := getWorkspaceMcpConfigForTest(t, nil)
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
	if len(resp.Servers) != 1 || resp.Servers[0].Name != "linear" {
		t.Fatalf("expected the server inventory, got %+v", resp.Servers)
	}
	if resp.Servers[0].Transport != "http" || !resp.Servers[0].Enabled {
		t.Errorf("inventory = %+v, want an enabled http server", resp.Servers[0])
	}
	if resp.ServerCount != 1 {
		t.Errorf("server_count = %d, want 1", resp.ServerCount)
	}
}

// The inventory is member-visible so an agent's settings view can show what it
// inherits; there is nothing secret in it to gate on role.
func TestGetWorkspaceMcpConfig_InventoryVisibleToPlainMember(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	setWorkspaceMcpConfigForTest(t, workspaceMcpTestDoc)

	ctx := context.Background()
	var plainUserID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Workspace MCP Member', 'ws-mcp-member@multica.test')
		RETURNING id
	`).Scan(&plainUserID); err != nil {
		t.Fatalf("create member user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE email = 'ws-mcp-member@multica.test'`)
	})
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, testWorkspaceID, plainUserID); err != nil {
		t.Fatalf("add workspace member: %v", err)
	}

	code, resp, raw := getWorkspaceMcpConfigForTest(t, func(req *http.Request) {
		req.Header.Set("X-User-ID", plainUserID)
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", code, raw)
	}
	if strings.Contains(raw, workspaceMcpTestSecret) {
		t.Fatalf("response echoed the shared credential to a plain member: %s", raw)
	}
	if resp.ServerCount != 1 {
		t.Errorf("server_count = %d, want 1", resp.ServerCount)
	}
}

func TestGetWorkspaceMcpConfig_UnconfiguredWorkspace(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	setWorkspaceMcpConfigForTest(t, "")

	code, resp, raw := getWorkspaceMcpConfigForTest(t, nil)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", code, raw)
	}
	if len(resp.Servers) != 0 || resp.ServerCount != 0 {
		t.Errorf("expected an empty inventory, got %+v", resp.Servers)
	}
}

// A document that somehow became malformed must still render the settings
// screen rather than 500 it.
func TestGetWorkspaceMcpConfig_MalformedDocumentYieldsEmptyInventory(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	setWorkspaceMcpConfigForTest(t, `{"mcpServers":{"broken":"not-an-object"}}`)

	code, resp, raw := getWorkspaceMcpConfigForTest(t, nil)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", code, raw)
	}
	if len(resp.Servers) != 0 {
		t.Errorf("expected an empty inventory for a malformed document, got %+v", resp.Servers)
	}
}

func updateWorkspaceMcpConfigForTest(t *testing.T, body any, mutate func(*http.Request)) (int, WorkspaceMcpConfigResponse, string) {
	t.Helper()

	req := newRequest(http.MethodPut, "/api/workspaces/"+testWorkspaceID+"/mcp-config", body)
	req = withURLParam(req, "id", testWorkspaceID)
	if mutate != nil {
		mutate(req)
	}
	w := httptest.NewRecorder()
	testHandler.UpdateWorkspaceMcpConfig(w, req)

	var resp WorkspaceMcpConfigResponse
	raw := w.Body.String()
	if w.Code == http.StatusOK {
		if err := json.Unmarshal([]byte(raw), &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
	}
	return w.Code, resp, raw
}

func TestUpdateWorkspaceMcpConfig_SetAndClear(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	setWorkspaceMcpConfigForTest(t, "")

	code, resp, raw := updateWorkspaceMcpConfigForTest(t, map[string]any{
		"mcp_config": json.RawMessage(workspaceMcpTestDoc),
	}, nil)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", code, raw)
	}
	if resp.ServerCount != 1 {
		t.Errorf("server_count = %d, want 1", resp.ServerCount)
	}
	// Even the write response — sent to the admin who just supplied the
	// document — does not echo it back.
	if strings.Contains(raw, workspaceMcpTestSecret) {
		t.Fatalf("write response echoed the shared credential: %s", raw)
	}
	if !strings.Contains(storedWorkspaceMcpConfig(t), workspaceMcpTestSecret) {
		t.Fatalf("stored document = %s, want the submitted one", storedWorkspaceMcpConfig(t))
	}

	// An explicit null clears the layer; every inheriting agent falls back to
	// its own config on the next claim.
	code, _, raw = updateWorkspaceMcpConfigForTest(t, map[string]any{"mcp_config": nil}, nil)
	if code != http.StatusOK {
		t.Fatalf("clear: expected 200, got %d: %s", code, raw)
	}
	if stored := storedWorkspaceMcpConfig(t); stored != "" {
		t.Fatalf("expected NULL after clear, got %s", stored)
	}
}

func TestUpdateWorkspaceMcpConfig_RejectsBadShape(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	setWorkspaceMcpConfigForTest(t, "")

	for _, body := range []map[string]any{
		{"mcp_config": []any{}},
		{"mcp_config": "nope"},
		{"mcp_config": map[string]any{"mcpServers": map[string]any{"a": "not-an-object"}}},
		{"mcp_config": map[string]any{"mcp": map[string]any{"a": map[string]any{}}}},
		{},
	} {
		code, _, raw := updateWorkspaceMcpConfigForTest(t, body, nil)
		if code != http.StatusBadRequest {
			t.Errorf("body %v: expected 400, got %d: %s", body, code, raw)
		}
	}

	if stored := storedWorkspaceMcpConfig(t); stored != "" {
		t.Fatalf("a rejected write must not touch the stored document, got %s", stored)
	}
}

// Writing shared servers stays a human admin action: a task token that happens
// to run under an owner's PAT must not be able to hand every agent in the
// workspace a new MCP server.
func TestUpdateWorkspaceMcpConfig_DeniesAgentActor(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	setWorkspaceMcpConfigForTest(t, "")

	caller := createHandlerTestAgent(t, "ws-mcp-writer", nil)
	taskID := insertHandlerTestTask(t, caller)
	asAgent := func(req *http.Request) {
		req.Header.Set("X-Actor-Source", "task_token")
		req.Header.Set("X-Agent-ID", caller)
		req.Header.Set("X-Task-ID", taskID)
	}

	code, _, raw := updateWorkspaceMcpConfigForTest(t, map[string]any{
		"mcp_config": json.RawMessage(workspaceMcpTestDoc),
	}, asAgent)
	if code != http.StatusForbidden {
		t.Fatalf("full replace: expected 403 for an agent actor, got %d: %s", code, raw)
	}

	code, _, raw = upsertWorkspaceMcpServerForTest(t, "linear", map[string]any{"url": "https://x.example"}, asAgent)
	if code != http.StatusForbidden {
		t.Fatalf("per-server upsert: expected 403 for an agent actor, got %d: %s", code, raw)
	}
}

func upsertWorkspaceMcpServerForTest(t *testing.T, name string, entry any, mutate func(*http.Request)) (int, WorkspaceMcpConfigResponse, string) {
	t.Helper()

	req := newRequest(http.MethodPut, "/api/workspaces/"+testWorkspaceID+"/mcp-config/servers/"+name, entry)
	req = withURLParams(req, "id", testWorkspaceID, "serverName", name)
	if mutate != nil {
		mutate(req)
	}
	w := httptest.NewRecorder()
	testHandler.UpsertWorkspaceMcpServer(w, req)

	var resp WorkspaceMcpConfigResponse
	raw := w.Body.String()
	if w.Code == http.StatusOK {
		if err := json.Unmarshal([]byte(raw), &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
	}
	return w.Code, resp, raw
}

func deleteWorkspaceMcpServerForTest(t *testing.T, name string) (int, WorkspaceMcpConfigResponse, string) {
	t.Helper()

	req := newRequest(http.MethodDelete, "/api/workspaces/"+testWorkspaceID+"/mcp-config/servers/"+name, nil)
	req = withURLParams(req, "id", testWorkspaceID, "serverName", name)
	w := httptest.NewRecorder()
	testHandler.DeleteWorkspaceMcpServer(w, req)

	var resp WorkspaceMcpConfigResponse
	raw := w.Body.String()
	if w.Code == http.StatusOK {
		if err := json.Unmarshal([]byte(raw), &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
	}
	return w.Code, resp, raw
}

// The per-server path is what the settings UI uses: because it can never read
// the document, the server has to do the read-modify-write for it — and must
// not disturb the servers the caller did not mention.
func TestUpsertWorkspaceMcpServer_MergesWithoutReadingBack(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	setWorkspaceMcpConfigForTest(t, workspaceMcpTestDoc)

	code, resp, raw := upsertWorkspaceMcpServerForTest(t, "github", map[string]any{"command": "github-mcp"}, nil)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", code, raw)
	}
	if resp.ServerCount != 2 {
		t.Fatalf("server_count = %d, want 2 (%+v)", resp.ServerCount, resp.Servers)
	}
	if resp.Servers[0].Name != "github" || resp.Servers[0].Transport != "stdio" {
		t.Errorf("inventory[0] = %+v, want the new stdio server first (sorted)", resp.Servers[0])
	}
	// The untouched server keeps its credential in storage.
	if !strings.Contains(storedWorkspaceMcpConfig(t), workspaceMcpTestSecret) {
		t.Fatalf("upsert dropped the existing server's credentials: %s", storedWorkspaceMcpConfig(t))
	}

	// Replacing an existing name overwrites just that entry.
	code, resp, raw = upsertWorkspaceMcpServerForTest(t, "linear", map[string]any{"url": "https://linear-v2.example"}, nil)
	if code != http.StatusOK {
		t.Fatalf("replace: expected 200, got %d: %s", code, raw)
	}
	if resp.ServerCount != 2 {
		t.Fatalf("replace: server_count = %d, want 2", resp.ServerCount)
	}
	if strings.Contains(storedWorkspaceMcpConfig(t), workspaceMcpTestSecret) {
		t.Errorf("replacing an entry must overwrite it, not merge into it: %s", storedWorkspaceMcpConfig(t))
	}
}

func TestUpsertWorkspaceMcpServer_RejectsBadEntry(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	setWorkspaceMcpConfigForTest(t, "")

	for _, entry := range []any{"not-an-object", []any{}, 42} {
		code, _, raw := upsertWorkspaceMcpServerForTest(t, "bad", entry, nil)
		if code != http.StatusBadRequest {
			t.Errorf("entry %v: expected 400, got %d: %s", entry, code, raw)
		}
	}
	if stored := storedWorkspaceMcpConfig(t); stored != "" {
		t.Fatalf("a rejected upsert must not write, got %s", stored)
	}
}

// Deleting the last shared server clears the column rather than storing an
// empty document, so inheriting agents fall back to their own config instead
// of being handed a managed-empty one.
func TestDeleteWorkspaceMcpServer_LastServerClearsTheLayer(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	setWorkspaceMcpConfigForTest(t, workspaceMcpTestDoc)

	code, _, raw := deleteWorkspaceMcpServerForTest(t, "does-not-exist")
	if code != http.StatusNotFound {
		t.Fatalf("expected 404 for an unknown server, got %d: %s", code, raw)
	}

	code, resp, raw := deleteWorkspaceMcpServerForTest(t, "linear")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", code, raw)
	}
	if resp.ServerCount != 0 {
		t.Errorf("server_count = %d, want 0", resp.ServerCount)
	}
	if stored := storedWorkspaceMcpConfig(t); stored != "" {
		t.Fatalf("expected NULL after removing the last server, got %s", stored)
	}
}
