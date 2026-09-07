package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

const (
	testWorkspaceMcpID = "workspace-123"
	testMcpServerID    = "11111111-1111-1111-1111-111111111111"
	testMcpAgentID     = "22222222-2222-2222-2222-222222222222"
)

func newWorkspaceMcpTestCmd(use string) *cobra.Command {
	cmd := &cobra.Command{Use: use}
	cmd.Flags().String("workspace-id", "", "")
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("output", "json", "")
	switch use {
	case "add", "update":
		cmd.Flags().String("server-config", "", "")
		cmd.Flags().Bool("server-config-stdin", false, "")
		cmd.Flags().String("server-config-file", "", "")
		if use == "update" {
			cmd.Flags().String("name", "", "")
		}
	}
	return cmd
}

// serverResponse is what every MCP endpoint returns: identity and transport,
// never the stored entry.
func serverResponse(name string) map[string]any {
	return map[string]any{
		"id":           testMcpServerID,
		"workspace_id": testWorkspaceMcpID,
		"name":         name,
		"transport":    "http",
		"created_at":   "2026-08-14T00:00:00Z",
		"updated_at":   "2026-08-14T00:00:00Z",
	}
}

// poisonedServerResponse is what a regressed (or hostile) server would send:
// the correct summary PLUS the stored entry. Nothing from it may reach stdout,
// in ANY output format — the CLI decodes into a named struct precisely so it
// cannot.
func poisonedServerResponse(name string) map[string]any {
	resp := serverResponse(name)
	resp["config"] = map[string]any{
		"url":     "https://mcp.example/sk-live-url-leak",
		"headers": map[string]any{"Authorization": "Bearer sk-live-header-leak"},
		"env":     map[string]any{"TOKEN": "sk-live-env-leak"},
	}
	resp["url"] = "https://mcp.example/sk-live-flat-leak"
	return resp
}

var workspaceMcpLeakMarkers = []string{
	"sk-live-url-leak",
	"sk-live-header-leak",
	"sk-live-env-leak",
	"sk-live-flat-leak",
	"config",
	"headers",
}

func mcpTestServer(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_WORKSPACE_ID", testWorkspaceMcpID)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
}

func TestRunWorkspaceMcpListShowsTheLibrary(t *testing.T) {
	var gotMethod, gotPath string
	mcpTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{serverResponse("linear")})
	})

	cmd := newWorkspaceMcpTestCmd("list")
	_ = cmd.Flags().Set("output", "table")
	out, err := captureStdout(t, func() error { return runWorkspaceMcpList(cmd, nil) })
	if err != nil {
		t.Fatalf("runWorkspaceMcpList: %v", err)
	}

	if gotMethod != http.MethodGet {
		t.Fatalf("method = %s, want GET", gotMethod)
	}
	if gotPath != "/api/workspaces/"+testWorkspaceMcpID+"/mcp-servers" {
		t.Fatalf("path = %q", gotPath)
	}
	if !strings.Contains(out, "linear") || !strings.Contains(out, testMcpServerID) {
		t.Fatalf("table output missing the server row: %q", out)
	}
}

func TestRunWorkspaceMcpListEmptyLibrary(t *testing.T) {
	mcpTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	})

	cmd := newWorkspaceMcpTestCmd("list")
	_ = cmd.Flags().Set("output", "table")
	out, err := captureStdout(t, func() error { return runWorkspaceMcpList(cmd, nil) })
	if err != nil {
		t.Fatalf("runWorkspaceMcpList: %v", err)
	}
	if !strings.Contains(out, "no MCP servers") {
		t.Fatalf("expected the empty-state line, got %q", out)
	}
}

// Both output modes, because the JSON one is the DEFAULT: a renderer that
// echoed the decoded response would leak there first.
func TestRunWorkspaceMcpNeverPrintsSecrets(t *testing.T) {
	for _, output := range []string{"json", "table"} {
		t.Run(output, func(t *testing.T) {
			mcpTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode([]map[string]any{poisonedServerResponse("linear")})
			})

			cmd := newWorkspaceMcpTestCmd("list")
			_ = cmd.Flags().Set("output", output)
			out, err := captureStdout(t, func() error { return runWorkspaceMcpList(cmd, nil) })
			if err != nil {
				t.Fatalf("runWorkspaceMcpList: %v", err)
			}
			for _, marker := range workspaceMcpLeakMarkers {
				if strings.Contains(out, marker) {
					t.Fatalf("%s output leaked %q: %q", output, marker, out)
				}
			}
			if !strings.Contains(out, "linear") {
				t.Fatalf("%s output dropped the server name: %q", output, out)
			}
		})
	}
}

