package metrics

import "testing"

func TestPriceForModelAliasMaintainedRates(t *testing.T) {
	cases := []struct {
		model string
		want  ModelPrice
	}{
		{
			model: "gpt-5-codex",
			want:  ModelPrice{Provider: "openai", Model: "gpt-5-codex", InputPerM: 1.25, CacheReadPerM: 0.125, CacheWritePerM: 1.25, OutputPerM: 10},
		},
		{
			model: "openai/gpt-5.2-codex",
			want:  ModelPrice{Provider: "openai", Model: "gpt-5.2-codex", InputPerM: 1.75, CacheReadPerM: 0.175, CacheWritePerM: 1.75, OutputPerM: 14},
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
		{
			model: "deepseek-v4-pro",
			want:  ModelPrice{Provider: "deepseek", Model: "v4-pro", InputPerM: 0.435, CacheReadPerM: 0.003625, CacheWritePerM: 0.435, OutputPerM: 0.87},
		},
		{
			model: "deepseek-chat",
			want:  ModelPrice{Provider: "deepseek", Model: "v4-flash", InputPerM: 0.14, CacheReadPerM: 0.0028, CacheWritePerM: 0.14, OutputPerM: 0.28},
		},
		{
			model: "moonshotai/kimi-k2.7-code-highspeed",
			want:  ModelPrice{Provider: "kimi", Model: "k2.7-code-highspeed", InputPerM: 1.9, CacheReadPerM: 0.38, CacheWritePerM: 1.9, OutputPerM: 8},
		},
		{
			model: "minimax-m2.7-highspeed",
			want:  ModelPrice{Provider: "minimax", Model: "m2.7-highspeed", InputPerM: 0.6, CacheReadPerM: 0.03, CacheWritePerM: 0.375, OutputPerM: 2.4},
		},
		{
			model: "gemini-2.5-flash-lite",
			want:  ModelPrice{Provider: "google", Model: "gemini-2.5-flash-lite", InputPerM: 0.1, CacheReadPerM: 0.01, CacheWritePerM: 0.1, OutputPerM: 0.4},
		},
		{
			model: "zhipuai/glm-4.5-airx",
			want:  ModelPrice{Provider: "zhipu", Model: "glm-4.5-airx", InputPerM: 1.1, CacheReadPerM: 0.22, CacheWritePerM: 1.1, OutputPerM: 4.5},
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
