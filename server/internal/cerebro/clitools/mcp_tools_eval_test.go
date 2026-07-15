package clitools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

func TestRegisterEvalToolsRegistersSharedSurface(t *testing.T) {
	server := mcp.NewServer("test", "0")
	registerEvalTools(server, cli.NewAPIClient("", "", ""))
	for _, name := range []string{"list_evals", "get_eval", "create_eval", "update_eval", "delete_eval", "list_eval_runs", "record_eval_run", "list_eval_bindings", "bind_eval", "unbind_eval"} {
		if !hasTool(server, name) {
			t.Errorf("expected tool %q", name)
		}
	}
}

func TestRecordEvalRunUsesEvalRoute(t *testing.T) {
	var gotPath string
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "run-1"})
	}))
	defer httpServer.Close()
	server := mcp.NewServer("test", "0")
	registerEvalTools(server, cli.NewAPIClient(httpServer.URL, "workspace-1", "token"))
	result, err := server.Call(context.Background(), "record_eval_run", map[string]any{"eval_id": "eval-1", "run": map[string]any{"status": "passed", "results": map[string]any{}}})
	if err != nil || result.IsError {
		t.Fatalf("call failed: result=%#v err=%v", result, err)
	}
	if gotPath != "/api/cerebro/evals/eval-1/runs" {
		t.Fatalf("path = %q", gotPath)
	}
}
