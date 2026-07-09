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

// TestUpdateAgent_RuntimeSwitch_PreservesValidValueRejectsInvalid covers
// the gap Elon flagged in PR1 review: a PATCH that switches `runtime_id`
// without explicitly touching `thinking_level` used to silently keep
// the existing value, so a Claude agent storing `max` could land on a
// Codex runtime where `max` is not a recognised token at all, and the
// daemon would receive a literal-invalid level.
//
// The contract the test pins, matching the existing "always 400 on
// literal-invalid" rule:
//
//   - existing value still valid for the new runtime → 200, value kept
//   - existing value invalid for the new runtime → 400, never silent
//     clear or coerce
//   - caller can recover by re-sending with `thinking_level: ""` to clear
//     in the same PATCH
func TestUpdateAgent_RuntimeSwitch_PreservesValidValueRejectsInvalid(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	claudeRuntimeID := createClaudeProviderRuntime(t)
	codexRuntimeID := createCodexProviderRuntime(t)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent WHERE workspace_id = $1 AND name LIKE 'runtime-switch-%'`, testWorkspaceID)
	})

	t.Run("existing value still valid for new runtime is kept", func(t *testing.T) {
		// `high` is valid for both Claude and Codex enums — switching
		// runtime without touching thinking_level should keep it.
		agentID := createAgentOnRuntime(t, "runtime-switch-keep", claudeRuntimeID, "high")
		body := map[string]any{
			"runtime_id": codexRuntimeID,
		}
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, body), "id", agentID)
		testHandler.UpdateAgent(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 when existing value is still valid, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		if resp["thinking_level"] != "high" {
			t.Errorf("expected thinking_level=high preserved across runtime switch, got %v", resp["thinking_level"])
		}
	})

	t.Run("existing value invalid for new runtime is 400, not silent", func(t *testing.T) {
		// `max` is Claude-only; switching to Codex must NOT silently
		// keep it. Behaviour stays consistent with the explicit-set
		// path: always 400 on literal-invalid.
		agentID := createAgentOnRuntime(t, "runtime-switch-reject", claudeRuntimeID, "max")
		body := map[string]any{
			"runtime_id": codexRuntimeID,
		}
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, body), "id", agentID)
		testHandler.UpdateAgent(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 when existing value is invalid for new runtime, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("simultaneous explicit clear lets the switch through", func(t *testing.T) {
		// The 400 above is recoverable: pass `thinking_level: ""` in
		// the same PATCH and the switch goes through with a cleared
		// value. This is the documented escape hatch in the error
		// message; the test pins it so the contract holds.
		agentID := createAgentOnRuntime(t, "runtime-switch-clear", claudeRuntimeID, "max")
		body := map[string]any{
			"runtime_id":     codexRuntimeID,
			"thinking_level": "",
		}
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, body), "id", agentID)
		testHandler.UpdateAgent(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 with simultaneous clear, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		if resp["thinking_level"] != "" {
			t.Errorf("expected thinking_level cleared, got %v", resp["thinking_level"])
		}
	})

	t.Run("simultaneous explicit set to valid value lets the switch through", func(t *testing.T) {
		// The other recovery: caller picks a value valid for the new
		// runtime. Same PATCH, no need for a separate roundtrip.
		agentID := createAgentOnRuntime(t, "runtime-switch-replace", claudeRuntimeID, "max")
		body := map[string]any{
			"runtime_id":     codexRuntimeID,
			"thinking_level": "minimal",
		}
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, body), "id", agentID)
		testHandler.UpdateAgent(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 with simultaneous set, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		if resp["thinking_level"] != "minimal" {
			t.Errorf("expected thinking_level=minimal, got %v", resp["thinking_level"])
		}
	})
}

// TestUpdateAgent_RuntimeSwitch_ClearsKnownIncompatibleModel covers the
// runtime/model persistence bug from MUL-3341: a runtime_id-only PATCH used
// to preserve a provider-native model string, so switching a Claude Code
// agent to Codex could leave agent.model = "claude-..." and fail at task
// execution. Unknown custom models are intentionally preserved because the
// API supports manual entries and cannot prove they are invalid.
func TestUpdateAgent_RuntimeSwitch_ClearsKnownIncompatibleModel(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	claudeRuntimeID := createClaudeProviderRuntime(t)
	codexRuntimeID := createCodexProviderRuntime(t)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent WHERE workspace_id = $1 AND name LIKE 'runtime-model-switch-%'`, testWorkspaceID)
	})

	t.Run("runtime-only switch clears known foreign model", func(t *testing.T) {
		agentID := createAgentOnRuntimeWithModel(t, "runtime-model-switch-clear", claudeRuntimeID, "claude-sonnet-4-6")
		body := map[string]any{
			"runtime_id": codexRuntimeID,
		}
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, body), "id", agentID)
		testHandler.UpdateAgent(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 switching runtime with incompatible model, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		if resp["model"] != "" {
			t.Errorf("expected model cleared across Claude->Codex runtime switch, got %v", resp["model"])
		}
	})

	t.Run("runtime-only switch clears provider-prefixed model not accepted by target", func(t *testing.T) {
		agentID := createAgentOnRuntimeWithModel(t, "runtime-model-switch-prefixed", claudeRuntimeID, "openai/gpt-4o")
		body := map[string]any{
			"runtime_id": codexRuntimeID,
		}
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, body), "id", agentID)
		testHandler.UpdateAgent(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 switching runtime with provider-prefixed model, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		if resp["model"] != "" {
			t.Errorf("expected provider-prefixed model cleared across runtime switch, got %v", resp["model"])
		}
	})

	t.Run("runtime-only switch keeps exact target accepted model", func(t *testing.T) {
		agentID := createAgentOnRuntimeWithModel(t, "runtime-model-switch-accepted", claudeRuntimeID, "gpt-5.5")
		body := map[string]any{
			"runtime_id": codexRuntimeID,
		}
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, body), "id", agentID)
		testHandler.UpdateAgent(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 preserving exact target accepted model, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		if resp["model"] != "gpt-5.5" {
			t.Errorf("expected exact target model preserved, got %v", resp["model"])
		}
	})

	t.Run("explicit replacement model wins during switch", func(t *testing.T) {
		agentID := createAgentOnRuntimeWithModel(t, "runtime-model-switch-replace", claudeRuntimeID, "claude-sonnet-4-6")
		body := map[string]any{
			"runtime_id": codexRuntimeID,
			"model":      "gpt-5.5",
		}
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, body), "id", agentID)
		testHandler.UpdateAgent(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 with explicit replacement model, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		if resp["model"] != "gpt-5.5" {
			t.Errorf("expected explicit model to be persisted, got %v", resp["model"])
		}
	})

	t.Run("unknown custom model is preserved", func(t *testing.T) {
		agentID := createAgentOnRuntimeWithModel(t, "runtime-model-switch-custom", claudeRuntimeID, "private-lab-model")
		body := map[string]any{
			"runtime_id": codexRuntimeID,
		}
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, body), "id", agentID)
		testHandler.UpdateAgent(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 preserving unknown custom model, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		if resp["model"] != "private-lab-model" {
			t.Errorf("expected unknown custom model preserved, got %v", resp["model"])
		}
	})
}

// createCodexProviderRuntime mirrors createClaudeProviderRuntime but for
// the codex provider, so runtime-switch tests can exercise a real
// cross-provider transition.
func createCodexProviderRuntime(t *testing.T) string {
	t.Helper()
	var runtimeID string
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, last_seen_at, owner_id
		)
		VALUES ($1, NULL, $2, 'cloud', 'codex', 'online', $3, '{}'::jsonb, now(), $4)
		RETURNING id
	`, testWorkspaceID, "Codex Thinking Runtime", "Codex thinking-level test runtime", testUserID).Scan(&runtimeID)
	if err != nil {
		t.Fatalf("create codex runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})
	return runtimeID
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

func createAgentOnRuntimeWithModel(t *testing.T, name, runtimeID, model string) string {
	t.Helper()
	var agentID string
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args, model
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'private', 1, $4, '', '{}'::jsonb, '[]'::jsonb, $5)
		RETURNING id
	`, testWorkspaceID, name, runtimeID, testUserID, model).Scan(&agentID)
	if err != nil {
		t.Fatalf("create agent on runtime %s with model %s: %v", runtimeID, model, err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})
	return agentID
}
