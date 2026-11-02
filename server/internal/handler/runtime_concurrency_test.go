package handler

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestUpdateAgentRuntime_MaxConcurrentTasks covers the runtime-wide cap
// (GitHub #9626): a PATCH sets it, JSON null clears it, an absent field
// leaves it unchanged, out-of-range / non-integer values are rejected before
// any write, and a plain member cannot change someone else's runtime.
func TestUpdateAgentRuntime_MaxConcurrentTasks(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID, runtimeOwnerID, plainMemberID := runtimeVisibilityFixture(t)

	decode := func(t *testing.T, body []byte) AgentRuntimeResponse {
		t.Helper()
		var resp AgentRuntimeResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		return resp
	}

	w := patchRuntimeCustomName(runtimeOwnerID, runtimeID, map[string]any{})
	if w.Code != http.StatusOK {
		t.Fatalf("empty PATCH: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if resp := decode(t, w.Body.Bytes()); resp.MaxConcurrentTasks != nil {
		t.Fatalf("new runtime should have no cap, got %d", *resp.MaxConcurrentTasks)
	}

	w = patchRuntimeCustomName(runtimeOwnerID, runtimeID, map[string]any{"max_concurrent_tasks": 1})
	if w.Code != http.StatusOK {
		t.Fatalf("set cap: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if resp := decode(t, w.Body.Bytes()); resp.MaxConcurrentTasks == nil || *resp.MaxConcurrentTasks != 1 {
		t.Fatalf("cap after set: got %v, want 1", resp.MaxConcurrentTasks)
	}

	// A PATCH for another field must not touch the cap.
	w = patchRuntimeCustomName(runtimeOwnerID, runtimeID, map[string]any{"custom_name": "GPU box"})
	if w.Code != http.StatusOK {
		t.Fatalf("rename: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if resp := decode(t, w.Body.Bytes()); resp.MaxConcurrentTasks == nil || *resp.MaxConcurrentTasks != 1 {
		t.Fatalf("cap after unrelated PATCH: got %v, want 1", resp.MaxConcurrentTasks)
	}

	for _, bad := range []any{0, 51, -1, 1.5, "2", true} {
		w = patchRuntimeCustomName(runtimeOwnerID, runtimeID, map[string]any{"max_concurrent_tasks": bad})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("cap=%v: expected 400, got %d: %s", bad, w.Code, w.Body.String())
		}
	}

	w = patchRuntimeCustomName(plainMemberID, runtimeID, map[string]any{"max_concurrent_tasks": 5})
	if w.Code != http.StatusForbidden {
		t.Fatalf("plain member: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	w = patchRuntimeCustomName(runtimeOwnerID, runtimeID, map[string]any{"max_concurrent_tasks": nil})
	if w.Code != http.StatusOK {
		t.Fatalf("clear cap: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if resp := decode(t, w.Body.Bytes()); resp.MaxConcurrentTasks != nil {
		t.Fatalf("cap after clear: got %d, want null", *resp.MaxConcurrentTasks)
	}
}
