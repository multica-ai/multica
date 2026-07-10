// CEREBRO-PATCH(cerebro-connections-admin-mcp-test): FIR-2835 cerebro-only file.
package clitools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

// TestRegisterCerebroConnectionAdminToolsRegistersCRUD verifies the six admin
// tools are registered under stable names.
func TestRegisterCerebroConnectionAdminToolsRegistersCRUD(t *testing.T) {
	srv := mcp.NewServer("test", "0")
	registerCerebroConnectionAdminTools(srv, cli.NewAPIClient("", "", ""))

	for _, name := range []string{
		"list_connections", "get_connection", "create_connection",
		"update_connection", "delete_connection", "test_connection",
	} {
		if !hasTool(srv, name) {
			t.Errorf("expected tool %q to be registered", name)
		}
	}
}

// TestCreateConnectionToolSendsInternalPath verifies create_connection posts an
// internal-path mcp_http connection with the bearer token — the shape asked for
// in FIR-2835.
func TestCreateConnectionToolSendsInternalPath(t *testing.T) {
	var got map[string]any
	var gotMethod, gotPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "new-id"})
	}))
	defer ts.Close()

	srv := mcp.NewServer("test", "0")
	registerCerebroConnectionAdminTools(srv, cli.NewAPIClient(ts.URL, "ws-1", "tok"))

	res, err := srv.Call(context.Background(), "create_connection", map[string]any{
		"name":       "finance-mcp",
		"type":       "mcp_http",
		"url":        "http://firtal-agents-private.internal:3000/api/mcp",
		"internal":   true,
		"auth_token": "sekret",
	})
	if err != nil {
		t.Fatalf("create_connection call: %v", err)
	}
	if res.IsError {
		t.Fatalf("create_connection returned error: %v", res.Content)
	}

	if gotMethod != http.MethodPost || gotPath != "/api/workspaces/ws-1/connections" {
		t.Fatalf("hit %s %s, want POST /api/workspaces/ws-1/connections", gotMethod, gotPath)
	}
	if got["name"] != "finance-mcp" || got["type"] != "mcp_http" {
		t.Errorf("name/type = %v/%v", got["name"], got["type"])
	}
	if got["internal"] != true {
		t.Errorf("internal = %v, want true", got["internal"])
	}
	if got["display_name"] != "finance-mcp" {
		t.Errorf("display_name = %v, want defaulted to name", got["display_name"])
	}
	auth, ok := got["auth_config"].(map[string]any)
	if !ok || auth["bearer_token"] != "sekret" {
		t.Errorf("auth_config = %v, want bearer_token=sekret", got["auth_config"])
	}
}

// TestCreateConnectionToolRequiresWorkspace — with no workspace set the tool
// returns an error result instead of making a request.
func TestCreateConnectionToolRequiresWorkspace(t *testing.T) {
	srv := mcp.NewServer("test", "0")
	registerCerebroConnectionAdminTools(srv, cli.NewAPIClient("http://unused", "", "tok"))

	res, err := srv.Call(context.Background(), "create_connection", map[string]any{
		"name": "x", "url": "http://y",
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected an error result when workspace_id is unset")
	}
}
