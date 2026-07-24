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

// TestPriceForModelAliasGrok pins the xAI catalog to the published rates
// (docs.x.ai/developers/pricing). Before these rows existed every Grok token
// took the unpriced branch in RecordLLMUsage, so llm_cost_usd reported zero
// Grok spend while the tokens piled up in llm_unpriced_tokens.
func TestPriceForModelAliasGrok(t *testing.T) {
	cases := []struct {
		model string
		want  ModelPrice
	}{
		{
			model: "grok-4.5",
			want:  ModelPrice{Provider: "xai", Model: "grok-4.5", InputPerM: 2, CacheReadPerM: 0.3, CacheWritePerM: 2, OutputPerM: 6},
		},
		{
			model: "xai:grok-4.5",
			want:  ModelPrice{Provider: "xai", Model: "grok-4.5", InputPerM: 2, CacheReadPerM: 0.3, CacheWritePerM: 2, OutputPerM: 6},
		},
		{
			model: "xai/grok-4.5",
			want:  ModelPrice{Provider: "xai", Model: "grok-4.5", InputPerM: 2, CacheReadPerM: 0.3, CacheWritePerM: 2, OutputPerM: 6},
		},
		{
			model: "grok-4.3",
			want:  ModelPrice{Provider: "xai", Model: "grok-4.3", InputPerM: 1.25, CacheReadPerM: 0.2, CacheWritePerM: 1.25, OutputPerM: 2.5},
		},
		{
			model: "grok-build-0.1",
			want:  ModelPrice{Provider: "xai", Model: "grok-build-0.1", InputPerM: 1, CacheReadPerM: 0.2, CacheWritePerM: 1, OutputPerM: 2},
		},
		{
			model: "grok-4.20-multi-agent-0309",
			want:  ModelPrice{Provider: "xai", Model: "grok-4.20-multi-agent-0309", InputPerM: 1.25, CacheReadPerM: 0.2, CacheWritePerM: 1.25, OutputPerM: 2.5},
		},
		{
			model: "grok-4.20-0309-reasoning",
			want:  ModelPrice{Provider: "xai", Model: "grok-4.20-0309-reasoning", InputPerM: 1.25, CacheReadPerM: 0.2, CacheWritePerM: 1.25, OutputPerM: 2.5},
		},
		{
			model: "grok-4.20-0309-non-reasoning",
			want:  ModelPrice{Provider: "xai", Model: "grok-4.20-0309-non-reasoning", InputPerM: 1.25, CacheReadPerM: 0.2, CacheWritePerM: 1.25, OutputPerM: 2.5},
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

	// `grok-composer-*` ships in the Grok Build catalog but xAI publishes no
	// rate for it, so it must stay unmapped rather than inherit grok-4.5's.
	// Suffixed and dash-spelled variants must miss for the same reason the
	// gpt-5.6 rows do: the frontend resolver is an exact match that does not
	// dash-normalize non-Anthropic ids, so both sides agree on "unmapped".
	for _, model := range []string{
		"grok-composer-2.5-fast",
		"grok-composer-2.5",
		"grok-4.5-fast",
		"grok-4-5",
		"grok-4.20-0309",
		"grok",
		"unknown",
	} {
		if got, ok := PriceForModelAlias(model); ok {
			t.Fatalf("PriceForModelAlias(%q) unexpectedly resolved to %+v; want unmapped", model, got)
		}
	}
}

// TestGrokPricingMatchesRecordedTurn re-derives the cost of a real
// grok 0.2.106 turn from the table and checks it against the costUsdTicks xAI
// returned for that same turn (1 tick = 1e-10 USD). This is the end-to-end
// proof that both the rates and the cached-input bucketing are right.
func TestGrokPricingMatchesRecordedTurn(t *testing.T) {
	// Captured payload: inputTokens 12929, cachedReadTokens 10880,
	// outputTokens 29, totalTokens 12958, costUsdTicks 75360000. Grok counts
	// the cached prefix inside inputTokens, so the uncached remainder is
	// 12929 - 10880 = 2049 (see excludeACPCachedInput in pkg/agent/hermes.go).
	const (
		uncachedInput = int64(2049)
		cacheRead     = int64(10880)
		output        = int64(29)
		wantUSD       = 75360000 / 1e10
	)

	price, ok := PriceForModelAlias("grok-4.5")
	if !ok {
		t.Fatal("grok-4.5 did not resolve")
	}
	got := tokenCostUSD(uncachedInput, price.InputPerM) +
		tokenCostUSD(cacheRead, price.CacheReadPerM) +
		tokenCostUSD(output, price.OutputPerM)

	if diff := got - wantUSD; diff > 1e-12 || diff < -1e-12 {
		t.Fatalf("recomputed cost = %.10f, want %.10f (xAI costUsdTicks)", got, wantUSD)
	}
}
