package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestCreateAgent_PiThinkingLevelValidation(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	piRuntimeID := createPiProviderRuntime(t)
	t.Cleanup(func() {
		testPool.Exec(ctx,
			`DELETE FROM agent WHERE workspace_id = $1 AND name LIKE 'pi-thinking-%'`,
			testWorkspaceID,
		)
	})

	t.Run("native max value succeeds", func(t *testing.T) {
		body := map[string]any{
			"name":                 "pi-thinking-max",
			"runtime_id":           piRuntimeID,
			"visibility":           "private",
			"max_concurrent_tasks": 1,
			"thinking_level":       "max",
		}
		w := httptest.NewRecorder()
		testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", body))
		if w.Code != http.StatusCreated {
			t.Fatalf("Pi thinking_level=max: expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		if resp["thinking_level"] != "max" {
			t.Fatalf("Pi thinking_level response = %v, want max", resp["thinking_level"])
		}
	})

	t.Run("non-Pi token is rejected", func(t *testing.T) {
		body := map[string]any{
			"name":                 "pi-thinking-ultra",
			"runtime_id":           piRuntimeID,
			"visibility":           "private",
			"max_concurrent_tasks": 1,
			"thinking_level":       "ultra",
		}
		w := httptest.NewRecorder()
		testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", body))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("Pi thinking_level=ultra: expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})
}

func TestAgentServiceTierValidationAndTriState(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	codexRuntimeID := createCodexProviderRuntime(t)
	claudeRuntimeID := createClaudeProviderRuntime(t)

	t.Run("create persists Codex catalog id", func(t *testing.T) {
		body := map[string]any{
			"name":                 "service-tier-create",
			"runtime_id":           codexRuntimeID,
			"visibility":           "private",
			"max_concurrent_tasks": 1,
			"model":                "gpt-5.6-sol",
			"service_tier":         "priority",
		}
		w := httptest.NewRecorder()
		testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", body))
		if w.Code != http.StatusCreated {
			t.Fatalf("create service_tier: expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		if resp["service_tier"] != "priority" {
			t.Fatalf("service_tier response = %v, want priority", resp["service_tier"])
		}
		agentID, _ := resp["id"].(string)
		t.Cleanup(func() {
			testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, agentID)
		})

		clear := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, map[string]any{
			"service_tier": "",
		}), "id", agentID)
		testHandler.UpdateAgent(clear, req)
		if clear.Code != http.StatusOK {
			t.Fatalf("clear service_tier: expected 200, got %d: %s", clear.Code, clear.Body.String())
		}
		var cleared map[string]any
		_ = json.NewDecoder(clear.Body).Decode(&cleared)
		if cleared["service_tier"] != "" {
			t.Fatalf("cleared service_tier = %v, want empty", cleared["service_tier"])
		}
	})

	t.Run("non-Codex runtime rejects tier", func(t *testing.T) {
		body := map[string]any{
			"name":                 "service-tier-rejected",
			"runtime_id":           claudeRuntimeID,
			"visibility":           "private",
			"max_concurrent_tasks": 1,
			"service_tier":         "priority",
		}
		w := httptest.NewRecorder()
		testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", body))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("Claude service_tier: expected 400, got %d: %s", w.Code, w.Body.String())
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

	// Direction is Codex → Claude: Codex's enum is a superset of Claude's
	// (it adds none/minimal plus the gpt-5.6 max/ultra levels), so a
	// Codex-only token is the value that becomes invalid on switch.
	t.Run("existing value still valid for new runtime is kept", func(t *testing.T) {
		// `high` is valid for both Codex and Claude enums — switching
		// runtime without touching thinking_level should keep it.
		agentID := createAgentOnRuntime(t, "runtime-switch-keep", codexRuntimeID, "high")
		body := map[string]any{
			"runtime_id": claudeRuntimeID,
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
		// `ultra` is Codex-only (added for the gpt-5.6 series); switching to
		// Claude must NOT silently keep it. Behaviour stays consistent with
		// the explicit-set path: always 400 on literal-invalid.
		agentID := createAgentOnRuntime(t, "runtime-switch-reject", codexRuntimeID, "ultra")
		body := map[string]any{
			"runtime_id": claudeRuntimeID,
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
		agentID := createAgentOnRuntime(t, "runtime-switch-clear", codexRuntimeID, "ultra")
		body := map[string]any{
			"runtime_id":     claudeRuntimeID,
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
		agentID := createAgentOnRuntime(t, "runtime-switch-replace", codexRuntimeID, "ultra")
		body := map[string]any{
			"runtime_id":     claudeRuntimeID,
			"thinking_level": "high",
		}
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, body), "id", agentID)
		testHandler.UpdateAgent(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 with simultaneous set, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		if resp["thinking_level"] != "high" {
			t.Errorf("expected thinking_level=high, got %v", resp["thinking_level"])
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
	secondClaudeRuntimeID := createClaudeProviderRuntime(t)
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

	t.Run("runtime-only switch keeps context-tagged target model", func(t *testing.T) {
		agentID := createAgentOnRuntimeWithModel(t, "runtime-model-switch-context-tag", claudeRuntimeID, "claude-opus-5[1m]")
		body := map[string]any{
			"runtime_id": secondClaudeRuntimeID,
		}
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, body), "id", agentID)
		testHandler.UpdateAgent(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 preserving context-tagged Claude model, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		if resp["model"] != "claude-opus-5[1m]" {
			t.Errorf("expected context-tagged Claude model preserved, got %v", resp["model"])
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

// TestThinkingLevelRejectionCopy pins WHY a thinking_level was refused, not
// just that it was. A runtime with no reasoning control must not be described
// as receiving an unrecognised value: "high" is a fine effort token, and the
// old shared sentence sent users looking for a spelling that cannot exist
// (MUL-5770).
//
// Copilot is the standing example: it discovers over ACP but executes through
// its own CLI, so no live ACP session exists to carry an effort. Hermes used
// to play this role and no longer can — jcode runs under that provider and
// applies an advertised effort, so the provider-level gate is open for it.
func TestThinkingLevelRejectionCopy(t *testing.T) {
	t.Parallel()

	t.Run("runtime without reasoning control names the capability gap", func(t *testing.T) {
		msg := thinkingLevelRejection("copilot", "high")
		if !strings.Contains(msg, "does not support a per-agent reasoning effort") {
			t.Errorf("expected a capability explanation, got %q", msg)
		}
		if strings.Contains(msg, "not a recognised value") {
			t.Errorf("capability gap must not read as a bad token, got %q", msg)
		}
		if !strings.Contains(msg, `"copilot"`) {
			t.Errorf("expected the runtime named in %q", msg)
		}
	})

	t.Run("runtime with reasoning control names the value", func(t *testing.T) {
		msg := thinkingLevelRejection("claude", "supersonic")
		if !strings.Contains(msg, "not a recognised value") {
			t.Errorf("expected a value-level explanation, got %q", msg)
		}
		if !strings.Contains(msg, `"supersonic"`) {
			t.Errorf("expected the rejected token echoed in %q", msg)
		}
	})

	t.Run("carry-over path always offers the clear escape hatch", func(t *testing.T) {
		for _, provider := range []string{"copilot", "claude"} {
			msg := existingThinkingLevelRejection(provider, "xhigh")
			if !strings.Contains(msg, `thinking_level=""`) {
				t.Errorf("provider %q: expected the clear instruction, got %q", provider, msg)
			}
		}
		if msg := existingThinkingLevelRejection("copilot", "xhigh"); !strings.Contains(msg, "does not support a per-agent reasoning effort") {
			t.Errorf("expected the capability explanation on the carry-over path, got %q", msg)
		}
	})
}

// TestCreateAgent_NoReasoningControlRejectsThinkingLevel covers the reported
// flow end-to-end: creating an agent on a runtime with no reasoning control
// 400s, and the body explains the capability gap rather than blaming the
// value. Empty stays valid — it means "runtime default".
//
// It also pins the other side of the MUL-5991 change: hermes, which used to be
// this test's subject, now accepts a level because jcode applies one.
func TestCreateAgent_NoReasoningControlRejectsThinkingLevel(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	gapRuntimeID := createProviderRuntime(t, "copilot")
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM agent WHERE workspace_id = $1 AND name LIKE '%-thinking-%'`,
			testWorkspaceID,
		)
	})

	t.Run("effort value rejected with a capability message", func(t *testing.T) {
		body := map[string]any{
			"name":                 "copilot-thinking-high",
			"runtime_id":           gapRuntimeID,
			"visibility":           "private",
			"max_concurrent_tasks": 1,
			"thinking_level":       "high",
		}
		w := httptest.NewRecorder()
		testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", body))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("copilot thinking_level=high: expected 400, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "does not support a per-agent reasoning effort") {
			t.Errorf("expected a capability explanation in the 400 body, got %s", w.Body.String())
		}
	})

	t.Run("empty value still creates", func(t *testing.T) {
		body := map[string]any{
			"name":                 "copilot-thinking-empty",
			"runtime_id":           gapRuntimeID,
			"visibility":           "private",
			"max_concurrent_tasks": 1,
			"thinking_level":       "",
		}
		w := httptest.NewRecorder()
		testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", body))
		if w.Code != http.StatusCreated {
			t.Fatalf("copilot empty thinking_level: expected 201, got %d: %s", w.Code, w.Body.String())
		}
	})

	// hermes with no discovered catalog must REFUSE, not accept. The catalog is
	// written only when a client asks for a model list, so a CLI-only caller can
	// sit in the undiscovered state forever — treating it as "supported" is the
	// permanent hole that let a Hermes Agent user persist a level the daemon
	// then drops. The message names the missing evidence rather than claiming a
	// limitation that would be false for jcode.
	t.Run("hermes with no discovered catalog is refused", func(t *testing.T) {
		hermesRuntimeID := createProviderRuntime(t, "hermes")
		body := map[string]any{
			"name":                 "hermes-thinking-high",
			"runtime_id":           hermesRuntimeID,
			"visibility":           "private",
			"max_concurrent_tasks": 1,
			"thinking_level":       "high",
		}
		w := httptest.NewRecorder()
		testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", body))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("hermes thinking_level=high with no catalog: expected 400, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "has not reported a model catalog yet") {
			t.Errorf("expected the not-yet-discovered explanation, got %s", w.Body.String())
		}
		if strings.Contains(w.Body.String(), "does not support") {
			t.Errorf("must not claim a limitation we have not established, got %s", w.Body.String())
		}
	})

	// Empty stays valid on the same runtime — "runtime default" needs no
	// capability evidence.
	t.Run("hermes with no catalog still accepts an empty value", func(t *testing.T) {
		hermesRuntimeID := createProviderRuntime(t, "hermes")
		body := map[string]any{
			"name":                 "hermes-thinking-empty",
			"runtime_id":           hermesRuntimeID,
			"visibility":           "private",
			"max_concurrent_tasks": 1,
			"thinking_level":       "",
		}
		w := httptest.NewRecorder()
		testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", body))
		if w.Code != http.StatusCreated {
			t.Fatalf("hermes empty thinking_level: expected 201, got %d: %s", w.Code, w.Body.String())
		}
	})
}

// TestUpdateAgent_ThinkingLevelCarriesOverToUndiscoveredHermes covers the
// branch where the caller never mentions thinking_level at all: an agent that
// already carries one is switched onto a hermes runtime whose capability we
// cannot confirm. Preserving the value there would smuggle a possibly-inert
// setting onto the new runtime, so the switch is refused with the escape hatch
// spelled out.
func TestUpdateAgent_ThinkingLevelCarriesOverToUndiscoveredHermes(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	claudeRuntimeID := createClaudeProviderRuntime(t)
	hermesRuntimeID := createProviderRuntime(t, "hermes")

	created := httptest.NewRecorder()
	testHandler.CreateAgent(created, newRequest(http.MethodPost, "/api/agents", map[string]any{
		"name":                 "carryover-thinking-agent",
		"runtime_id":           claudeRuntimeID,
		"visibility":           "private",
		"max_concurrent_tasks": 1,
		"thinking_level":       "high",
	}))
	if created.Code != http.StatusCreated {
		t.Fatalf("seed agent: expected 201, got %d: %s", created.Code, created.Body.String())
	}
	var seeded map[string]any
	_ = json.NewDecoder(created.Body).Decode(&seeded)
	agentID, _ := seeded["id"].(string)
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, agentID) })

	w := httptest.NewRecorder()
	testHandler.UpdateAgent(w, withURLParam(
		newRequest(http.MethodPatch, "/api/agents/"+agentID, map[string]any{
			"runtime_id": hermesRuntimeID,
		}), "id", agentID))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("carry-over onto undiscovered hermes: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	// Decode rather than substring-match the raw body: the escape hatch contains
	// quotes, which JSON-escapes to `thinking_level=\"\"` on the wire.
	var errBody struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(w.Body).Decode(&errBody); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if !strings.Contains(errBody.Error, "has not reported a model catalog yet") {
		t.Errorf("expected the not-yet-discovered explanation, got %q", errBody.Error)
	}
	if !strings.Contains(errBody.Error, `thinking_level=""`) {
		t.Errorf("expected the clear-it escape hatch, got %q", errBody.Error)
	}
}

// TestAcpThinkingDecision is the fix for the regression the hermes
// change introduced: `hermes` covers jcode (advertises an effort) and Hermes
// Agent (advertises none), and the provider name cannot tell them apart. The
// discovered catalog can, so the capability 400 survives for the binary that
// genuinely has no reasoning dial.
func TestAcpThinkingDecision(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	runtimeID := parseUUID("11111111-1111-1111-1111-111111111111")

	withCatalog := func(t *testing.T, models []ModelEntry) *Handler {
		t.Helper()
		cache := NewInMemoryModelCatalogCache()
		if err := cache.Put(ctx, uuidToString(runtimeID), models, nil, true); err != nil {
			t.Fatalf("seed catalog: %v", err)
		}
		return &Handler{ModelCatalogCache: cache}
	}

	withEffort := []ModelEntry{{
		ID:       "gpt-5.6-sol",
		Thinking: &ModelThinking{SupportedLevels: []ThinkingLevel{{Value: "high", Label: "High"}}},
	}}
	withoutEffort := []ModelEntry{{ID: "hermes-4"}}

	t.Run("jcode-shaped catalog keeps the level accepted", func(t *testing.T) {
		if got := withCatalog(t, withEffort).acpThinkingDecision(ctx, "hermes", runtimeID); got != acpEffortPresent {
			t.Errorf("decision = %v, want acpEffortPresent", got)
		}
	})

	t.Run("Hermes-Agent-shaped catalog restores the capability answer", func(t *testing.T) {
		if got := withCatalog(t, withoutEffort).acpThinkingDecision(ctx, "hermes", runtimeID); got != acpEffortAbsent {
			t.Errorf("decision = %v, want acpEffortAbsent", got)
		}
	})

	// Undiscovered is its own answer for an ambiguous provider — not "supported".
	// The catalog is only written once a client asks for a model list, so this
	// state persists indefinitely for CLI-only callers.
	t.Run("no cache is unknown for hermes", func(t *testing.T) {
		if got := (&Handler{}).acpThinkingDecision(ctx, "hermes", runtimeID); got != acpEffortUnknown {
			t.Errorf("decision = %v, want acpEffortUnknown", got)
		}
	})

	t.Run("empty catalog is unknown for hermes", func(t *testing.T) {
		if got := withCatalog(t, nil).acpThinkingDecision(ctx, "hermes", runtimeID); got != acpEffortUnknown {
			t.Errorf("decision = %v, want acpEffortUnknown", got)
		}
	})

	// reasonix names exactly one binary, and that binary supports an effort, so
	// an undiscovered reasonix runtime is allowed rather than blocked before its
	// first discovery. Tightening hermes must not tighten this.
	t.Run("no cache still allows reasonix", func(t *testing.T) {
		if got := (&Handler{}).acpThinkingDecision(ctx, "reasonix", runtimeID); got != acpEffortPresent {
			t.Errorf("decision = %v, want acpEffortPresent — reasonix is unambiguous", got)
		}
	})

	// Providers outside the ACP-catalog set are answered by name alone; this
	// check must not start second-guessing them from a catalog.
	t.Run("non-ACP provider is not consulted", func(t *testing.T) {
		if got := withCatalog(t, withoutEffort).acpThinkingDecision(ctx, "claude", runtimeID); got != acpEffortPresent {
			t.Errorf("decision = %v, want acpEffortPresent — claude is decided by provider", got)
		}
	})
}

// createProviderRuntime stands up a runtime row for the given provider so a
// test can exercise provider-level gates end-to-end.
func createProviderRuntime(t *testing.T, provider string) string {
	t.Helper()
	var runtimeID string
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, last_seen_at, owner_id
		)
		VALUES ($1, NULL, $2, 'cloud', $5, 'online', $3, '{}'::jsonb, now(), $4)
		RETURNING id
	`, testWorkspaceID, provider+" Thinking Runtime", provider+" thinking-level test runtime", testUserID, provider).Scan(&runtimeID)
	if err != nil {
		t.Fatalf("create %s runtime: %v", provider, err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})
	return runtimeID
}

func createPiProviderRuntime(t *testing.T) string {
	t.Helper()
	var runtimeID string
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, last_seen_at, owner_id
		)
		VALUES ($1, NULL, $2, 'cloud', 'pi', 'online', $3, '{}'::jsonb, now(), $4)
		RETURNING id
	`, testWorkspaceID, "Pi Thinking Runtime", "Pi thinking-level test runtime", testUserID).Scan(&runtimeID)
	if err != nil {
		t.Fatalf("create pi runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})
	return runtimeID
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
