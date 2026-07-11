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

func TestRegisterWorkflowToolsRegistersManagementSurface(t *testing.T) {
	srv := mcp.NewServer("test", "0")
	registerWorkflowTools(srv, cli.NewAPIClient("", "", ""))
	for _, name := range []string{"list_workflows", "get_workflow", "create_workflow", "update_workflow", "delete_workflow", "toggle_workflow", "activate_workflow", "get_active_workflow"} {
		if !hasTool(srv, name) {
			t.Errorf("expected tool %q", name)
		}
	}
}

func TestActivateWorkflowToolPostsIssueID(t *testing.T) {
	var body map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/cerebro/workflows/wf-1/activate" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_ = json.NewEncoder(w).Encode(map[string]any{"activated": true})
	}))
	defer ts.Close()

	srv := mcp.NewServer("test", "0")
	registerWorkflowTools(srv, cli.NewAPIClient(ts.URL, "ws-1", "tok"))
	res, err := srv.Call(context.Background(), "activate_workflow", map[string]any{"workflow_id": "wf-1", "issue_id": "issue-1"})
	if err != nil || res.IsError {
		t.Fatalf("call = %v, result = %#v", err, res)
	}
	if body["issue_id"] != "issue-1" {
		t.Fatalf("body = %#v", body)
	}
}
