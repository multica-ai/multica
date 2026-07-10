package metrics

import "testing"

func TestPriceForModelAliasAnthropicFableAndOpus48(t *testing.T) {
	cases := []struct {
		model string
		want  ModelPrice
	}{
		{
			model: "claude-sonnet-5",
			want:  ModelPrice{Provider: "anthropic", Model: "claude-sonnet-5", InputPerM: 2, CacheReadPerM: 0.2, CacheWritePerM: 2.5, OutputPerM: 10},
		},
		{
			model: "anthropic:claude-sonnet-5",
			want:  ModelPrice{Provider: "anthropic", Model: "claude-sonnet-5", InputPerM: 2, CacheReadPerM: 0.2, CacheWritePerM: 2.5, OutputPerM: 10},
		},
		{
			model: "claude-5-sonnet",
			want:  ModelPrice{Provider: "anthropic", Model: "claude-sonnet-5", InputPerM: 2, CacheReadPerM: 0.2, CacheWritePerM: 2.5, OutputPerM: 10},
		},
		{
			model: "claude-fable-5",
			want:  ModelPrice{Provider: "anthropic", Model: "claude-fable-5", InputPerM: 10, CacheReadPerM: 1, CacheWritePerM: 12.5, OutputPerM: 50},
		},
		{
			model: "anthropic/claude-fable-5",
			want:  ModelPrice{Provider: "anthropic", Model: "claude-fable-5", InputPerM: 10, CacheReadPerM: 1, CacheWritePerM: 12.5, OutputPerM: 50},
		},
		{
			model: "claude-opus-4-8",
			want:  ModelPrice{Provider: "anthropic", Model: "claude-opus-4.8", InputPerM: 5, CacheReadPerM: 0.5, CacheWritePerM: 6.25, OutputPerM: 25},
		},
	}

	for _, tc := range cases {
		got, ok := PriceForModelAlias(tc.model)
		if !ok {
			t.Fatalf("PriceForModelAlias(%q) did not resolve", tc.model)
		}
		if got != tc.want {
			t.Fatalf("PriceForModelAlias(%q) = %+v, want %+v", tc.model, got, tc.want)
		}
	}
}

func TestPriceForModelAliasCodexGPT56(t *testing.T) {
	// Official rates from OpenAI's GPT-5.6 announcement: cache read = 0.1x
	// input (90% cached-input discount), cache write = 1.25x input.
	cases := []struct {
		model string
		want  ModelPrice
	}{
		{
			model: "gpt-5.6-sol",
			want:  ModelPrice{Provider: "openai", Model: "gpt-5.6-sol", InputPerM: 5, CacheReadPerM: 0.5, CacheWritePerM: 6.25, OutputPerM: 30},
		},
		{
			model: "openai:gpt-5.6-terra",
			want:  ModelPrice{Provider: "openai", Model: "gpt-5.6-terra", InputPerM: 2.5, CacheReadPerM: 0.25, CacheWritePerM: 3.125, OutputPerM: 15},
		},
		{
			model: "openai/gpt-5.6-luna",
			want:  ModelPrice{Provider: "openai", Model: "gpt-5.6-luna", InputPerM: 1, CacheReadPerM: 0.1, CacheWritePerM: 1.25, OutputPerM: 6},
		},
	}

	for _, tc := range cases {
		got, ok := PriceForModelAlias(tc.model)
		if !ok {
			t.Fatalf("PriceForModelAlias(%q) did not resolve", tc.model)
		}
		if got != tc.want {
			t.Fatalf("PriceForModelAlias(%q) = %+v, want %+v", tc.model, got, tc.want)
		}
	}

	// Unknown suffixed variants must NOT borrow a 5.6 tier — the alias is an
	// anchored exact match, mirroring the frontend's exact-match resolver.
	// The dash-normalized ids (`gpt-5-6-luna`) must also miss: the real Codex
	// slug is always dotted and the frontend does not dash-normalize, so both
	// sides surface these as unmapped instead of silently pricing them.
	for _, model := range []string{
		"gpt-5.6-luna-pro",
		"gpt-5.6-luna/unknown",
		"gpt-5.6-sol-high",
		"gpt-5.6-mini",
		"gpt-5-6-luna",
		"gpt-5-6-sol",
		"gpt-5-6-terra",
	} {
		if got, ok := PriceForModelAlias(model); ok {
			t.Fatalf("PriceForModelAlias(%q) unexpectedly resolved to %+v; want unmapped", model, got)
		}
	}
}
