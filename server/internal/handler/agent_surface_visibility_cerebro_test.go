// CEREBRO-PATCH(agent-surface-visibility): TECH-3670 — tests for per-surface
// discovery visibility parsing, normalization, response shaping, and the
// create round-trip wire contract.
package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseSurfaceVisibility(t *testing.T) {
	if parseSurfaceVisibility(nil) != nil {
		t.Fatal("nil blob must parse to nil map")
	}
	if parseSurfaceVisibility([]byte("not json")) != nil {
		t.Fatal("invalid blob must parse to nil map")
	}
	m := parseSurfaceVisibility([]byte(`{"lists":false,"chat":true}`))
	if m[agentSurfaceLists] != false || m[agentSurfaceChat] != true {
		t.Fatalf("unexpected parse: %v", m)
	}
}

func TestNormalizeSurfaceVisibilityInput(t *testing.T) {
	// Empty input → nil blob, valid (leave unchanged).
	if b, ok := normalizeSurfaceVisibilityInput(nil); !ok || b != nil {
		t.Fatalf("empty input: got (%v,%v)", b, ok)
	}
	// Unknown-only keys dropped → "{}".
	b, ok := normalizeSurfaceVisibilityInput(json.RawMessage(`{"bogus":true}`))
	if !ok || string(b) != "{}" {
		t.Fatalf("unknown-only input: got (%s,%v)", b, ok)
	}
	// Non-bool value → rejected (whole object fails map[string]bool unmarshal).
	if _, ok := normalizeSurfaceVisibilityInput(json.RawMessage(`{"lists":1}`)); ok {
		t.Fatal("non-bool value should be rejected")
	}
	// Valid surfaces preserved; unknown keys stripped.
	b, ok = normalizeSurfaceVisibilityInput(json.RawMessage(`{"lists":false,"chat":true}`))
	if !ok {
		t.Fatal("valid bool object should pass")
	}
	var m map[string]bool
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("re-encoded blob invalid: %v", err)
	}
	if m[agentSurfaceLists] != false || m[agentSurfaceChat] != true {
		t.Fatalf("surface values not preserved: %v", m)
	}
}

func TestSurfaceVisibilityResponseOmitsWhenUnset(t *testing.T) {
	if surfaceVisibilityResponse(nil) != nil {
		t.Fatal("unset column must yield nil response (field omitted)")
	}
	if surfaceVisibilityResponse([]byte("{}")) == nil {
		// Empty object is a valid "visible everywhere" map, not nil.
		t.Fatal("empty object should decode to a (non-nil) empty map")
	}
}

// TestCreateAgentSurfaceVisibilityRoundTrip exercises the full wire contract:
// a create request carrying surface_visibility persists and returns it, and an
// unknown surface key is stripped server-side.
func TestCreateAgentSurfaceVisibilityRoundTrip(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/agents", map[string]any{
		"name":       "Handler Surface Visibility",
		"runtime_id": handlerTestRuntimeID(t),
		"surface_visibility": map[string]any{
			"lists":   false,
			"mention": false,
			"chat":    true,
			"bogus":   true, // unknown key must be dropped
		},
	})
	testHandler.CreateAgent(w, req)
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

	if created.SurfaceVisibility == nil {
		t.Fatal("CreateAgent: expected surface_visibility in response")
	}
	if v, ok := created.SurfaceVisibility["lists"]; !ok || v != false {
		t.Fatalf("lists should be false, got %v (present=%v)", v, ok)
	}
	if v, ok := created.SurfaceVisibility["chat"]; !ok || v != true {
		t.Fatalf("chat should be true, got %v (present=%v)", v, ok)
	}
	if _, ok := created.SurfaceVisibility["bogus"]; ok {
		t.Fatal("unknown surface key must be stripped")
	}
}
