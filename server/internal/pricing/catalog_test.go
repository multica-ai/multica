package pricing

import (
	"encoding/json"
	"testing"
)

func TestGLMFlashArkAliasUsesSyncedPrice(t *testing.T) {
	lite := []byte(`{"zai/glm-5.3-flash":{"litellm_provider":"zai","mode":"chat","input_cost_per_token":0.00000015,"output_cost_per_token":0.0000005,"cache_read_input_token_cost":0.00000003}}`)
	models := []byte(`{"zai":{"models":{"glm-5.3-flash":{"cost":{"input":0.15,"output":0.5,"cache_read":0.03}}}}}`)
	catalog, err := BuildCatalog(lite, models)
	if err != nil {
		t.Fatal(err)
	}
	for _, model := range []string{"glm-5.3-flash", "glm-5-3-flash", "ark-coding-plan/glm-5-3-flash", "custom:ark-coding-plan/glm-5-3-flash"} {
		got, ok := Resolve(catalog, nil, model, "omp")
		if !ok || got.Input != 0.15 || got.Output != 0.5 || got.CacheRead != 0.03 {
			t.Fatalf("%s did not resolve the synced API rate: %+v, %v", model, got, ok)
		}
	}
	if _, ok := Resolve(catalog, nil, "glm-5-3-flash-extra", "omp"); ok {
		t.Fatal("unknown GLM variant borrowed the Flash price")
	}
	catalog.Rows["ark-coding-plan/glm-5-3-flash"] = Row{Input: 9, Output: 10}
	if got, _ := Resolve(catalog, nil, "ark-coding-plan/glm-5-3-flash", "omp"); got.Input != 9 {
		t.Fatalf("explicit provider rate was overridden: %+v", got)
	}
}

func TestSubscriptionUsesPublicAPIPrice(t *testing.T) {
	lite := []byte(`{"moonshot/kimi-k3":{"litellm_provider":"moonshot","mode":"chat","input_cost_per_token":0.000003,"output_cost_per_token":0.000015,"cache_read_input_token_cost":0.0000003}}`)
	models := []byte(`{"kimi-for-coding":{"models":{"k3-256k":{"family":"kimi-k3","cost":{"input":0,"output":0}}}},"moonshotai":{"models":{"kimi-k3":{"family":"kimi-k3","cost":{"input":3,"output":15,"cache_read":0.3}}}}}`)
	catalog, err := BuildCatalog(lite, models)
	if err != nil {
		t.Fatal(err)
	}
	for _, model := range []string{"k3-256k", "custom:k3-256k", "kimi-code/k3-256k", "custom:kimi-for-coding/k3-256k", "kimi-k3"} {
		got, ok := Resolve(catalog, nil, model, "hermes")
		if !ok || got.Input != 3 || got.Output != 15 || got.CacheRead != 0.3 {
			t.Fatalf("%s: %+v, %v", model, got, ok)
		}
	}
	if _, ok := Resolve(catalog, nil, "k3-256k-other", "hermes"); ok {
		t.Fatal("unknown variant borrowed a price")
	}
}

func TestOverridesWinAndRemainScoped(t *testing.T) {
	catalog := Bundled()
	overrides := map[string]Row{"hermes/custom:k3-256k": {Input: 7, Output: 9}, "gpt-5.5": {Input: 1, Output: 2}}
	got, ok := Resolve(catalog, overrides, "custom:k3-256k", "hermes")
	if !ok || got.Input != 7 {
		t.Fatalf("override = %+v", got)
	}
	got, ok = Resolve(catalog, nil, "custom:k3-256k", "hermes")
	if !ok || got.Input == 7 {
		t.Fatalf("override leaked = %+v", got)
	}
	got, _ = Resolve(catalog, overrides, "gpt-5.5", "codex")
	if got.Input != 1 {
		t.Fatalf("built-in shadowed override: %+v", got)
	}
}

func TestCatalogValidation(t *testing.T) {
	for _, payload := range []string{`{}`, `null`, `{"error":"upstream unavailable"}`, `{"x":{"mode":"chat","input_cost_per_token":-1,"output_cost_per_token":1}}`} {
		if _, err := BuildCatalog([]byte(payload), []byte(`{}`)); err == nil {
			t.Fatalf("accepted %s", payload)
		}
	}
	var row Row
	if err := json.Unmarshal([]byte(`{"input":null,"output":1,"cacheRead":0,"cacheWrite":0}`), &row); err == nil {
		t.Fatal("accepted null rate")
	}
}

