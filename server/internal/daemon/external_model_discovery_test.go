package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseCLIModelsJSON_Full(t *testing.T) {
	t.Parallel()
	input := `{
		"models": [
			{
				"id": "deepseek-v4-pro-ioa",
				"label": "DeepSeek V4 Pro",
				"provider": "deepseek",
				"default": true,
				"thinking": {
					"supported_levels": [
						{"value": "none", "label": "None"},
						{"value": "low", "label": "Low"},
						{"value": "high", "label": "High"}
					],
					"default_level": "high"
				},
				"pricing": {
					"input": 0.5,
					"output": 1.5,
					"cacheRead": 0.05,
					"cacheWrite": 0.5
				}
			},
			{
				"id": "hunyuan-t1",
				"label": "Hunyuan T1"
			}
		]
	}`

	result, err := ParseCLIModelsJSON([]byte(input))
	if err != nil {
		t.Fatalf("ParseCLIModelsJSON: %v", err)
	}
	if len(result.Models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(result.Models))
	}

	m0 := result.Models[0]
	if m0.ID != "deepseek-v4-pro-ioa" {
		t.Errorf("model[0].ID = %q, want deepseek-v4-pro-ioa", m0.ID)
	}
	if m0.Label != "DeepSeek V4 Pro" {
		t.Errorf("model[0].Label = %q", m0.Label)
	}
	if !m0.Default {
		t.Errorf("model[0].Default should be true")
	}
	if m0.Thinking == nil {
		t.Fatal("model[0].Thinking is nil")
	}
	if len(m0.Thinking.SupportedLevels) != 3 {
		t.Errorf("thinking levels = %d, want 3", len(m0.Thinking.SupportedLevels))
	}
	if m0.Thinking.DefaultLevel != "high" {
		t.Errorf("default_level = %q, want high", m0.Thinking.DefaultLevel)
	}

	// Check pricing
	p, ok := result.Pricing["deepseek-v4-pro-ioa"]
	if !ok {
		t.Fatal("pricing missing for deepseek-v4-pro-ioa")
	}
	if p.Input != 0.5 || p.Output != 1.5 || p.CacheRead != 0.05 || p.CacheWrite != 0.5 {
		t.Errorf("pricing = %+v", p)
	}

	// Second model has no pricing.
	if _, ok := result.Pricing["hunyuan-t1"]; ok {
		t.Errorf("hunyuan-t1 should not have pricing")
	}

	m1 := result.Models[1]
	if m1.ID != "hunyuan-t1" || m1.Label != "Hunyuan T1" {
		t.Errorf("model[1] = %+v", m1)
	}
	if m1.Thinking != nil {
		t.Errorf("model[1] should have no thinking")
	}
}

