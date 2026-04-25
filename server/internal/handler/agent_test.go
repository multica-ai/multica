package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateAgent_RejectsDuplicateName(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	// Clean up any agents created by this test.
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM agent WHERE workspace_id = $1 AND name = $2`,
			testWorkspaceID, "duplicate-name-test-agent",
		)
	})

	body := map[string]any{
		"name":                 "duplicate-name-test-agent",
		"description":          "first description",
		"runtime_id":           testRuntimeID,
		"visibility":           "private",
		"max_concurrent_tasks": 1,
	}

	// First call — creates the agent.
	w1 := httptest.NewRecorder()
	testHandler.CreateAgent(w1, newRequest(http.MethodPost, "/api/agents", body))
	if w1.Code != http.StatusCreated {
		t.Fatalf("first CreateAgent: expected 201, got %d: %s", w1.Code, w1.Body.String())
	}
	var resp1 map[string]any
	if err := json.NewDecoder(w1.Body).Decode(&resp1); err != nil {
		t.Fatalf("decode first response: %v", err)
	}
	agentID1, _ := resp1["id"].(string)
	if agentID1 == "" {
		t.Fatalf("first CreateAgent: no id in response: %v", resp1)
	}

	// Second call — same name must be rejected with 409 Conflict.
	// The unique constraint prevents silent duplicates; the UI shows a clear error.
	body["description"] = "updated description"
	w2 := httptest.NewRecorder()
	testHandler.CreateAgent(w2, newRequest(http.MethodPost, "/api/agents", body))
	if w2.Code != http.StatusConflict {
		t.Fatalf("second CreateAgent with duplicate name: expected 409, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestDeriveOpencodeGoModel(t *testing.T) {
	tests := []struct {
		name      string
		agentName string
		want      string
		wantOK    bool
	}{
		{
			name:      "deepseek flash",
			agentName: OpencodeAgentPrefix + "deepseek-v4-flash",
			want:      OpencodeModelPrefix + "deepseek-v4-flash",
			wantOK:    true,
		},
		{
			name:      "deepseek pro",
			agentName: OpencodeAgentPrefix + "deepseek-v4-pro",
			want:      OpencodeModelPrefix + "deepseek-v4-pro",
			wantOK:    true,
		},
		{
			name:      "kimi decimal version",
			agentName: OpencodeAgentPrefix + "kimi-k2-6",
			want:      OpencodeModelPrefix + "kimi-k2.6",
			wantOK:    true,
		},
		{
			name:      "qwen decimal version",
			agentName: OpencodeAgentPrefix + "qwen3-5-plus",
			want:      OpencodeModelPrefix + "qwen3.5-plus",
			wantOK:    true,
		},
		{
			name:      "qwen next decimal version",
			agentName: OpencodeAgentPrefix + "qwen3-6-plus",
			want:      OpencodeModelPrefix + "qwen3.6-plus",
			wantOK:    true,
		},
		{
			name:      "mimo decimal version",
			agentName: OpencodeAgentPrefix + "mimo-v2-5",
			want:      OpencodeModelPrefix + "mimo-v2.5",
			wantOK:    true,
		},
		{
			name:      "glm major version",
			agentName: OpencodeAgentPrefix + "glm-5",
			want:      OpencodeModelPrefix + "glm-5",
			wantOK:    true,
		},
		{
			name:      "glm minor version",
			agentName: OpencodeAgentPrefix + "glm-5-1",
			want:      OpencodeModelPrefix + "glm-5.1",
			wantOK:    true,
		},
		{
			name:      "multiple version groups",
			agentName: OpencodeAgentPrefix + "model-v1-2-3",
			want:      OpencodeModelPrefix + "model-v1.2.3",
			wantOK:    true,
		},
		{
			name:      "custom name",
			agentName: "custom-name",
			wantOK:    false,
		},
		{
			name:      "empty opencode suffix",
			agentName: OpencodeAgentPrefix,
			wantOK:    false,
		},
		{
			name:      "opencode seul sans tiret",
			agentName: ProviderOpencode,
			wantOK:    false,
		},
		{
			name:      "casing Opencode-glm-5",
			agentName: "Opencode-glm-5",
			wantOK:    false,
		},
		{
			name:      "trailing dash opencode-foo-",
			agentName: OpencodeAgentPrefix + "foo-",
			want:      OpencodeModelPrefix + "foo-",
			wantOK:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := deriveOpencodeGoModel(tt.agentName)
			if ok != tt.wantOK {
				t.Fatalf("deriveOpencodeGoModel(%q) ok = %v, want %v", tt.agentName, ok, tt.wantOK)
			}
			if got != tt.want {
				t.Fatalf("deriveOpencodeGoModel(%q) = %q, want %q", tt.agentName, got, tt.want)
			}
		})
	}
}

func TestCreateAgent_OpencodeProviderModelDefaults(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	tests := []struct {
		name      string
		provider  string
		agentName string
		model     string
		wantModel string
	}{
		{
			name:      "auto fills derived model",
			provider:  ProviderOpencode,
			agentName: OpencodeAgentPrefix + "qwen3-5-plus",
			wantModel: OpencodeModelPrefix + "qwen3.5-plus",
		},
		{
			name:      "keeps empty model for custom opencode name",
			provider:  ProviderOpencode,
			agentName: "custom-name-auto-model-test",
		},
		{
			name:      "preserves explicit model",
			provider:  ProviderOpencode,
			agentName: OpencodeAgentPrefix + "glm-5-1",
			model:     "custom/provider",
			wantModel: "custom/provider",
		},
		{
			name:      "does not fill for other provider",
			provider:  "handler_test_runtime",
			agentName: OpencodeAgentPrefix + "mimo-v2-5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtimeID := createHandlerTestRuntime(t, tt.provider)
			body := map[string]any{
				"name":                 tt.agentName,
				"runtime_id":           runtimeID,
				"visibility":           "private",
				"max_concurrent_tasks": 1,
			}
			if tt.model != "" {
				body["model"] = tt.model
			}

			created := createAgentForHandlerTest(t, body)
			if created.Model != tt.wantModel {
				t.Fatalf("CreateAgent model = %q, want %q", created.Model, tt.wantModel)
			}
		})
	}
}

func createHandlerTestRuntime(t *testing.T, provider string) string {
	t.Helper()

	var runtimeID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at
		)
		VALUES ($1, NULL, $2, 'cloud', $3, 'offline', $4, '{}'::jsonb, now())
		RETURNING id
	`, testWorkspaceID, "Handler Test Runtime "+t.Name(), provider, "Handler test runtime").Scan(&runtimeID); err != nil {
		t.Fatalf("create handler test runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	return runtimeID
}

func createAgentForHandlerTest(t *testing.T, body map[string]any) AgentResponse {
	t.Helper()

	w := httptest.NewRecorder()
	testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", body))
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAgent: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("CreateAgent: decode response: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, created.ID)
	})

	return created
}
