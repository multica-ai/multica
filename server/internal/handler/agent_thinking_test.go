package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCreateAgent_ThinkingLevel_ValidationConsistency exercises the
// MUL-2339 invariant: when an HTTP caller sends a literal-invalid
// thinking_level the API MUST return 400, regardless of which other
// field combination the same request mutates. The constraint comes
// from Trump's PR1 review: "invalid value 的 API 行为请保持一致，
// 不要同一类变更有时 400、有时静默清空".
func TestCreateAgent_ThinkingLevel_ValidationConsistency(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	claudeRuntimeID := createClaudeProviderRuntime(t)

	t.Cleanup(func() {
		testPool.Exec(ctx,
			`DELETE FROM agent WHERE workspace_id = $1 AND name LIKE 'thinking-test-%'`,
			testWorkspaceID,
		)
	})

	t.Run("empty value succeeds", func(t *testing.T) {
		body := map[string]any{
			"name":                 "thinking-test-empty",
			"runtime_id":           claudeRuntimeID,
			"visibility":           "private",
			"max_concurrent_tasks": 1,
			"thinking_level":       "",
		}
		w := httptest.NewRecorder()
		testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", body))
		if w.Code != http.StatusCreated {
			t.Fatalf("empty thinking_level: expected 201, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("known claude value succeeds", func(t *testing.T) {
		body := map[string]any{
			"name":                 "thinking-test-known",
			"runtime_id":           claudeRuntimeID,
			"visibility":           "private",
			"max_concurrent_tasks": 1,
			"thinking_level":       "high",
		}
		w := httptest.NewRecorder()
		testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", body))
		if w.Code != http.StatusCreated {
			t.Fatalf("thinking_level=high: expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		if resp["thinking_level"] != "high" {
			t.Errorf("expected thinking_level=high in response, got %v", resp["thinking_level"])
		}
	})

	t.Run("codex-only token rejected for claude runtime", func(t *testing.T) {
		// `none` is a valid Codex token but NOT a Claude token. The
		// gate must always 400 regardless of which other fields the
		// request also tried to change.
		body := map[string]any{
			"name":                 "thinking-test-codex-only",
			"runtime_id":           claudeRuntimeID,
			"visibility":           "private",
			"max_concurrent_tasks": 1,
			"thinking_level":       "none",
		}
		w := httptest.NewRecorder()
		testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", body))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("codex-only thinking_level on claude runtime: expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("garbage value rejected", func(t *testing.T) {
		body := map[string]any{
			"name":                 "thinking-test-garbage",
			"runtime_id":           claudeRuntimeID,
			"visibility":           "private",
			"max_concurrent_tasks": 1,
			"thinking_level":       "supersonic",
		}
		w := httptest.NewRecorder()
		testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", body))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("garbage thinking_level: expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})
}

// TestUpdateAgent_ThinkingLevel_TriState covers the three modes of
// the field on PATCH:
//   - field omitted → leave the existing value alone (the silent-clear
//     anti-pattern flagged by Trump's review must NOT happen here)
//   - explicit "" → clear back to NULL
//   - non-empty → validate against the CURRENT runtime's provider enum
//
// All three branches share the same 400 / 200 outcome rule: validation
// failures are always 400, never auto-clear.
func TestUpdateAgent_ThinkingLevel_TriState(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	claudeRuntimeID := createClaudeProviderRuntime(t)
	agentID := createAgentOnRuntime(t, "thinking-update-test", claudeRuntimeID, "high")

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, agentID)
	})

	// 1. Omitted field — name-only update must NOT touch thinking_level.
	t.Run("omitted field leaves value alone", func(t *testing.T) {
		body := map[string]any{
			"name": "thinking-update-test-renamed",
		}
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, body), "id", agentID)
		testHandler.UpdateAgent(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("name-only update: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		if resp["thinking_level"] != "high" {
			t.Errorf("name-only update silently changed thinking_level: got %v, want high", resp["thinking_level"])
		}
	})

	// 2. Explicit "" — must clear.
	t.Run("empty string clears", func(t *testing.T) {
		body := map[string]any{
			"thinking_level": "",
		}
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, body), "id", agentID)
		testHandler.UpdateAgent(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("clear update: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		if resp["thinking_level"] != "" {
			t.Errorf("empty thinking_level should clear: got %v", resp["thinking_level"])
		}
	})

	// 3. Garbage value — always 400, never silently clear.
	t.Run("garbage value is always 400", func(t *testing.T) {
		body := map[string]any{
			"thinking_level": "warp-speed",
		}
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, body), "id", agentID)
		testHandler.UpdateAgent(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("garbage thinking_level: expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	// 4. Codex-only token while bound to a Claude runtime → 400. This
	//    is the "consistency" case from Trump's review: the API does
	//    NOT auto-clear or coerce; the same token that's valid for a
	//    Codex runtime is rejected here.
	t.Run("codex token on claude runtime is 400, not silent clear", func(t *testing.T) {
		body := map[string]any{
			"thinking_level": "minimal",
		}
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, body), "id", agentID)
		testHandler.UpdateAgent(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("codex token on claude runtime: expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})
}

// createClaudeProviderRuntime stands up a runtime row with provider
// "claude" so the thinking_level gate runs against the real Claude
// enum (the default test runtime uses a fake provider). The runtime
// is workspace-private but visible to the test owner.
func createClaudeProviderRuntime(t *testing.T) string {
	t.Helper()
	var runtimeID string
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, last_seen_at, owner_id
		)
		VALUES ($1, NULL, $2, 'cloud', 'claude', 'online', $3, '{}'::jsonb, now(), $4)
		RETURNING id
	`, testWorkspaceID, "Claude Thinking Runtime", "Claude thinking-level test runtime", testUserID).Scan(&runtimeID)
	if err != nil {
		t.Fatalf("create claude runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})
	return runtimeID
}

// createAgentOnRuntime seeds an agent row bound to the given runtime
// with the given initial thinking_level (empty for NULL).
func createAgentOnRuntime(t *testing.T, name, runtimeID, level string) string {
	t.Helper()
	var agentID string
	var levelArg any
	if level == "" {
		levelArg = nil
	} else {
		levelArg = level
	}
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args, thinking_level
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'private', 1, $4, '', '{}'::jsonb, '[]'::jsonb, $5)
		RETURNING id
	`, testWorkspaceID, name, runtimeID, testUserID, levelArg).Scan(&agentID)
	if err != nil {
		t.Fatalf("create agent on runtime %s: %v", runtimeID, err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})
	return agentID
}
