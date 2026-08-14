package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

const testWorkspaceMcpID = "workspace-123"

func newWorkspaceMcpTestCmd(use string) *cobra.Command {
	cmd := &cobra.Command{Use: use}
	cmd.Flags().String("workspace-id", "", "")
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("output", "json", "")
	switch use {
	case "set":
		cmd.Flags().String("mcp-config", "", "")
		cmd.Flags().Bool("mcp-config-stdin", false, "")
		cmd.Flags().String("mcp-config-file", "", "")
	case "add":
		cmd.Flags().String("server-config", "", "")
		cmd.Flags().Bool("server-config-stdin", false, "")
		cmd.Flags().String("server-config-file", "", "")
	}
	return cmd
}

// inventoryResponse is what every workspace-mcp endpoint returns: names and
// transports, never the stored document.
func inventoryResponse(names ...string) map[string]any {
	servers := make([]map[string]any, 0, len(names))
	for _, name := range names {
		servers = append(servers, map[string]any{"name": name, "transport": "http", "enabled": true})
	}
	return map[string]any{
		"workspace_id": testWorkspaceMcpID,
		"servers":      servers,
		"server_count": len(servers),
	}
}

func TestRunWorkspaceMcpGetListsInventory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_WORKSPACE_ID", testWorkspaceMcpID)

	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(inventoryResponse("linear"))
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)

	cmd := newWorkspaceMcpTestCmd("get")
	_ = cmd.Flags().Set("output", "table")
	out, err := captureStdout(t, func() error { return runWorkspaceMcpGet(cmd, nil) })
	if err != nil {
		t.Fatalf("runWorkspaceMcpGet: %v", err)
	}

	if gotMethod != http.MethodGet {
		t.Fatalf("method = %s, want GET", gotMethod)
	}
	if gotPath != "/api/workspaces/"+testWorkspaceMcpID+"/mcp-config" {
		t.Fatalf("path = %q", gotPath)
	}
	if !strings.Contains(out, "linear") || !strings.Contains(out, "http") {
		t.Fatalf("table output missing the server row: %q", out)
	}
}

func TestRunWorkspaceMcpGetEmptyInventory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_WORKSPACE_ID", testWorkspaceMcpID)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(inventoryResponse())
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)

	cmd := newWorkspaceMcpTestCmd("get")
	_ = cmd.Flags().Set("output", "table")
	out, err := captureStdout(t, func() error { return runWorkspaceMcpGet(cmd, nil) })
	if err != nil {
		t.Fatalf("runWorkspaceMcpGet: %v", err)
	}
	if !strings.Contains(out, "no MCP servers shared") {
		t.Fatalf("expected the empty-state line, got %q", out)
	}
}

// poisonedInventoryResponse is what a regressed (or hostile) server would send:
// the correct inventory PLUS the stored document at the top level and secret
// fields inside a server entry. Nothing from it may reach stdout, in ANY output
// format — the CLI decodes into a named struct precisely so it cannot.
func poisonedInventoryResponse() map[string]any {
	resp := inventoryResponse()
	resp["servers"] = []map[string]any{{
		"name":      "linear",
		"transport": "http",
		"enabled":   true,
		// Secret material smuggled inside the safe summary.
		"url":     "https://mcp.example/sk-live-url-leak",
		"headers": map[string]any{"Authorization": "Bearer sk-live-header-leak"},
		"env":     map[string]any{"TOKEN": "sk-live-env-leak"},
	}}
	resp["server_count"] = 1
	// Secret material smuggled at the top level.
	resp["mcp_config"] = map[string]any{
		"mcpServers": map[string]any{
			"linear": map[string]any{"headers": map[string]any{"Authorization": "Bearer sk-live-doc-leak"}},
		},
	}
	return resp
}

var workspaceMcpLeakMarkers = []string{
	"sk-live-url-leak",
	"sk-live-header-leak",
	"sk-live-env-leak",
	"sk-live-doc-leak",
	"mcp_config",
	"headers",
}

