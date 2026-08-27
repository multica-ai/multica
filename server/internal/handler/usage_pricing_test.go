package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEmbeddedUsagePricingContract(t *testing.T) {
	var catalog struct {
		Version string `json:"version"`
		Units   struct {
			Rates              string `json:"rates"`
			CostTicksPerDollar int64  `json:"cost_usd_ticks_per_usd"`
		} `json:"units"`
		UncostedSemantics string `json:"uncosted_semantics"`
		Models            map[string]struct {
			Input      float64 `json:"input"`
			Output     float64 `json:"output"`
			CacheRead  float64 `json:"cache_read"`
			CacheWrite float64 `json:"cache_write"`
		} `json:"models"`
	}
	if err := json.Unmarshal(usagePricingV1Raw, &catalog); err != nil {
		t.Fatalf("decode pricing contract: %v", err)
	}
	if catalog.Version != "1" || catalog.Units.Rates != "usd_per_million_tokens" || catalog.Units.CostTicksPerDollar != 10_000_000_000 {
		t.Fatalf("unexpected protocol metadata: %+v", catalog)
	}
	if catalog.UncostedSemantics != "tokens_without_provider_reported_cost" {
		t.Fatalf("unexpected uncosted semantics: %q", catalog.UncostedSemantics)
	}
	gpt := catalog.Models["gpt-5.6-sol"]
	if gpt.Input != 5 || gpt.Output != 30 || gpt.CacheRead != 0.5 || gpt.CacheWrite != 6.25 {
		t.Fatalf("unexpected gpt-5.6-sol pricing: %+v", gpt)
	}
}

func TestGetUsagePricing(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	w := httptest.NewRecorder()
	testHandler.GetUsagePricing(w, newRequest("GET", "/api/usage/pricing", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var response struct {
		Version string `json:"version"`
		Models  map[string]struct {
			Input float64 `json:"input"`
		} `json:"models"`
	}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Version != "1" || response.Models["gpt-5.6-sol"].Input != 5 {
		t.Fatalf("unexpected pricing response: %+v", response)
	}
}
