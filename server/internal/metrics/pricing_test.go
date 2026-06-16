package metrics

import "testing"

func TestPriceForModelAliasAnthropicFableAndOpus48(t *testing.T) {
	cases := []struct {
		model string
		want  ModelPrice
	}{
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
		{
			model: "openai/gpt-5.5",
			want:  ModelPrice{Provider: "openai", Model: "gpt-5.5", InputPerM: 5, CacheReadPerM: 0.5, CacheWritePerM: 5, OutputPerM: 30},
		},
		{
			model: "deepseek-v4-flash",
			want:  ModelPrice{Provider: "deepseek", Model: "v4-flash", InputPerM: 0.14, CacheReadPerM: 0.0028, CacheWritePerM: 0.14, OutputPerM: 0.28},
		},
		{
			model: "deepseek-v4-pro",
			want:  ModelPrice{Provider: "deepseek", Model: "v4-pro", InputPerM: 0.435, CacheReadPerM: 0.003625, CacheWritePerM: 0.435, OutputPerM: 0.87},
		},
		{
			model: "nous:moonshotai/kimi-k2.7-code-highspeed",
			want:  ModelPrice{Provider: "kimi", Model: "k2.7-code-highspeed", InputPerM: 1.9, CacheReadPerM: 0.38, CacheWritePerM: 1.9, OutputPerM: 8},
		},
		{
			model: "moonshotai/kimi-k2.5",
			want:  ModelPrice{Provider: "kimi", Model: "k2.5", InputPerM: 0.6, CacheReadPerM: 0.1, CacheWritePerM: 0.6, OutputPerM: 3},
		},
		{
			model: "gemini-3.1-pro-preview-customtools",
			want:  ModelPrice{Provider: "google", Model: "gemini-3.1-pro-preview-customtools", InputPerM: 2, CacheReadPerM: 0.2, CacheWritePerM: 2, OutputPerM: 12},
		},
		{
			model: "google/gemini-2.5-flash-lite",
			want:  ModelPrice{Provider: "google", Model: "gemini-2.5-flash-lite", InputPerM: 0.1, CacheReadPerM: 0.01, CacheWritePerM: 0.1, OutputPerM: 0.4},
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
