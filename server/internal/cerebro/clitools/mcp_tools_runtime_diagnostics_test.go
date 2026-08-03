package clitools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

func TestRuntimeAccessDiagnosticsMCPUsesSharedRESTContract(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/runtimes/runtime-1/access-diagnostics" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"runtime_id":"runtime-1","provider":"claude","status":"online","diagnostics":[{"code":"mcp_discovery","state":"success","title":"MCP discovery","message":"current","source_policy":"MCP tools/list","recovery_action":"none","version":"sha256:abc"}]}`))
	}))
	defer remote.Close()

	srv := mcp.NewServer("test", "0")
	registerRuntimeAccessDiagnosticsTools(srv, cli.NewAPIClient(remote.URL, "", ""))
	result, err := srv.Call(context.Background(), "get_runtime_access_diagnostics", map[string]any{"runtime_id": "runtime-1"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if len(result.Content) != 1 || !strings.Contains(result.Content[0].Text, `"version": "sha256:abc"`) {
		t.Fatalf("result = %#v", result)
	}
}