func TestRunWorkspaceMcpGetNeverPrintsSecrets(t *testing.T) {
	// Both output modes, because the JSON one is the DEFAULT: a renderer that
	// echoed the decoded response would leak there first.
	for _, output := range []string{"json", "table"} {
		t.Run(output, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("MULTICA_TOKEN", "test-token")
			t.Setenv("MULTICA_WORKSPACE_ID", testWorkspaceMcpID)

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(poisonedInventoryResponse())
			}))
			defer srv.Close()
			t.Setenv("MULTICA_SERVER_URL", srv.URL)

			cmd := newWorkspaceMcpTestCmd("get")
			_ = cmd.Flags().Set("output", output)
			out, err := captureStdout(t, func() error { return runWorkspaceMcpGet(cmd, nil) })
			if err != nil {
				t.Fatalf("runWorkspaceMcpGet: %v", err)
			}

			for _, marker := range workspaceMcpLeakMarkers {
				if strings.Contains(out, marker) {
					t.Fatalf("%s output leaked %q: %q", output, marker, out)
				}
			}
			// The safe inventory still renders.
			if !strings.Contains(out, "linear") {
				t.Fatalf("%s output dropped the server name: %q", output, out)
			}
		})
	}
}

// The write paths render the same inventory, so they need the same guard —
// the response to a write is where a server is most likely to echo back what
// it just stored.
func TestRunWorkspaceMcpWritesNeverPrintSecrets(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_WORKSPACE_ID", testWorkspaceMcpID)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(poisonedInventoryResponse())
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)

	cmd := newWorkspaceMcpTestCmd("add")
	_ = cmd.Flags().Set("server-config", `{"url":"https://mcp.example"}`)
	out, err := captureStdout(t, func() error { return runWorkspaceMcpAdd(cmd, []string{"linear"}) })
	if err != nil {
		t.Fatalf("runWorkspaceMcpAdd: %v", err)
	}
	for _, marker := range workspaceMcpLeakMarkers {
		if strings.Contains(out, marker) {
			t.Fatalf("add output leaked %q: %q", marker, out)
		}
	}
}

func TestRunWorkspaceMcpSetSendsDocument(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_WORKSPACE_ID", testWorkspaceMcpID)

	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(inventoryResponse("linear"))
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)

	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(path, []byte(`{"mcpServers":{"linear":{"url":"https://linear.example"}}}`), 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	cmd := newWorkspaceMcpTestCmd("set")
	_ = cmd.Flags().Set("mcp-config-file", path)
	if _, err := captureStdout(t, func() error { return runWorkspaceMcpSet(cmd, nil) }); err != nil {
		t.Fatalf("runWorkspaceMcpSet: %v", err)
	}

	if gotMethod != http.MethodPut {
		t.Fatalf("method = %s, want PUT", gotMethod)
	}
	if gotPath != "/api/workspaces/"+testWorkspaceMcpID+"/mcp-config" {
		t.Fatalf("path = %q", gotPath)
	}
	doc, _ := gotBody["mcp_config"].(map[string]any)
	if doc == nil {
		t.Fatalf("body = %v, want the document under mcp_config", gotBody)
	}
	if _, ok := doc["mcpServers"]; !ok {
		t.Fatalf("mcp_config = %v, want the file contents", doc)
	}
}

func TestRunWorkspaceMcpSetNullClears(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_WORKSPACE_ID", testWorkspaceMcpID)

	var rawBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r.Body)
		rawBody = buf.String()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(inventoryResponse())
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)

	cmd := newWorkspaceMcpTestCmd("set")
	_ = cmd.Flags().Set("mcp-config", "null")
	if _, err := captureStdout(t, func() error { return runWorkspaceMcpSet(cmd, nil) }); err != nil {
		t.Fatalf("runWorkspaceMcpSet: %v", err)
	}
	if !strings.Contains(rawBody, `"mcp_config":null`) {
		t.Fatalf("body = %q, want an explicit null to clear", rawBody)
	}
}

func TestRunWorkspaceMcpSetRequiresAnInputChannel(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_WORKSPACE_ID", testWorkspaceMcpID)

	cmd := newWorkspaceMcpTestCmd("set")
	err := runWorkspaceMcpSet(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "--mcp-config") {
		t.Fatalf("error = %v, want it to name the required flags", err)
	}
}

func TestRunWorkspaceMcpSetRejectsMultipleInputChannels(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_WORKSPACE_ID", testWorkspaceMcpID)

	cmd := newWorkspaceMcpTestCmd("set")
	_ = cmd.Flags().Set("mcp-config", "{}")
	_ = cmd.Flags().Set("mcp-config-file", "/tmp/does-not-matter.json")
	err := runWorkspaceMcpSet(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("error = %v, want a mutual-exclusion error", err)
	}
}

