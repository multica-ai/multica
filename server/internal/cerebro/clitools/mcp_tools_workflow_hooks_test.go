package clitools

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

func TestWorkflowHookMCPRegistersCompleteContract(t *testing.T) {
	server := mcp.NewServer("test", "test")
	registerWorkflowHookTools(server, &cli.APIClient{})
	tools := server.Tools()
	want := map[string]bool{
		"list_workflow_hooks": false, "get_workflow_hook": false,
		"create_workflow_hook": false, "update_workflow_hook": false,
		"test_workflow_hook": false, "publish_workflow_hook": false,
		"get_effective_workflow_hooks": false, "list_workflow_hook_runs": false,
	}
	for _, tool := range tools {
		if _, ok := want[tool.Name]; ok {
			want[tool.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("missing MCP tool %s", name)
		}
	}
}
