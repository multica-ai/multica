package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/cobra"
)

func TestRunRuntimeDiagnosticsJSONUsesSharedRESTContract(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/runtimes/runtime-1/access-diagnostics" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"runtime_id": "runtime-1",
			"provider":   "claude",
			"status":     "online",
			"diagnostics": []map[string]any{{
				"code": "mcp_discovery", "state": "stale", "title": "MCP discovery",
				"message": "old", "source_policy": "MCP tools/list", "recovery_action": "Run Scan now.",
				"version": "sha256:abc",
			}},
		})
	}))
	t.Cleanup(srv.Close)
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "workspace-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := &cobra.Command{Use: "diagnostics"}
	cmd.Flags().String("output", "json", "")
	out, err := captureStdout(t, func() error {
		return runRuntimeDiagnostics(cmd, []string{"runtime-1"})
	})
	if err != nil {
		t.Fatalf("runRuntimeDiagnostics: %v", err)
	}
	var report map[string]any
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode output %q: %v", out, err)
	}
	diagnostics, ok := report["diagnostics"].([]any)
	if !ok || len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v", report["diagnostics"])
	}
	diagnostic, _ := diagnostics[0].(map[string]any)
	if diagnostic["state"] != "stale" || diagnostic["source_policy"] != "MCP tools/list" || diagnostic["version"] != "sha256:abc" {
		t.Fatalf("diagnostic = %#v, want unchanged shared contract", diagnostic)
	}
}
