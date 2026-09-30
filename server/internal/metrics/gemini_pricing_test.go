package metrics

import "testing"

func TestPriceForModelAliasGeminiFlash(t *testing.T) {
	cases := []struct {
		id    string
		price ModelPrice
	}{
		{"gemini-3.8-flash", ModelPrice{Provider: "google", Model: "gemini-3.8-flash", InputPerM: 0.75, CacheReadPerM: 0.075, CacheWritePerM: 0.75, OutputPerM: 3.75}},
		{"gemini-3.7-flash", ModelPrice{Provider: "google", Model: "gemini-3.7-flash", InputPerM: 0.75, CacheReadPerM: 0.075, CacheWritePerM: 0.75, OutputPerM: 3.75}},
		{"gemini-3.6-flash", ModelPrice{Provider: "google", Model: "gemini-3.6-flash", InputPerM: 0.75, CacheReadPerM: 0.075, CacheWritePerM: 0.75, OutputPerM: 3.75}},
		{"gemini-3.5-flash", ModelPrice{Provider: "google", Model: "gemini-3.5-flash", InputPerM: 1.5, CacheReadPerM: 0.15, CacheWritePerM: 1.5, OutputPerM: 9}},
	}
	for _, tc := range cases {
		for _, alias := range []string{tc.id, "google/" + tc.id, "antigravity:" + tc.id + "-high", "custom:google/" + tc.id + "-low[1m]"} {
			t.Run(alias, func(t *testing.T) {
				if got, ok := PriceForModelAlias(alias); !ok || got != tc.price {
					t.Fatalf("PriceForModelAlias(%q) = %+v, %v; want %+v", alias, got, ok, tc.price)
				}
			})
		}
	}
	for _, alias := range []string{"Gemini 3.8 Flash", "Gemini 3.8 Flash (High)", "Gemini 3.8 Flash (Low)", "google/Gemini 3.8 Flash (Medium)", "gemini-3.8-flash-minimal"} {
		if got, ok := PriceForModelAlias(alias); !ok || got != cases[0].price {
			t.Errorf("display/effort alias %q did not resolve: %+v, %v", alias, got, ok)
		}
	}
	for _, alias := range []string{"gemini-3.8-flash-lite", "gemini-3.8-flash-live", "gemini-3.8-flash-preview", "gemini-3.8-flash-ultra", "gemini-3-8-flash", "Gemini 3.8 Flash (Ultra)", "Gemini 3.9 Flash (High)", "gemini-3.8-flash[1m][2m]"} {
		if got, ok := PriceForModelAlias(alias); ok {
			t.Errorf("unknown alias %q borrowed a rate: %+v", alias, got)
		}
	}
}
