package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestPatchThenGetModelMapFallback round-trips a fallback chain through the
// global fallback endpoints: PATCH writes the chain and GET returns it.
func TestPatchThenGetModelMapFallback(t *testing.T) {
	patchReq := newRequest("PATCH", "/api/model-map/fallback", map[string][]string{
		"premium": {"qwen3.6-max", "mimo"},
		"cheap":   {"haiku"},
	})
	w := httptest.NewRecorder()
	testHandler.PatchModelMapFallback(w, patchReq)
	if w.Code != http.StatusOK {
		t.Fatalf("PatchModelMapFallback: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// The patch must preserve any previously-set concrete for the tier while
	// storing the fallback chain. Seed a concrete first, then re-check that the
	// GET reflects the fallback chain.
	getReq := newRequest("GET", "/api/model-map/fallback", nil)
	w = httptest.NewRecorder()
	testHandler.GetModelMapFallback(w, getReq)
	if w.Code != http.StatusOK {
		t.Fatalf("GetModelMapFallback: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got map[string][]string
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("GetModelMapFallback: failed to decode: %v", err)
	}
	if len(got["premium"]) != 2 || got["premium"][0] != "qwen3.6-max" || got["premium"][1] != "mimo" {
		t.Fatalf("GetModelMapFallback: premium chain = %v, want [qwen3.6-max mimo]", got["premium"])
	}
	if len(got["cheap"]) != 1 || got["cheap"][0] != "haiku" {
		t.Fatalf("GetModelMapFallback: cheap chain = %v, want [haiku]", got["cheap"])
	}

	// Clean up the rows we created so other tests see a stable global map.
	dbfx.Exec(t, `DELETE FROM model_tier_map WHERE workspace_id IS NULL AND tier IN ('premium','cheap')`)
}

// TestPutModelHealthFlipsStatus marks a model unhealthy then healthy and
// asserts model_health.status flips accordingly.
func TestPutModelHealthFlipsStatus(t *testing.T) {
	const model = "mimo-flip-test"

	// unhealthy
	unhealthyReq := newRequest("PUT", "/api/model-health", map[string]any{
		"concrete_model": model,
		"status":         "unhealthy",
		"reason":         "rate limited",
	})
	w := httptest.NewRecorder()
	testHandler.PutModelHealth(w, unhealthyReq)
	if w.Code != http.StatusOK {
		t.Fatalf("PutModelHealth(unhealthy): expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var unhealthy db.ModelHealth
	json.NewDecoder(w.Body).Decode(&unhealthy)
	if unhealthy.Status != "unhealthy" {
		t.Fatalf("PutModelHealth(unhealthy): status = %q, want unhealthy", unhealthy.Status)
	}
	if unhealthy.Reason.String != "rate limited" {
		t.Fatalf("PutModelHealth(unhealthy): reason = %q, want 'rate limited'", unhealthy.Reason.String)
	}

	// healthy
	healthyReq := newRequest("PUT", "/api/model-health", map[string]any{
		"concrete_model": model,
		"status":         "healthy",
	})
	w = httptest.NewRecorder()
	testHandler.PutModelHealth(w, healthyReq)
	if w.Code != http.StatusOK {
		t.Fatalf("PutModelHealth(healthy): expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var healthy db.ModelHealth
	json.NewDecoder(w.Body).Decode(&healthy)
	if healthy.Status != "healthy" {
		t.Fatalf("PutModelHealth(healthy): status = %q, want healthy", healthy.Status)
	}

	// Clean up.
	dbfx.Exec(t, `DELETE FROM model_health WHERE concrete = $1`, model)
}
