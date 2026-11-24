package metrics

import "testing"

func TestPriceForRuntimeModelCursorVariants(t *testing.T) {
	for _, tc := range []struct {
		model string
		want  string
	}{
		// Effort and thinking parts, in either order, don't change the rate.
		{"claude-opus-5-5-xhigh", "claude-opus-5-5"},
		{"claude-opus-5-thinking-max", "claude-opus-5"},
		{"claude-4.6-sonnet-medium-thinking", "claude-4.6-sonnet"},
		{"claude-fable-5-1-thinking-high", "claude-fable-5-1"},
		{"claude-fable-5-2-xhigh", "claude-fable-5-2"},
		{"gpt-5.5-extra-high", "gpt-5.5"},
		{"gpt-5.6-sol-none", "gpt-5.6-sol"},
		{"gpt-5.3-codex-xhigh", "gpt-5.3-codex"},
		{"gpt-5.1-high", "gpt-5.1"},
		{"cursor-grok-4.6-high", "grok-4.6"},
		{"muse-wonder-1.0-minimal", "muse-wonder-1.0"},
		{"deepseek-v41-flash-1017-max", "deepseek-v41-flash-1017"},
		{"claude-opus-4-8[context=1m,effort=high,fast=false]", "claude-opus-4-8"},
		{"Claude-Opus-5-5-High", "claude-opus-5-5"},
		{"auto", "auto"},
		{"composer-2.5", "composer-2.5"},
		{"cursor", "cursor"},
		// Fast resolves to its own row, from a suffix or a parameter.
		{"composer-2.5-fast", "composer-2.5-fast"},
		{"cursor-grok-4.5-high-fast", "grok-4.5-fast"},
		{"grok-4.7-xhigh-fast", "grok-4.7-fast"},
		{"claude-opus-4-7-thinking-high-fast", "claude-opus-4-7-fast"},
		{"claude-opus-4-8[effort=high,fast=true]", "claude-opus-4-8-fast"},
		{"claude-opus-5-5-xhigh-fast", "claude-opus-5-5-fast"},
		{"gpt-5.4-xhigh-fast", "gpt-5.4-fast"},
		{"gpt-5.5-medium-fast", "gpt-5.5-fast"},
	} {
		got, ok := PriceForRuntimeModel("cursor", tc.model)
		if !ok {
			t.Errorf("PriceForRuntimeModel(cursor, %q) did not resolve", tc.model)
			continue
		}
		if want := cursorModelPrices[tc.want]; got != want {
			t.Errorf("PriceForRuntimeModel(cursor, %q) = %+v, want %+v", tc.model, got, want)
		}
	}
}

func TestPriceForRuntimeModelCursorRates(t *testing.T) {
	for _, tc := range []struct {
		model                                string
		input, output, cacheRead, cacheWrite float64
	}{
		{"claude-opus-4-7-high-fast", 30, 150, 3, 37.5},
		{"claude-opus-5-5-high-fast", 8, 40, 0.4, 10},
		{"claude-opus-5-high-fast", 10, 50, 1, 12.5},
		{"claude-fable-5-2-xhigh", 8, 40, 0.2, 10},
		{"gpt-5.5-high-fast", 12.5, 75, 1.25, 0},
		{"gemini-3.6-flash-high", 1.5, 7.5, 0.15, 0},
		{"gemini-3.5-flash", 1.5, 9, 0.15, 0},
		{"gemini-4-hydrogen-high", 1.5, 7.5, 0.075, 0},
		{"gemini-4-hydrogen-xhigh", 1.5, 7.5, 0.075, 0},
		{"gemini-4-carbon-medium", 4, 20, 0.2, 0},
	} {
		got, ok := PriceForRuntimeModel("cursor", tc.model)
		if !ok {
			t.Errorf("PriceForRuntimeModel(cursor, %q) did not resolve", tc.model)
			continue
		}
		if got.InputPerM != tc.input || got.OutputPerM != tc.output || got.CacheReadPerM != tc.cacheRead || got.CacheWritePerM != tc.cacheWrite {
			t.Errorf("PriceForRuntimeModel(cursor, %q) = %+v, want %v/%v/%v/%v", tc.model, got, tc.input, tc.output, tc.cacheRead, tc.cacheWrite)
		}
	}
}