func TestProviderPricesDoNotMix(t *testing.T) {
	catalog, err := BuildCatalog([]byte(`{"moonshot/kimi-k3":{"litellm_provider":"moonshot","mode":"chat","input_cost_per_token":0.000003,"output_cost_per_token":0.000015},"together_ai/kimi-k3":{"litellm_provider":"together_ai","mode":"chat","input_cost_per_token":0.000006,"output_cost_per_token":0.00002}}`), []byte(`{"moonshotai":{"models":{"kimi-k3":{"cost":{"input":3,"output":15}}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := Resolve(catalog, nil, "together_ai/kimi-k3", "hermes")
	if got.Input != 6 {
		t.Fatalf("provider price lost: %+v", got)
	}
	got, _ = Resolve(catalog, nil, "kimi-k3", "hermes")
	if got.Input != 3 {
		t.Fatalf("default API price changed: %+v", got)
	}
}

func TestExplicitServingProviderSeparators(t *testing.T) {
	reseller := Row{Input: 9, Output: 30, CacheRead: 0.9, CacheWrite: 9}
	override := Row{Input: 12, Output: 40, CacheRead: 1.2, CacheWrite: 12}
	catalog := Catalog{
		Rows: map[string]Row{
			"reseller/gpt-5.6-sol": reseller,
			"openai/gpt-5.6-sol":   {Input: 4, Output: 20, CacheRead: 0.4, CacheWrite: 5},
		},
		Aliases: map[string]string{"gpt-5.6-sol": "openai/gpt-5.6-sol"},
	}
	// These cases mirror the TypeScript resolver's serving-provider fixtures.
	for _, model := range []string{
		"reseller/gpt-5.6-sol",
		"reseller:gpt-5.6-sol",
		"custom:reseller:gpt-5.6-sol",
		"hermes/custom:reseller:gpt-5.6-sol",
		"reseller:gpt-5.6-sol[1m]",
	} {
		t.Run(model, func(t *testing.T) {
			if got, ok := Resolve(catalog, nil, model, "hermes"); !ok || got != reseller {
				t.Fatalf("serving-provider price = %+v, %v; want %+v", got, ok, reseller)
			}
			if got, ok := Resolve(catalog, map[string]Row{"reseller/gpt-5.6-sol": override}, model, "hermes"); !ok || got != override {
				t.Fatalf("serving-provider override = %+v, %v; want %+v", got, ok, override)
			}
		})
	}

	exact := Row{Input: 15, Output: 30, CacheRead: 1.5, CacheWrite: 15}
	catalog.Rows["reseller:gpt-5.6-sol"] = exact
	for _, provider := range []string{"hermes", "reseller"} {
		if got, ok := Resolve(catalog, nil, "reseller:gpt-5.6-sol", provider); !ok || got != exact {
			t.Fatalf("raw catalog key lost precedence for %s: %+v, %v", provider, got, ok)
		}
		if got, ok := Resolve(catalog, catalog.Rows, "reseller:gpt-5.6-sol", provider); !ok || got != exact {
			t.Fatalf("raw override key lost precedence for %s: %+v, %v", provider, got, ok)
		}
	}
}

func TestFreeAPIAndCanonicalSubscriptionOverride(t *testing.T) {
	lite := []byte(`{"moonshot/kimi-k3":{"litellm_provider":"moonshot","mode":"chat","input_cost_per_token":0.000003,"output_cost_per_token":0.000015}}`)
	models := []byte(`{"free-api":{"models":{"kimi-k3":{"family":"kimi-k3","cost":{"input":0,"output":0}}}},"kimi-for-coding":{"models":{"k3-256k":{"family":"kimi-k3","cost":{"input":0,"output":0}}}}}`)
	catalog, err := BuildCatalog(lite, models)
	if err != nil {
		t.Fatal(err)
	}
	free, ok := Resolve(catalog, nil, "free-api/kimi-k3", "hermes")
	if !ok || free.Input != 0 || free.Output != 0 {
		t.Fatalf("free API became subscription estimate: %+v", free)
	}
	catalog.Aliases["custom-subscription"] = "k3-256k"
	overrides := map[string]Row{"kimi-k3": {Input: 7, Output: 9}}
	got, ok := Resolve(catalog, overrides, "custom-subscription", "hermes")
	if !ok || got.Input != 7 {
		t.Fatalf("canonical override lost: %+v", got)
	}
}

func TestLiteLLMSubscriptionZeroUsesAPIReference(t *testing.T) {
	lite := []byte(`{"moonshot/kimi-k3":{"litellm_provider":"moonshot","mode":"chat","input_cost_per_token":0.000003,"output_cost_per_token":0.000015},"github_copilot/kimi-k3":{"litellm_provider":"github_copilot","mode":"chat","input_cost_per_token":0,"output_cost_per_token":0},"github_copilot/unknown-model":{"litellm_provider":"github_copilot","mode":"chat","input_cost_per_token":0,"output_cost_per_token":0}}`)
	models := []byte(`{"moonshotai":{"models":{"kimi-k3":{"cost":{"input":3,"output":15}}}}}`)
	catalog, err := BuildCatalog(lite, models)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := Resolve(catalog, nil, "github_copilot/kimi-k3", "hermes")
	if !ok || got.Input != 3 || got.Output != 15 {
		t.Fatalf("subscription lost its API estimate: %+v, %v", got, ok)
	}
	if _, ok := Resolve(catalog, nil, "github_copilot/unknown-model", "hermes"); ok {
		t.Fatal("unmapped subscription became a free API")
	}
}