func TestParseCLIModelsJSON_Empty(t *testing.T) {
	t.Parallel()
	result, err := ParseCLIModelsJSON([]byte(`{"models":[]}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Models) != 0 {
		t.Errorf("expected 0 models, got %d", len(result.Models))
	}
}

func TestParseCLIModelsJSON_InvalidJSON(t *testing.T) {
	t.Parallel()
	_, err := ParseCLIModelsJSON([]byte(`not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseCLIModelsJSON_LabelFallsBackToID(t *testing.T) {
	t.Parallel()
	result, err := ParseCLIModelsJSON([]byte(`{"models":[{"id":"no-label"}]}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Models[0].Label != "no-label" {
		t.Errorf("label should fall back to id, got %q", result.Models[0].Label)
	}
}

func TestParseCodeBuddyProductModelsJSON(t *testing.T) {
	t.Parallel()
	input := `{
		"models": [
			{"id": "deepseek-v4-pro-ioa", "name": "Deepseek-V4-Pro"},
			{"id": "hidden-model", "name": "Hidden", "hidden": true},
			{"id": "disabled-model", "name": "Disabled", "disabled": true},
			{"id": "nameless-model"},
			{"id": "deepseek-v4-pro-ioa", "name": "Duplicate"}
		]
	}`

	result, err := parseCodeBuddyProductModelsJSON([]byte(input))
	if err != nil {
		t.Fatalf("parseCodeBuddyProductModelsJSON: %v", err)
	}
	if len(result.Models) != 2 {
		t.Fatalf("expected 2 visible models, got %d", len(result.Models))
	}
	if result.Models[0].ID != "deepseek-v4-pro-ioa" || result.Models[0].Label != "Deepseek-V4-Pro" {
		t.Fatalf("first model = %+v", result.Models[0])
	}
	if !result.Models[0].Default {
		t.Fatal("first CodeBuddy product model should be marked as default when catalog has no explicit default")
	}
	if result.Models[0].Provider != "codebuddy" {
		t.Fatalf("provider = %q", result.Models[0].Provider)
	}
	if result.Models[1].ID != "nameless-model" || result.Models[1].Label != "nameless-model" {
		t.Fatalf("second model = %+v", result.Models[1])
	}
}

func TestFindCodeBuddyPackageRootFromAdjacentNodeModules(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	root := filepath.Join(dir, "node_modules", "@tencent-ai", "codebuddy-code")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"@tencent-ai/codebuddy-code"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	wrapper := filepath.Join(dir, "codebuddy")
	if err := os.WriteFile(wrapper, []byte("#!/usr/bin/env node\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := findCodeBuddyPackageRoot(wrapper)
	if err != nil {
		t.Fatalf("findCodeBuddyPackageRoot: %v", err)
	}
	if got != root {
		t.Fatalf("root = %q, want %q", got, root)
	}
}

func TestResolveCodeBuddyProductPathPrefersManifestEnvPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	productPath := filepath.Join(dir, "product.ioa.json")
	if err := os.WriteFile(productPath, []byte(`{"models":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := resolveCodeBuddyProductPath(AgentEntry{
		Path:       "missing-codebuddy",
		ManifestID: "codebuddy",
		Provider:   "codebuddy",
		Env:        map[string]string{"ACC_PRODUCT_CONFIG_PATH": productPath},
	})
	if err != nil {
		t.Fatalf("resolveCodeBuddyProductPath: %v", err)
	}
	if got != productPath {
		t.Fatalf("product path = %q, want %q", got, productPath)
	}
}

func TestFindCodeBuddyProductForModel(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "product.json"), []byte(`{
		"models": [{"id": "default-model", "name": "Default"}]
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "product.ioa.json"), []byte(`{
		"models": [{"id": "deepseek-v4-pro-ioa", "name": "Deepseek-V4-Pro"}]
	}`), 0o644); err != nil {
		t.Fatal(err)
	}

	got := findCodeBuddyProductForModel(root, "deepseek-v4-pro-ioa")
	want := filepath.Join(root, "product.ioa.json")
	if got != want {
		t.Fatalf("product path = %q, want %q", got, want)
	}
	if got := findCodeBuddyProductForModel(root, "missing"); got != "" {
		t.Fatalf("missing model should not resolve a product file, got %q", got)
	}
}

func TestParseACPSessionNewModelsExternal(t *testing.T) {
	t.Parallel()
	resp := map[string]any{
		"result": map[string]any{
			"models": map[string]any{
				"availableModels": []any{
					map[string]any{
						"modelId": "claude-sonnet-4-6",
						"name":    "Claude Sonnet 4.6",
						"default": true,
						"pricing": map[string]any{
							"input":  3.0,
							"output": 15.0,
						},
					},
					map[string]any{
						"modelId": "claude-opus-4-8",
						"name":    "Claude Opus 4.8",
					},
				},
			},
		},
	}

	result, err := parseACPSessionNewModelsExternal(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(result.Models))
	}
	if result.Models[0].ID != "claude-sonnet-4-6" {
		t.Errorf("model[0].ID = %q", result.Models[0].ID)
	}
	if !result.Models[0].Default {
		t.Errorf("model[0] should be default")
	}
	p, ok := result.Pricing["claude-sonnet-4-6"]
	if !ok {
		t.Fatal("pricing missing for claude-sonnet-4-6")
	}
	if p.Input != 3.0 || p.Output != 15.0 {
		t.Errorf("pricing = %+v", p)
	}
	if _, ok := result.Pricing["claude-opus-4-8"]; ok {
		t.Errorf("opus should not have pricing (none declared)")
	}
}

func TestParseACPSessionNewModelsExternal_EmptyResponse(t *testing.T) {
	t.Parallel()
	result, err := parseACPSessionNewModelsExternal(map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Models) != 0 {
		t.Errorf("expected 0 models")
	}
}

func TestResolveModelsDiscovery_InfersMethod(t *testing.T) {
	t.Parallel()
	t.Run("stream-json infers cli", func(t *testing.T) {
		cfg := &ModelsDiscoveryConfig{CLI: &CLIDiscoveryConfig{Args: []string{"--list-models"}}}
		out := resolveModelsDiscovery(cfg, "stream-json")
		if out.Method != "cli" {
			t.Errorf("method = %q, want cli", out.Method)
		}
		if out.CLI.TimeoutSeconds != 15 {
			t.Errorf("timeout = %d, want 15", out.CLI.TimeoutSeconds)
		}
		if out.CacheTTLSeconds != 60 {
			t.Errorf("cacheTTL = %d, want 60", out.CacheTTLSeconds)
		}
	})
	t.Run("acp-stdio infers acp", func(t *testing.T) {
		cfg := &ModelsDiscoveryConfig{}
		out := resolveModelsDiscovery(cfg, "acp-stdio")
		if out.Method != "acp" {
			t.Errorf("method = %q, want acp", out.Method)
		}
	})
	t.Run("nil returns nil", func(t *testing.T) {
		if resolveModelsDiscovery(nil, "stream-json") != nil {
			t.Errorf("nil config should return nil")
		}
	})
}

func TestModelDiscoveryCache(t *testing.T) {
	t.Parallel()
	cache := &modelCache{store: make(map[string]*modelCacheEntry)}

	// Miss on empty.
	if cache.get("foo") != nil {
		t.Errorf("expected nil on miss")
	}

	// Set and get.
	result := &DiscoveredModels{Pricing: map[string]RuntimePricing{"a": {Input: 1}}}
	cache.set("foo", result, 1*time.Minute)
	got := cache.get("foo")
	if got == nil || got.Pricing["a"].Input != 1 {
		t.Errorf("expected cached result, got %v", got)
	}

	// Expired.
	cache.store["foo"].expires = time.Now().Add(-1 * time.Second)
	if cache.get("foo") != nil {
		t.Errorf("expected nil for expired entry")
	}
}

// Helpers.
func init() {
	// Ensure CLIModelsOutput round-trips cleanly.
	_ = json.Marshal
}