func TestPriceForRuntimeModelCursorFastNeverBorrowsStandardRate(t *testing.T) {
	for _, model := range []string{
		"claude-sonnet-5-5-high-fast",
		"claude-sonnet-5[fast=true]",
		"kimi-k3-fast",
		"gemini-4-argon-fast",
		"gemini-4-hydrogen-high-fast",
	} {
		if got, ok := PriceForRuntimeModel("cursor", model); ok {
			t.Errorf("PriceForRuntimeModel(cursor, %q) = %+v; want unmapped", model, got)
		}
	}
}

func TestPriceForRuntimeModelKeepsCursorParsingScopedToCursor(t *testing.T) {
	if got, ok := PriceForRuntimeModel("claude", "claude-opus-5-5-high"); ok {
		t.Errorf("non-Cursor variant id resolved to %+v; want unmapped", got)
	}
	if got, ok := PriceForRuntimeModel("claude", "auto"); ok {
		t.Errorf("non-Cursor `auto` resolved to %+v; want unmapped", got)
	}
	got, ok := PriceForRuntimeModel("codex", "gpt-5.4")
	if want := modelPrices["openai:gpt-5.4"]; !ok || got != want {
		t.Errorf("PriceForRuntimeModel(codex, gpt-5.4) = %+v, %v; want %+v", got, ok, want)
	}
}

// TestPriceForRuntimeModelAntigravityClaudeEffort pins the Antigravity-scoped
// Claude effort fallback: agy reports Claude models with a reasoning-effort
// or Thinking suffix, or a display label (see readAntigravitySelectedModel in
// server/pkg/agent/antigravity.go); both select reasoning, not a SKU, so the
// id prices at the base Claude tier. Only the levels Antigravity offers for
// Claude are stripped, the version is a major or major.minor number, and
// only the antigravity provider gets the rewrite — the non-Antigravity rows
// stay pinned unmapped by
// TestPriceForRuntimeModelKeepsCursorParsingScopedToCursor. Mirrors the
// antigravity branch of pricingCandidates in
// packages/views/runtimes/utils.ts.
func TestPriceForRuntimeModelAntigravityClaudeEffort(t *testing.T) {
	for _, tc := range []struct {
		model string
		key   string
	}{
		{"claude-opus-5-5-high", "anthropic:claude-opus-5-5"},
		{"claude-opus-5-5-medium", "anthropic:claude-opus-5-5"},
		{"claude-sonnet-5-5-low", "anthropic:claude-sonnet-5-5"},
		{"claude-haiku-5-5-minimal", "anthropic:claude-haiku-5-5"},
		{"claude-opus-5-5-high[1m]", "anthropic:claude-opus-5-5"},
		{"claude-opus-5.5-high", "anthropic:claude-opus-5-5"},
		{"Claude-Opus-5.5-High", "anthropic:claude-opus-5-5"},
		{"antigravity:claude-opus-5-5-high", "anthropic:claude-opus-5-5"},
		{"Claude Opus 5.5 (High)", "anthropic:claude-opus-5-5"},
		{"Claude Opus 5.5", "anthropic:claude-opus-5-5"},
		{"claude-opus-5-5-thinking", "anthropic:claude-opus-5-5"},
		{"Claude Opus 5.5 (Thinking)", "anthropic:claude-opus-5-5"},
		{"Claude Sonnet 5.5 (Thinking)", "anthropic:claude-sonnet-5-5"},
		{"claude-sonnet-5-5-thinking", "anthropic:claude-sonnet-5-5"},
		{"Claude Opus 4.6 (Thinking)", "anthropic:claude-opus-4.6"},
		{"claude-opus-4-6-thinking", "anthropic:claude-opus-4.6"},
		{"Claude Sonnet 4.6 (Thinking)", "anthropic:claude-sonnet-4.6"},
		{"gemini-4-carbon-high", "google:gemini-4-carbon"},
		{"gemini-4-neon-xhigh", "google:gemini-4-neon"},
		{"Gemini 4 Hydrogen (Extra)", "google:gemini-4-hydrogen"},
	} {
		want := modelPrices[tc.key]
		got, ok := PriceForRuntimeModel("antigravity", tc.model)
		if !ok || got != want {
			t.Errorf("PriceForRuntimeModel(antigravity, %q) = %+v, %v; want %+v", tc.model, got, ok, want)
		}
	}
	for _, model := range []string{
		"claude-opus-5-5-fast",
		"claude-opus-5-5-max",
		"claude-opus-5-5-xhigh",
		"claude-opus-5-5-thinking-high",
		"Claude Opus 5.5 (Extra)",
		"claude-opus-5-5-preview",
		"claude-opus-5-5-99-high",
		"claude-opus-5-5-20261001-high",
		"claude-opus-5-5-high-latest",
		"/claude-opus-5-5-high",
		"claude-opus-5-5[]",
		"claude-opus-5-5-high[1m][2m]",
		"Claude Opus 5.5 (Turbo)",
	} {
		if got, ok := PriceForRuntimeModel("antigravity", model); ok {
			t.Errorf("PriceForRuntimeModel(antigravity, %q) = %+v; want unmapped", model, got)
		}
	}
	// The Claude effort fallback must not leak beyond the antigravity
	// provider; provider="claude" is pinned by
	// TestPriceForRuntimeModelKeepsCursorParsingScopedToCursor.
	for _, model := range []string{"claude-opus-5-5-high", "Claude Opus 5.5 (Thinking)"} {
		if got, ok := PriceForRuntimeModel("", model); ok {
			t.Errorf("PriceForRuntimeModel(\"\", %q) = %+v; want unmapped", model, got)
		}
	}
}