// The write paths render the same shape, so they need the same guard — a write
// response is where a server is most likely to echo back what it just stored.
func TestRunWorkspaceMcpAddNeverPrintsSecrets(t *testing.T) {
	mcpTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(poisonedServerResponse("linear"))
	})

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

func TestRunWorkspaceMcpAddPostsNameAndEntry(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	mcpTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(serverResponse("linear"))
	})

	path := filepath.Join(t.TempDir(), "linear.json")
	if err := os.WriteFile(path, []byte(`{"url":"https://linear.example"}`), 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	cmd := newWorkspaceMcpTestCmd("add")
	_ = cmd.Flags().Set("server-config-file", path)
	if _, err := captureStdout(t, func() error { return runWorkspaceMcpAdd(cmd, []string{"linear"}) }); err != nil {
		t.Fatalf("runWorkspaceMcpAdd: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Fatalf("method = %s, want POST", gotMethod)
	}
	if gotPath != "/api/workspaces/"+testWorkspaceMcpID+"/mcp-servers" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotBody["name"] != "linear" {
		t.Fatalf("body name = %v, want linear", gotBody["name"])
	}
	entry, _ := gotBody["config"].(map[string]any)
	if entry == nil || entry["url"] != "https://linear.example" {
		t.Fatalf("body config = %v, want the file contents", gotBody["config"])
	}
}

func TestRunWorkspaceMcpAddRequiresAnInputChannel(t *testing.T) {
	mcpTestServer(t, func(w http.ResponseWriter, r *http.Request) {})

	cmd := newWorkspaceMcpTestCmd("add")
	err := runWorkspaceMcpAdd(cmd, []string{"linear"})
	if err == nil || !strings.Contains(err.Error(), "--server-config") {
		t.Fatalf("error = %v, want it to name the required flags", err)
	}
}

// `null` clears a whole document elsewhere; for a single entry it is far more
// likely to be a mistake, so it is rejected.
func TestRunWorkspaceMcpAddRejectsNullEntry(t *testing.T) {
	mcpTestServer(t, func(w http.ResponseWriter, r *http.Request) {})

	cmd := newWorkspaceMcpTestCmd("add")
	_ = cmd.Flags().Set("server-config", "null")
	if err := runWorkspaceMcpAdd(cmd, []string{"linear"}); err == nil {
		t.Fatal("expected 'null' to be rejected for a single server entry")
	}
}

func TestRunWorkspaceMcpUpdateSendsOnlyWhatChanged(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	mcpTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(serverResponse("linear-v2"))
	})

	cmd := newWorkspaceMcpTestCmd("update")
	_ = cmd.Flags().Set("name", "linear-v2")
	if _, err := captureStdout(t, func() error { return runWorkspaceMcpUpdate(cmd, []string{testMcpServerID}) }); err != nil {
		t.Fatalf("runWorkspaceMcpUpdate: %v", err)
	}

	if gotMethod != http.MethodPut {
		t.Fatalf("method = %s, want PUT", gotMethod)
	}
	if gotPath != "/api/workspaces/"+testWorkspaceMcpID+"/mcp-servers/"+testMcpServerID {
		t.Fatalf("path = %q", gotPath)
	}
	if gotBody["name"] != "linear-v2" {
		t.Fatalf("body name = %v, want linear-v2", gotBody["name"])
	}
	// A rename must not silently blank the stored entry.
	if _, sent := gotBody["config"]; sent {
		t.Fatalf("body sent config on a rename-only update: %v", gotBody)
	}
}

func TestRunWorkspaceMcpUpdateRequiresSomethingToChange(t *testing.T) {
	mcpTestServer(t, func(w http.ResponseWriter, r *http.Request) {})

	cmd := newWorkspaceMcpTestCmd("update")
	err := runWorkspaceMcpUpdate(cmd, []string{testMcpServerID})
	if err == nil || !strings.Contains(err.Error(), "nothing to update") {
		t.Fatalf("error = %v, want a nothing-to-update error", err)
	}
}

func TestRunWorkspaceMcpRemoveDeletesTheLibraryEntry(t *testing.T) {
	var gotMethod, gotPath string
	mcpTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})

	cmd := newWorkspaceMcpTestCmd("remove")
	if _, err := captureStdout(t, func() error { return runWorkspaceMcpRemove(cmd, []string{testMcpServerID}) }); err != nil {
		t.Fatalf("runWorkspaceMcpRemove: %v", err)
	}

	if gotMethod != http.MethodDelete {
		t.Fatalf("method = %s, want DELETE", gotMethod)
	}
	if gotPath != "/api/workspaces/"+testWorkspaceMcpID+"/mcp-servers/"+testMcpServerID {
		t.Fatalf("path = %q", gotPath)
	}
}

