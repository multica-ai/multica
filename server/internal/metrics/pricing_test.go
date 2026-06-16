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

func TestPriceForModelAliasCodexAutoReview(t *testing.T) {
	got, ok := PriceForModelAlias("codex-auto-review")
	if !ok {
		t.Fatal("PriceForModelAlias(codex-auto-review) did not resolve")
	}
	want := ModelPrice{
		Provider:       "openai",
		Model:          "gpt-5.4-mini",
		InputPerM:      0.75,
		CacheReadPerM:  0.075,
		CacheWritePerM: 0.075,
		OutputPerM:     4.5,
	}
	if got != want {
		t.Fatalf("PriceForModelAlias(codex-auto-review) = %+v, want %+v", got, want)
	}
}
