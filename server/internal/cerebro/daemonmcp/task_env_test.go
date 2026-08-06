package daemonmcp

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestWithTaskEnvForMulticaServer(t *testing.T) {
	raw := json.RawMessage(`{
		"mcpServers": {
			"multica": {
				"command": "/opt/multica",
				"args": ["mcp", "serve"],
				"env": {"KEEP": "existing"}
			}
		}
	}`)
	taskEnv := map[string]string{
		"MULTICA_TOKEN":        "task-token",
		"MULTICA_SERVER_URL":   "https://multica.example.test",
		"MULTICA_WORKSPACE_ID": "workspace-id",
		"MULTICA_AGENT_ID":     "agent-id",
		"MULTICA_TASK_ID":      "task-id",
		"OPENAI_API_KEY":       "must-not-be-forwarded",
		"EMPTY_VALUE":          "",
	}

	got := WithTaskEnv(raw, taskEnv)

	var decoded struct {
		McpServers map[string]struct {
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	server := decoded.McpServers["multica"]
	if server.Command != "/opt/multica" || !reflect.DeepEqual(server.Args, []string{"mcp", "serve"}) {
		t.Fatalf("platform server command changed: %+v", server)
	}
	wantEnv := map[string]string{
		"KEEP":                 "existing",
		"MULTICA_TOKEN":        "task-token",
		"MULTICA_SERVER_URL":   "https://multica.example.test",
		"MULTICA_WORKSPACE_ID": "workspace-id",
		"MULTICA_AGENT_ID":     "agent-id",
		"MULTICA_TASK_ID":      "task-id",
	}
	if !reflect.DeepEqual(server.Env, wantEnv) {
		t.Fatalf("task env mismatch\n got: %#v\nwant: %#v", server.Env, wantEnv)
	}
}
