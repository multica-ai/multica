package daemon

import (
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/multica-ai/multica/server/internal/piagent"
)

func TestDecodePiRuntimeConfig(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg, ok := decodePiRuntimeConfig(json.RawMessage(`{
		"provider":"openai",
		"api":"openai-responses",
		"base_url":"https://api.example.com/v1",
		"model":"gpt-5"
	}`), logger)
	if !ok {
		t.Fatal("expected valid configuration")
	}
	if cfg.Provider != "openai" || cfg.Model != "gpt-5" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestDecodePiRuntimeConfigRejectsPartialOrUnsafeEndpoints(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, raw := range []string{
		`{"provider":"openai"}`,
		`{"provider":"openai","api":"unknown","base_url":"https://api.example.com","model":"x"}`,
		`{"provider":"openai","api":"openai-responses","base_url":"http://api.example.com","model":"x"}`,
		`{"provider":"openai","api":"openai-responses","base_url":"https://user:pass@example.com","model":"x"}`,
	} {
		if cfg, ok := decodePiRuntimeConfig(json.RawMessage(raw), logger); ok {
			t.Fatalf("expected invalid config for %s, got %+v", raw, cfg)
		}
	}
}

func TestDecodePiRuntimeConfigAllowsLoopbackHTTP(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	_, ok := decodePiRuntimeConfig(json.RawMessage(`{
		"provider":"ollama",
		"api":"openai-completions",
		"base_url":"http://127.0.0.1:11434/v1",
		"model":"qwen"
	}`), logger)
	if !ok {
		t.Fatal("expected loopback HTTP configuration to be valid")
	}
}

func TestApplyPiAgentModelOverrideUsesPlainModel(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := applyPiAgentModelOverride(piagent.Config{
		Provider: "deepseek",
		API:      "openai-completions",
		BaseURL:  "https://api.deepseek.com",
		Model:    "deepseek-v4-pro",
	}, "deepseek-v4-flash", logger)

	if cfg.Model != "deepseek-v4-flash" {
		t.Fatalf("model = %q, want deepseek-v4-flash", cfg.Model)
	}
}

func TestApplyPiAgentModelOverrideStripsSameProviderPrefix(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := applyPiAgentModelOverride(piagent.Config{
		Provider: "deepseek",
		API:      "openai-completions",
		BaseURL:  "https://api.deepseek.com",
		Model:    "deepseek-v4-pro",
	}, "deepseek/deepseek-v4-flash", logger)

	if cfg.Model != "deepseek-v4-flash" {
		t.Fatalf("model = %q, want deepseek-v4-flash", cfg.Model)
	}
}

func TestApplyPiAgentModelOverrideRejectsDifferentProviderPrefix(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := applyPiAgentModelOverride(piagent.Config{
		Provider: "deepseek",
		API:      "openai-completions",
		BaseURL:  "https://api.deepseek.com",
		Model:    "deepseek-v4-pro",
	}, "openai/gpt-5", logger)

	if cfg.Model != "deepseek-v4-pro" {
		t.Fatalf("model = %q, want deepseek-v4-pro", cfg.Model)
	}
}
