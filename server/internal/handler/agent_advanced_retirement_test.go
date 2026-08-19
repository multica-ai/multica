package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/middleware"
)

func markHandlerTestUserAsVIBESMirror(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO vibes_user_mirror (vibes_user_id, multica_user_id, profile_email)
		VALUES ('vibes-agent-retirement-test-user', $1, 'retirement@example.test')
		ON CONFLICT (vibes_user_id) DO UPDATE
		SET multica_user_id = EXCLUDED.multica_user_id,
		    profile_email = EXCLUDED.profile_email
	`, testUserID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `
			DELETE FROM vibes_user_mirror
			WHERE vibes_user_id = 'vibes-agent-retirement-test-user'
		`)
	})
}

func asVIBESTagRequest(r *http.Request) *http.Request {
	return r.WithContext(middleware.SetClientMetadata(r.Context(), "vibes-tag-host", "test", ""))
}

func TestVIBESAgentCreateRejectsRetiredAdvancedFieldsButAllowsBasicFields(t *testing.T) {
	markHandlerTestUserAsVIBESMirror(t)

	advanced := httptest.NewRecorder()
	testHandler.CreateAgent(advanced, asVIBESTagRequest(newRequest(http.MethodPost, "/api/agents", map[string]any{
		"name":           "Retired advanced create",
		"runtime_id":     testRuntimeID,
		"runtime_config": map[string]any{"profile": "custom"},
	})))
	if advanced.Code != http.StatusGone {
		t.Fatalf("advanced create status = %d, want 410: %s", advanced.Code, advanced.Body.String())
	}

	basic := httptest.NewRecorder()
	testHandler.CreateAgent(basic, asVIBESTagRequest(newRequest(http.MethodPost, "/api/agents", map[string]any{
		"name":                 "Approved basic create",
		"description":          "basic role",
		"instructions":         "Do the approved work",
		"runtime_id":           testRuntimeID,
		"model":                "approved-model",
		"max_concurrent_tasks": 2,
	})))
	if basic.Code != http.StatusCreated {
		t.Fatalf("basic create status = %d, want 201: %s", basic.Code, basic.Body.String())
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE workspace_id = $1 AND name = 'Approved basic create'`, testWorkspaceID)
	})
}