// TestPriceForRuntimeModelAntigravityGPTOSS pins GPT-OSS 120B on Antigravity
// to Vertex AI's rate, and keeps that rate from leaking to other runtimes,
// which reach gpt-oss through hosts with different prices. Orchestrator
// splits a turn across two models, so it stays unmapped.
func TestPriceForRuntimeModelAntigravityGPTOSS(t *testing.T) {
	want := ModelPrice{Provider: "google", Model: "gpt-oss-120b", InputPerM: 0.09, CacheReadPerM: 0.09, CacheWritePerM: 0.09, OutputPerM: 0.36}
	for _, model := range []string{
		"GPT-OSS 120B (Medium)",
		"GPT-OSS 120B",
		"gpt-oss-120b",
		"gpt-oss-120b-medium",
		"gpt-oss-120b-high[1m]",
		"openai/gpt-oss-120b",
	} {
		if got, ok := PriceForRuntimeModel("antigravity", model); !ok || got != want {
			t.Errorf("PriceForRuntimeModel(antigravity, %q) = %+v, %v; want %+v", model, got, ok, want)
		}
	}
	for _, model := range []string{
		"gpt-oss-20b",
		"GPT-OSS 20B (Medium)",
		"gpt-oss-120b-xhigh",
		"gpt-oss-120b-preview",
		"GPT-OSS 120B (Turbo)",
		"gpt-oss-120b[1m][2m]",
		"Orchestrator (Neon)",
		"Orchestrator (Carbon)",
		"gemini-pro-orchestrator",
	} {
		if got, ok := PriceForRuntimeModel("antigravity", model); ok {
			t.Errorf("PriceForRuntimeModel(antigravity, %q) = %+v; want unmapped", model, got)
		}
	}
	for _, provider := range []string{"", "opencode", "cursor"} {
		if got, ok := PriceForRuntimeModel(provider, "gpt-oss-120b"); ok {
			t.Errorf("PriceForRuntimeModel(%q, gpt-oss-120b) = %+v; want unmapped", provider, got)
		}
	}
}
