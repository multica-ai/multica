package daemonmcp

import "encoding/json"

var taskBoundMulticaEnvKeys = [...]string{
	"MULTICA_TOKEN",
	"MULTICA_SERVER_URL",
	"MULTICA_WORKSPACE_ID",
	"MULTICA_AGENT_ID",
	"MULTICA_TASK_ID",
}

// WithTaskEnv copies the task-bound Multica identity into the injected
// platform MCP server. Codex starts stdio MCP children from its own config and
// does not otherwise pass the task environment through to them.
func WithTaskEnv(raw json.RawMessage, taskEnv map[string]string) json.RawMessage {
	doc := decode(raw)
	servers, ok := doc["mcpServers"].(map[string]any)
	if !ok {
		return raw
	}
	server, ok := servers["multica"].(map[string]any)
	if !ok {
		return raw
	}

	env, _ := server["env"].(map[string]any)
	if env == nil {
		env = make(map[string]any, len(taskBoundMulticaEnvKeys))
	}
	changed := false
	for _, key := range taskBoundMulticaEnvKeys {
		if value := taskEnv[key]; value != "" {
			env[key] = value
			changed = true
		}
	}
	if !changed {
		return raw
	}
	server["env"] = env

	encoded, err := json.Marshal(doc)
	if err != nil {
		return raw
	}
	return encoded
}
