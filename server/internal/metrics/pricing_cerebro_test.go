// CEREBRO (FIR-2698): fixture + tests for the registry-backed price lookup.
// TestMain installs a fixture lookup mirroring the registry seed rows the
// tests exercise, since the in-code price table this package carried moved to
// the model registry.
package metrics

import (
	"os"
	"strings"
	"testing"
)

// testPrices mirrors the model-registry seed (migration 9120) for the models
// this package's tests exercise. Fixture data, not a price source.
var testPrices = map[string]ModelPrice{
	"gpt-5.4":         {Provider: "openai", Model: "gpt-5.4", InputPerM: 2.50, CacheReadPerM: 0.25, CacheWritePerM: 2.50, OutputPerM: 15.00},
	"claude-fable-5":  {Provider: "anthropic", Model: "claude-fable-5", InputPerM: 10.00, CacheReadPerM: 1.00, CacheWritePerM: 12.50, OutputPerM: 50.00},
	"claude-sonnet-5": {Provider: "anthropic", Model: "claude-sonnet-5", InputPerM: 3.00, CacheReadPerM: 0.30, CacheWritePerM: 3.75, OutputPerM: 15.00},
}

// testLookup emulates the registry store's normalization for the fixture:
// lowercase, trim, and a "<x>[.-]<y>" tolerant contains-match on the key.
func testLookup(model string) (ModelPrice, bool) {
	key := strings.ToLower(strings.TrimSpace(model))
	if p, ok := testPrices[key]; ok {
		return p, true
	}
	for id, p := range testPrices {
		if strings.Contains(key, id) {
			return p, true
		}
	}
	return ModelPrice{}, false
}

func TestMain(m *testing.M) {
	SetRegistryPriceLookup(testLookup)
	os.Exit(m.Run())
}

func TestPriceForModelAlias_DelegatesToRegistryHook(t *testing.T) {
	p, ok := PriceForModelAlias("  GPT-5.4  ")
	if !ok || p.Provider != "openai" || p.InputPerM != 2.50 {
		t.Errorf("PriceForModelAlias(gpt-5.4) = %+v ok=%v", p, ok)
	}
	if _, ok := PriceForModelAlias("never-heard-of-it"); ok {
		t.Error("unknown model must be unpriced")
	}
}

func TestPriceForModelAlias_UnpricedWithoutHook(t *testing.T) {
	SetRegistryPriceLookup(nil)
	defer SetRegistryPriceLookup(testLookup)
	if _, ok := PriceForModelAlias("gpt-5.4"); ok {
		t.Error("without a hook everything must be unpriced")
	}
}
