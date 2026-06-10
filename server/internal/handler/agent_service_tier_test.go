package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateAgent_ServiceTier_CodexOnly(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	codexRuntimeID := createCodexProviderRuntime(t)
	claudeRuntimeID := createClaudeProviderRuntime(t)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent WHERE workspace_id = $1 AND name LIKE 'service-tier-create-%'`, testWorkspaceID)
	})

	t.Run("codex accepts and returns fast", func(t *testing.T) {
		body := map[string]any{
			"name":                 "service-tier-create-codex",
			"runtime_id":           codexRuntimeID,
			"visibility":           "private",
			"max_concurrent_tasks": 1,
			"service_tier":         "fast",
		}
		w := httptest.NewRecorder()
		testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", body))
		if w.Code != http.StatusCreated {
			t.Fatalf("service_tier=fast: expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		if resp["service_tier"] != "fast" {
			t.Fatalf("expected service_tier=fast in response, got %v", resp["service_tier"])
		}
	})

	t.Run("claude ignores and hides value", func(t *testing.T) {
		body := map[string]any{
			"name":                 "service-tier-create-claude",
			"runtime_id":           claudeRuntimeID,
			"visibility":           "private",
			"max_concurrent_tasks": 1,
			"service_tier":         "fast",
		}
		w := httptest.NewRecorder()
		testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", body))
		if w.Code != http.StatusCreated {
			t.Fatalf("claude service_tier ignored: expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		if _, ok := resp["service_tier"]; ok {
			t.Fatalf("non-Codex response must hide service_tier, got %v", resp["service_tier"])
		}
	})

	t.Run("codex rejects garbage", func(t *testing.T) {
		body := map[string]any{
			"name":                 "service-tier-create-garbage",
			"runtime_id":           codexRuntimeID,
			"visibility":           "private",
			"max_concurrent_tasks": 1,
			"service_tier":         "warp",
		}
		w := httptest.NewRecorder()
		testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", body))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("garbage service_tier: expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})
}

func TestUpdateAgent_ServiceTier_TriStateAndRuntimeSwitch(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	codexRuntimeID := createCodexProviderRuntime(t)
	claudeRuntimeID := createClaudeProviderRuntime(t)
	agentID := createAgentOnRuntimeWithServiceTier(t, "service-tier-update", codexRuntimeID, "fast")

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, agentID)
	})

	t.Run("omitted leaves value alone", func(t *testing.T) {
		body := map[string]any{"name": "service-tier-update-renamed"}
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, body), "id", agentID)
		testHandler.UpdateAgent(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("name-only update: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		if resp["service_tier"] != "fast" {
			t.Fatalf("name-only update changed service_tier: got %v", resp["service_tier"])
		}
	})

	t.Run("empty clears", func(t *testing.T) {
		body := map[string]any{"service_tier": ""}
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, body), "id", agentID)
		testHandler.UpdateAgent(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("clear update: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		if resp["service_tier"] != "" {
			t.Fatalf("empty service_tier should clear to empty response value, got %v", resp["service_tier"])
		}
	})

	t.Run("default sets standard tier", func(t *testing.T) {
		body := map[string]any{"service_tier": "default"}
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, body), "id", agentID)
		testHandler.UpdateAgent(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("default update: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		if resp["service_tier"] != "default" {
			t.Fatalf("service_tier=default not returned, got %v", resp["service_tier"])
		}
	})

	t.Run("non-codex update ignores and hides value", func(t *testing.T) {
		body := map[string]any{
			"runtime_id":   claudeRuntimeID,
			"service_tier": "fast",
		}
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, body), "id", agentID)
		testHandler.UpdateAgent(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("switch to claude with service_tier: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		if _, ok := resp["service_tier"]; ok {
			t.Fatalf("non-Codex response must hide service_tier, got %v", resp["service_tier"])
		}

		var stored any
		if err := testPool.QueryRow(ctx, `SELECT service_tier FROM agent WHERE id = $1`, agentID).Scan(&stored); err != nil {
			t.Fatalf("load stored service_tier: %v", err)
		}
		if stored != nil {
			t.Fatalf("service_tier should be cleared after switching to non-Codex runtime, got %#v", stored)
		}
	})
}

func TestClaimTask_ServiceTier_CodexOnly(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	codexRuntimeID := createCodexProviderRuntime(t)
	claudeRuntimeID := createClaudeProviderRuntime(t)
	codexAgentID := createAgentOnRuntimeWithServiceTier(t, "service-tier-claim-codex", codexRuntimeID, "fast")
	claudeAgentID := createAgentOnRuntimeWithServiceTier(t, "service-tier-claim-claude", claudeRuntimeID, "fast")
	queueServiceTierTask(t, codexAgentID, codexRuntimeID)
	queueServiceTierTask(t, claudeAgentID, claudeRuntimeID)

	codexAgent := claimAndDecodeAgent(t, codexRuntimeID)
	if codexAgent.ServiceTier != "fast" {
		t.Fatalf("codex claim service_tier = %q, want fast", codexAgent.ServiceTier)
	}

	claudeAgent := claimAndDecodeAgent(t, claudeRuntimeID)
	if claudeAgent.ServiceTier != "" {
		t.Fatalf("non-Codex claim should omit service_tier, got %q", claudeAgent.ServiceTier)
	}
}

func createAgentOnRuntimeWithServiceTier(t *testing.T, name, runtimeID, serviceTier string) string {
	t.Helper()
	var agentID string
	var serviceTierArg any
	if serviceTier != "" {
		serviceTierArg = serviceTier
	}
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args, service_tier
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'private', 1, $4, '', '{}'::jsonb, '[]'::jsonb, $5)
		RETURNING id
	`, testWorkspaceID, name, runtimeID, testUserID, serviceTierArg).Scan(&agentID)
	if err != nil {
		t.Fatalf("create agent on runtime %s: %v", runtimeID, err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})
	return agentID
}

func queueServiceTierTask(t *testing.T, agentID, runtimeID string) string {
	t.Helper()
	var taskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority)
		VALUES ($1, $2, 'queued', 0)
		RETURNING id
	`, agentID, runtimeID).Scan(&taskID); err != nil {
		t.Fatalf("queue service tier task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})
	return taskID
}
