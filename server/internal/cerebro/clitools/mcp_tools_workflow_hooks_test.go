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
		"list_active_hook_rules": false,
		"create_workflow_hook":   false, "update_workflow_hook": false,
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

func TestWorkflowHookMCPSchemaDocumentsConditionMode(t *testing.T) {
	server := mcp.NewServer("test", "test")
	registerWorkflowHookTools(server, &cli.APIClient{})

	for _, tool := range server.Tools() {
		if tool.Name != "create_workflow_hook" {
			continue
		}
		properties := tool.InputSchema["properties"].(map[string]any)
		hook := properties["hook"].(map[string]any)
		hookProperties := hook["properties"].(map[string]any)
		conditionMode := hookProperties["condition_mode"].(map[string]any)
		enum := conditionMode["enum"].([]string)
		if len(enum) != 2 || enum[0] != "all" || enum[1] != "any" {
			t.Fatalf("condition_mode enum = %#v, want [all any]", enum)
		}
		return
	}
	t.Fatal("create_workflow_hook tool not found")
}

func TestWorkflowHookMCPSchemaCarriesOptionalDraftRevision(t *testing.T) {
	server := mcp.NewServer("test", "test")
	registerWorkflowHookTools(server, &cli.APIClient{})

	for _, tool := range server.Tools() {
		if tool.Name != "update_workflow_hook" {
			continue
		}
		properties := tool.InputSchema["properties"].(map[string]any)
		hook := properties["hook"].(map[string]any)
		hookProperties := hook["properties"].(map[string]any)
		revision := hookProperties["revision"].(map[string]any)
		if revision["type"] != "integer" || revision["minimum"] != 1 {
			t.Fatalf("revision schema = %#v", revision)
		}
		return
	}
	t.Fatal("update_workflow_hook tool not found")
}

func TestWorkflowHookMCPTestUsesRetainedEventAndExactRevisionWithoutNewToolName(t *testing.T) {
	server := mcp.NewServer("test", "test")
	registerWorkflowHookTools(server, &cli.APIClient{})

	for _, tool := range server.Tools() {
		if tool.Name != "test_workflow_hook" {
			continue
		}
		properties := tool.InputSchema["properties"].(map[string]any)
		document := properties["hook"].(map[string]any)
		documentProperties := document["properties"].(map[string]any)
		if documentProperties["event_id"].(map[string]any)["type"] != "string" {
			t.Fatalf("event_id schema = %#v", documentProperties["event_id"])
		}
		if documentProperties["revision"].(map[string]any)["minimum"] != 1 {
			t.Fatalf("revision schema = %#v", documentProperties["revision"])
		}
		required := document["required"].([]string)
		if len(required) != 2 || required[0] != "event_id" || required[1] != "revision" {
			t.Fatalf("test document required = %#v", required)
		}
		return
	}
	t.Fatal("test_workflow_hook tool not found")
}
