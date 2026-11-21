package handler

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestQoderAgentWritesValidateEffectiveSettings(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	qoderRuntime := dbfx.Runtime(t, "qoder settings runtime", testutil.Cols{"provider": "qoder_cloud"})
	localRuntime := dbfx.Runtime(t, "local settings runtime")
	qoderAgent := dbfx.Agent(t, "qoder settings agent", qoderRuntime)
	modelAgent := dbfx.Agent(t, "local model agent", localRuntime, testutil.Cols{"model": "gpt-5"})
	var skillID string
	dbfx.QueryRow(t, `
		INSERT INTO skill (workspace_id, name, description, content, config, created_by)
		VALUES ($1, 'Qoder settings skill', '', '# Skill', '{}'::jsonb, $2)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&skillID)
	dbfx.Cleanup(t, `DELETE FROM skill WHERE id = $1`, skillID)

	put := func(handler http.HandlerFunc, agentID, path string, body map[string]any) *testutil.Response {
		return testutil.Call(t, handler, withURLParam(newRequest(http.MethodPut, "/api/agents/"+agentID+path, body), "id", agentID))
	}

	for name, body := range map[string]map[string]any{
		"model":       {"model": "gpt-5"},
		"custom args": {"custom_args": []string{"--verbose"}},
		"mcp config":  {"mcp_config": map[string]any{"mcpServers": map[string]any{"x": map[string]any{"command": "x"}}}},
	} {
		t.Run(name, func(t *testing.T) {
			put(testHandler.UpdateAgent, qoderAgent, "", body).Want(http.StatusBadRequest)
		})
	}
	put(testHandler.UpdateAgent, qoderAgent, "", map[string]any{"description": "still fine"}).Want(http.StatusOK)
	put(testHandler.UpdateAgent, qoderAgent, "", map[string]any{"model": ""}).Want(http.StatusOK)

	put(testHandler.UpdateAgent, modelAgent, "", map[string]any{"runtime_id": qoderRuntime, "runtime_config": map[string]any{"qoder_agent_id": "agent_1"}}).Want(http.StatusBadRequest)

	put(testHandler.UpdateAgentEnv, qoderAgent, "/env", map[string]any{"custom_env": map[string]string{"API_KEY": "x"}}).Want(http.StatusBadRequest)
	put(testHandler.SetAgentSkills, qoderAgent, "/skills", map[string]any{"skill_ids": []string{skillID}}).Want(http.StatusBadRequest)
	put(testHandler.SetAgentSkills, qoderAgent, "/skills", map[string]any{"skill_ids": []string{}}).Want(http.StatusOK)
}

func TestClaudeManagedAgentWritesValidateEffectiveSettings(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	claudeRuntime := dbfx.Runtime(t, "claude settings runtime", testutil.Cols{"provider": "claude_managed"})
	localRuntime := dbfx.Runtime(t, "local claude settings runtime")
	modelAgent := dbfx.Agent(t, "local model agent for claude", localRuntime, testutil.Cols{"model": "gpt-5"})
	plainAgent := dbfx.Agent(t, "local plain agent for claude", localRuntime)
	binding := map[string]any{"claude_agent_id": "agent_01abc"}

	create := func(body map[string]any) *testutil.Response {
		body["name"] = "claude settings " + t.Name() + fmt.Sprint(len(body))
		body["runtime_id"] = claudeRuntime
		return testutil.Call(t, testHandler.CreateAgent, newRequest(http.MethodPost, "/api/agents", body))
	}
	create(map[string]any{}).Want(http.StatusBadRequest)
	create(map[string]any{"runtime_config": map[string]any{"claude_agent_id": "not-an-agent"}}).Want(http.StatusBadRequest)
	create(map[string]any{"runtime_config": map[string]any{"claude_agent_id": "agent_01abc", "repository": map[string]any{"url": "http://example.com/x"}}}).Want(http.StatusBadRequest)
	create(map[string]any{"runtime_config": binding, "model": "claude-sonnet-5-5"}).Want(http.StatusBadRequest)
	var created AgentResponse
	create(map[string]any{"runtime_config": binding}).Want(http.StatusCreated).JSON(&created)
	dbfx.Cleanup(t, `DELETE FROM agent WHERE id = $1`, created.ID)

	put := func(agentID string, body map[string]any) *testutil.Response {
		return testutil.Call(t, testHandler.UpdateAgent, withURLParam(newRequest(http.MethodPut, "/api/agents/"+agentID, body), "id", agentID))
	}
	put(created.ID, map[string]any{"custom_args": []string{"--verbose"}}).Want(http.StatusBadRequest)
	put(created.ID, map[string]any{"runtime_config": map[string]any{}}).Want(http.StatusBadRequest)
	put(created.ID, map[string]any{"description": "still fine"}).Want(http.StatusOK)
	put(modelAgent, map[string]any{"runtime_id": claudeRuntime, "runtime_config": binding}).Want(http.StatusBadRequest)
	put(plainAgent, map[string]any{"runtime_id": claudeRuntime}).Want(http.StatusBadRequest)
	put(plainAgent, map[string]any{"runtime_id": claudeRuntime, "runtime_config": binding}).Want(http.StatusOK)
}