// The positional workspace argument is honoured, so an admin can target a
// workspace that is not their profile default.
func TestRunWorkspaceMcpUsesWorkspaceArgument(t *testing.T) {
	const wsUUID = "33333333-3333-3333-3333-333333333333"
	var gotPath string
	mcpTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	})

	cmd := newWorkspaceMcpTestCmd("list")
	if _, err := captureStdout(t, func() error { return runWorkspaceMcpList(cmd, []string{wsUUID}) }); err != nil {
		t.Fatalf("runWorkspaceMcpList: %v", err)
	}
	if gotPath != "/api/workspaces/"+wsUUID+"/mcp-servers" {
		t.Fatalf("path = %q, want the positional workspace to win", gotPath)
	}
}

func newAgentMcpTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "mcp"}
	cmd.Flags().String("workspace-id", "", "")
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("output", "table", "")
	return cmd
}

func TestRunAgentMcpAddAssignsAServer(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	mcpTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		server := serverResponse("linear")
		server["enabled"] = true
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{server})
	})

	cmd := newAgentMcpTestCmd()
	out, err := captureStdout(t, func() error { return runAgentMcpAdd(cmd, []string{testMcpAgentID, testMcpServerID}) })
	if err != nil {
		t.Fatalf("runAgentMcpAdd: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Fatalf("method = %s, want POST", gotMethod)
	}
	if gotPath != "/api/agents/"+testMcpAgentID+"/mcp-servers" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotBody["server_id"] != testMcpServerID {
		t.Fatalf("body server_id = %v, want %s", gotBody["server_id"], testMcpServerID)
	}
	if !strings.Contains(out, "enabled") {
		t.Fatalf("table output missing the assignment state: %q", out)
	}
}

func TestRunAgentMcpDisableAndEnable(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	mcpTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		server := serverResponse("linear")
		server["enabled"] = gotBody["enabled"]
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{server})
	})

	cmd := newAgentMcpTestCmd()
	out, err := captureStdout(t, func() error { return runAgentMcpDisable(cmd, []string{testMcpAgentID, testMcpServerID}) })
	if err != nil {
		t.Fatalf("runAgentMcpDisable: %v", err)
	}
	if gotPath != "/api/agents/"+testMcpAgentID+"/mcp-servers/"+testMcpServerID+"/enabled" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotBody["enabled"] != false {
		t.Fatalf("body enabled = %v, want false", gotBody["enabled"])
	}
	if !strings.Contains(out, "disabled") {
		t.Fatalf("table output should show the server as disabled: %q", out)
	}

	if _, err := captureStdout(t, func() error { return runAgentMcpEnable(cmd, []string{testMcpAgentID, testMcpServerID}) }); err != nil {
		t.Fatalf("runAgentMcpEnable: %v", err)
	}
	if gotBody["enabled"] != true {
		t.Fatalf("body enabled = %v, want true", gotBody["enabled"])
	}
}

func TestRunAgentMcpRemoveDropsTheAssignment(t *testing.T) {
	var gotMethod, gotPath string
	mcpTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	})

	cmd := newAgentMcpTestCmd()
	if _, err := captureStdout(t, func() error { return runAgentMcpRemove(cmd, []string{testMcpAgentID, testMcpServerID}) }); err != nil {
		t.Fatalf("runAgentMcpRemove: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Fatalf("method = %s, want DELETE", gotMethod)
	}
	if gotPath != "/api/agents/"+testMcpAgentID+"/mcp-servers/"+testMcpServerID {
		t.Fatalf("path = %q", gotPath)
	}
}

func TestRunAgentMcpListNeverPrintsSecrets(t *testing.T) {
	mcpTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		server := poisonedServerResponse("linear")
		server["enabled"] = true
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{server})
	})

	cmd := newAgentMcpTestCmd()
	_ = cmd.Flags().Set("output", "json")
	out, err := captureStdout(t, func() error { return runAgentMcpList(cmd, []string{testMcpAgentID}) })
	if err != nil {
		t.Fatalf("runAgentMcpList: %v", err)
	}
	for _, marker := range workspaceMcpLeakMarkers {
		if strings.Contains(out, marker) {
			t.Fatalf("agent mcp list leaked %q: %q", marker, out)
		}
	}
}
