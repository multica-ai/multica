package piagent

import (
	"encoding/json"
	"testing"
)

func TestDecodeConfig(t *testing.T) {
	cfg, configured, err := Decode(json.RawMessage(`{
		"provider":" deepseek ",
		"api":"openai-completions",
		"base_url":"https://api.deepseek.com",
		"model":"deepseek-v4-flash"
	}`))
	if err != nil || !configured {
		t.Fatalf("Decode() = (%+v, %v, %v), want configured", cfg, configured, err)
	}
	if cfg.Provider != "deepseek" || cfg.Model != "deepseek-v4-flash" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestDecodeEmptyConfig(t *testing.T) {
	for _, raw := range []string{"", "null", "{}"} {
		if _, configured, err := Decode(json.RawMessage(raw)); err != nil || configured {
			t.Fatalf("Decode(%q) = configured %v, err %v", raw, configured, err)
		}
	}
}

func TestValidateRejectsPartialOrUnsafeEndpoints(t *testing.T) {
	for _, cfg := range []Config{
		{Provider: "deepseek"},
		{Provider: "deepseek", API: "unknown", BaseURL: "https://api.example.com", Model: "x"},
		{Provider: "deepseek", API: "openai-completions", BaseURL: "http://api.example.com", Model: "x"},
		{Provider: "deepseek", API: "openai-completions", BaseURL: "https://user:pass@example.com", Model: "x"},
	} {
		if err := Validate(cfg); err == nil {
			t.Fatalf("Validate(%+v) succeeded, want error", cfg)
		}
	}
}

func TestValidateAllowsLoopbackHTTP(t *testing.T) {
	cfg := Config{
		Provider: "ollama",
		API:      "openai-completions",
		BaseURL:  "http://127.0.0.1:11434/v1",
		Model:    "qwen",
	}
	if err := Validate(cfg); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestSameEndpointAllowsModelOverrides(t *testing.T) {
	base := Config{
		Provider: "deepseek",
		API:      "openai-completions",
		BaseURL:  "https://api.deepseek.com",
		Model:    "deepseek-v4-flash",
	}
	override := base
	override.BaseURL += "/"
	override.Model = "deepseek-v4-pro"
	if !SameEndpoint(base, override) {
		t.Fatal("model-only override should share the runtime credential")
	}
	override.Provider = "openai"
	if SameEndpoint(base, override) {
		t.Fatal("different providers must not share credentials")
	}
}