func TestVIBESAgentAdvancedWritesAndSecretRevealsAreGone(t *testing.T) {
	markHandlerTestUserAsVIBESMirror(t)

	var agentID string
	if err := testPool.QueryRow(t.Context(), `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id,
			custom_env, custom_args, mcp_config, composio_toolkit_allowlist
		)
		VALUES (
			$1, 'Retired advanced existing', '', 'cloud', '{"profile":"legacy"}'::jsonb,
			$2, 'private', 'private', 1, $3,
			'{"TOKEN":"secret"}'::jsonb, '["--legacy"]'::jsonb,
			'{"servers":{"legacy":{"url":"https://example.test"}}}'::jsonb,
			ARRAY['github']::text[]
		)
		RETURNING id
	`, testWorkspaceID, testRuntimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})

	update := httptest.NewRecorder()
	testHandler.UpdateAgent(update, withURLParam(asVIBESTagRequest(newRequest(http.MethodPut, "/api/agents/"+agentID, map[string]any{
		"custom_args": []string{"--new"},
	})), "id", agentID))
	if update.Code != http.StatusGone {
		t.Fatalf("advanced update status = %d, want 410: %s", update.Code, update.Body.String())
	}

	reveal := httptest.NewRecorder()
	testHandler.GetAgentEnv(reveal, withURLParam(asVIBESTagRequest(newRequest(http.MethodGet, "/api/agents/"+agentID+"/env", nil)), "id", agentID))
	if reveal.Code != http.StatusGone {
		t.Fatalf("env reveal status = %d, want 410: %s", reveal.Code, reveal.Body.String())
	}

	mcpReveal := httptest.NewRecorder()
	testHandler.ListAgentMcpServers(mcpReveal, withURLParam(asVIBESTagRequest(newRequest(http.MethodGet, "/api/agents/"+agentID+"/mcp-servers", nil)), "id", agentID))
	if mcpReveal.Code != http.StatusGone {
		t.Fatalf("agent MCP reveal status = %d, want 410: %s", mcpReveal.Code, mcpReveal.Body.String())
	}

	detail := httptest.NewRecorder()
	testHandler.GetAgent(detail, withURLParam(asVIBESTagRequest(newRequest(http.MethodGet, "/api/agents/"+agentID, nil)), "id", agentID))
	if detail.Code != http.StatusOK {
		t.Fatalf("basic detail status = %d, want 200: %s", detail.Code, detail.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(detail.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if runtimeConfig, ok := body["runtime_config"].(map[string]any); !ok || len(runtimeConfig) != 0 {
		t.Fatalf("runtime_config exposed to VIBES browser: %#v", body["runtime_config"])
	}
	if customArgs, ok := body["custom_args"].([]any); !ok || len(customArgs) != 0 {
		t.Fatalf("custom_args exposed to VIBES browser: %#v", body["custom_args"])
	}
	if body["mcp_config"] != nil || body["has_custom_env"] != false || body["custom_env_key_count"] != float64(0) {
		t.Fatalf("secret-bearing Agent metadata exposed to VIBES browser: %#v", body)
	}
	if body["composio_toolkit_allowlist"] != nil {
		t.Fatalf("Composio allowlist exposed to VIBES browser: %#v", body["composio_toolkit_allowlist"])
	}

	basic := httptest.NewRecorder()
	testHandler.UpdateAgent(basic, withURLParam(asVIBESTagRequest(newRequest(http.MethodPut, "/api/agents/"+agentID, map[string]any{
		"name":         "Approved renamed agent",
		"instructions": "Keep approved instructions",
		"model":        "approved-model",
	})), "id", agentID))
	if basic.Code != http.StatusOK {
		t.Fatalf("basic update status = %d, want 200: %s", basic.Code, basic.Body.String())
	}
}

func TestVIBESAgentBuilderSessionsAreGone(t *testing.T) {
	markHandlerTestUserAsVIBESMirror(t)

	response := httptest.NewRecorder()
	testHandler.CreateAgentBuilderSession(response, asVIBESTagRequest(newRequest(http.MethodPost, "/api/agent-builder/sessions", map[string]any{
		"runtime_id": testRuntimeID,
	})))
	if response.Code != http.StatusGone {
		t.Fatalf("builder create status = %d, want 410: %s", response.Code, response.Body.String())
	}
}

func TestVIBESAgentAdvancedRouteFamiliesAreGone(t *testing.T) {
	markHandlerTestUserAsVIBESMirror(t)

	tests := []struct {
		name   string
		method string
		path   string
		invoke func(http.ResponseWriter, *http.Request)
	}{
		{"environment write", http.MethodPut, "/api/agents/agent-id/env", testHandler.UpdateAgentEnv},
		{"agent MCP add", http.MethodPost, "/api/agents/agent-id/mcp-servers", testHandler.AddAgentMcpServer},
		{"agent MCP toggle", http.MethodPut, "/api/agents/agent-id/mcp-servers/server-id", testHandler.SetAgentMcpServerEnabled},
		{"agent MCP remove", http.MethodDelete, "/api/agents/agent-id/mcp-servers/server-id", testHandler.RemoveAgentMcpServer},
		{"builder list", http.MethodGet, "/api/agent-builder/sessions", testHandler.ListAgentBuilderSessions},
		{"builder draft save", http.MethodPut, "/api/agent-builder/sessions/session-id/draft", testHandler.SaveAgentBuilderDraft},
		{"builder runtime switch", http.MethodPost, "/api/agent-builder/sessions/session-id/runtime", testHandler.SwitchAgentBuilderRuntime},
		{"lark install begin", http.MethodPost, "/api/workspaces/workspace-id/lark/install/begin", testHandler.BeginLarkInstall},
		{"lark install status", http.MethodGet, "/api/workspaces/workspace-id/lark/install/session-id/status", testHandler.GetLarkInstallStatus},
		{"slack Agent binding", http.MethodPost, "/api/workspaces/workspace-id/slack/install/byo", testHandler.RegisterSlackBYO},
		{"dingtalk Agent binding", http.MethodPost, "/api/workspaces/workspace-id/dingtalk/install/byo", testHandler.RegisterDingTalkBYO},
		{"dingtalk group reassignment", http.MethodPatch, "/api/workspaces/workspace-id/dingtalk/group-routes/route-id", testHandler.UpdateDingTalkGroupRoute},
		{"wecom Agent binding", http.MethodPost, "/api/workspaces/workspace-id/wecom/install/byo", testHandler.RegisterWecomBYO},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			test.invoke(response, asVIBESTagRequest(newRequest(test.method, test.path, map[string]any{})))
			if response.Code != http.StatusGone {
				t.Fatalf("status = %d, want 410: %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestVIBESMirrorDoesNotBreakRetainedNonTagExecutionCompatibility(t *testing.T) {
	markHandlerTestUserAsVIBESMirror(t)

	var agentID string
	if err := testPool.QueryRow(t.Context(), `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id
		)
		VALUES ($1, 'Retained native compatibility', '', 'local', '{}'::jsonb,
			$2, 'private', 'private', 1, $3)
		RETURNING id
	`, testWorkspaceID, testRuntimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})

	response := httptest.NewRecorder()
	testHandler.UpdateAgent(response, withURLParam(newRequest(http.MethodPut, "/api/agents/"+agentID, map[string]any{
		"custom_args": []string{"--retained-for-daemon"},
	}), "id", agentID))
	if response.Code != http.StatusOK {
		t.Fatalf("native compatibility update status = %d, want 200: %s", response.Code, response.Body.String())
	}

	var customArgs []byte
	if err := testPool.QueryRow(t.Context(), `SELECT custom_args FROM agent WHERE id = $1`, agentID).Scan(&customArgs); err != nil {
		t.Fatal(err)
	}
	if string(customArgs) != `["--retained-for-daemon"]` {
		t.Fatalf("custom_args = %s, want retained daemon value", customArgs)
	}
}