func TestRunWorkspaceMcpAddUpsertsOneServer(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_WORKSPACE_ID", testWorkspaceMcpID)

	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(inventoryResponse("linear"))
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)

	cmd := newWorkspaceMcpTestCmd("add")
	_ = cmd.Flags().Set("server-config", `{"url":"https://linear.example"}`)
	if _, err := captureStdout(t, func() error { return runWorkspaceMcpAdd(cmd, []string{"linear"}) }); err != nil {
		t.Fatalf("runWorkspaceMcpAdd: %v", err)
	}

	if gotMethod != http.MethodPut {
		t.Fatalf("method = %s, want PUT", gotMethod)
	}
	if gotPath != "/api/workspaces/"+testWorkspaceMcpID+"/mcp-config/servers/linear" {
		t.Fatalf("path = %q", gotPath)
	}
	// The entry is sent as the whole body — not wrapped in mcp_config, and not
	// wrapped in an mcpServers envelope.
	if gotBody["url"] != "https://linear.example" {
		t.Fatalf("body = %v, want the bare server entry", gotBody)
	}
}

// A server name is a path segment: anything needing escaping must survive.
func TestRunWorkspaceMcpAddEscapesServerName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_WORKSPACE_ID", testWorkspaceMcpID)

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(inventoryResponse("my server"))
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)

	cmd := newWorkspaceMcpTestCmd("add")
	_ = cmd.Flags().Set("server-config", `{"url":"https://x.example"}`)
	if _, err := captureStdout(t, func() error { return runWorkspaceMcpAdd(cmd, []string{"my server"}) }); err != nil {
		t.Fatalf("runWorkspaceMcpAdd: %v", err)
	}
	// net/http decodes the escaped segment back before it reaches the handler.
	if gotPath != "/api/workspaces/"+testWorkspaceMcpID+"/mcp-config/servers/my server" {
		t.Fatalf("path = %q, want the name preserved through escaping", gotPath)
	}
}

// `null` clears the whole layer, which is `set`'s job — for a single entry it
// is far more likely to be a mistake, so it is rejected rather than obeyed.
func TestRunWorkspaceMcpAddRejectsNullEntry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_WORKSPACE_ID", testWorkspaceMcpID)

	cmd := newWorkspaceMcpTestCmd("add")
	_ = cmd.Flags().Set("server-config", "null")
	if err := runWorkspaceMcpAdd(cmd, []string{"linear"}); err == nil {
		t.Fatal("expected 'null' to be rejected for a single server entry")
	}

	cmd = newWorkspaceMcpTestCmd("add")
	_ = cmd.Flags().Set("server-config", `["not-an-object"]`)
	if err := runWorkspaceMcpAdd(cmd, []string{"linear"}); err == nil {
		t.Fatal("expected a non-object entry to be rejected")
	}
}

func TestRunWorkspaceMcpAddRequiresAnInputChannel(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_WORKSPACE_ID", testWorkspaceMcpID)

	cmd := newWorkspaceMcpTestCmd("add")
	err := runWorkspaceMcpAdd(cmd, []string{"linear"})
	if err == nil || !strings.Contains(err.Error(), "--server-config") {
		t.Fatalf("error = %v, want it to name the required flags", err)
	}
}

func TestRunWorkspaceMcpRemoveDeletesOneServer(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_WORKSPACE_ID", testWorkspaceMcpID)

	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(inventoryResponse())
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)

	cmd := newWorkspaceMcpTestCmd("remove")
	if _, err := captureStdout(t, func() error { return runWorkspaceMcpRemove(cmd, []string{"linear"}) }); err != nil {
		t.Fatalf("runWorkspaceMcpRemove: %v", err)
	}

	if gotMethod != http.MethodDelete {
		t.Fatalf("method = %s, want DELETE", gotMethod)
	}
	if gotPath != "/api/workspaces/"+testWorkspaceMcpID+"/mcp-config/servers/linear" {
		t.Fatalf("path = %q", gotPath)
	}
}

// The positional workspace argument is honoured on every subcommand, so an
// admin can target a workspace that is not their profile default.
func TestRunWorkspaceMcpUsesWorkspaceArgument(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_TOKEN", "test-token")

	const wsUUID = "11111111-1111-1111-1111-111111111111"
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(inventoryResponse())
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)

	cmd := newWorkspaceMcpTestCmd("remove")
	if _, err := captureStdout(t, func() error { return runWorkspaceMcpRemove(cmd, []string{"linear", wsUUID}) }); err != nil {
		t.Fatalf("runWorkspaceMcpRemove: %v", err)
	}
	if gotPath != "/api/workspaces/"+wsUUID+"/mcp-config/servers/linear" {
		t.Fatalf("path = %q, want the positional workspace to win", gotPath)
	}
}
