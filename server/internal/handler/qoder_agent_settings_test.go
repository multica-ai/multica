package handler

import (
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
