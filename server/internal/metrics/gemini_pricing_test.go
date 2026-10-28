package metrics

import "testing"

func TestPriceForModelAliasGemini(t *testing.T) {
	cases := []struct {
		id    string
		price ModelPrice
	}{
		{"gemini-4-argon", ModelPrice{Provider: "google", Model: "gemini-4-argon", InputPerM: 2, CacheReadPerM: 0.1, CacheWritePerM: 2, OutputPerM: 10}},
		{"gemini-3.8-flash", ModelPrice{Provider: "google", Model: "gemini-3.8-flash", InputPerM: 0.75, CacheReadPerM: 0.075, CacheWritePerM: 0.75, OutputPerM: 3.75}},
		{"gemini-3.7-flash", ModelPrice{Provider: "google", Model: "gemini-3.7-flash", InputPerM: 0.75, CacheReadPerM: 0.075, CacheWritePerM: 0.75, OutputPerM: 3.75}},
		{"gemini-3.6-flash", ModelPrice{Provider: "google", Model: "gemini-3.6-flash", InputPerM: 0.75, CacheReadPerM: 0.075, CacheWritePerM: 0.75, OutputPerM: 3.75}},
		{"gemini-3.5-flash", ModelPrice{Provider: "google", Model: "gemini-3.5-flash", InputPerM: 0.75, CacheReadPerM: 0.075, CacheWritePerM: 0.75, OutputPerM: 3.75}},
		{"gemini-3.5-flash-lite", ModelPrice{Provider: "google", Model: "gemini-3.5-flash-lite", InputPerM: 0.3, CacheReadPerM: 0.03, CacheWritePerM: 0.3, OutputPerM: 2.5}},
		{"gemini-3.1-flash-lite", ModelPrice{Provider: "google", Model: "gemini-3.1-flash-lite", InputPerM: 0.25, CacheReadPerM: 0.025, CacheWritePerM: 0.25, OutputPerM: 1.5}},
		{"gemini-3.1-pro", ModelPrice{Provider: "google", Model: "gemini-3.1-pro", InputPerM: 2, CacheReadPerM: 0.2, CacheWritePerM: 2, OutputPerM: 12}},
		{"gemini-3.1-pro-preview", ModelPrice{Provider: "google", Model: "gemini-3.1-pro-preview", InputPerM: 2, CacheReadPerM: 0.2, CacheWritePerM: 2, OutputPerM: 12}},
		{"gemini-3.1-pro-preview-customtools", ModelPrice{Provider: "google", Model: "gemini-3.1-pro-preview-customtools", InputPerM: 2, CacheReadPerM: 0.2, CacheWritePerM: 2, OutputPerM: 12}},
		{"gemini-3-flash", ModelPrice{Provider: "google", Model: "gemini-3-flash", InputPerM: 0.5, CacheReadPerM: 0.05, CacheWritePerM: 0.5, OutputPerM: 3}},
		{"gemini-3-flash-preview", ModelPrice{Provider: "google", Model: "gemini-3-flash-preview", InputPerM: 0.5, CacheReadPerM: 0.05, CacheWritePerM: 0.5, OutputPerM: 3}},
		{"gemini-2.5-pro", ModelPrice{Provider: "google", Model: "gemini-2.5-pro", InputPerM: 1.25, CacheReadPerM: 0.125, CacheWritePerM: 1.25, OutputPerM: 10}},
		{"gemini-2.5-flash", ModelPrice{Provider: "google", Model: "gemini-2.5-flash", InputPerM: 0.3, CacheReadPerM: 0.03, CacheWritePerM: 0.3, OutputPerM: 2.5}},
		{"gemini-2.5-flash-lite", ModelPrice{Provider: "google", Model: "gemini-2.5-flash-lite", InputPerM: 0.1, CacheReadPerM: 0.01, CacheWritePerM: 0.1, OutputPerM: 0.4}},
		{"gemini-robotics-er-2-preview", ModelPrice{Provider: "google", Model: "gemini-robotics-er-2-preview", InputPerM: 1, CacheReadPerM: 0.1, CacheWritePerM: 1, OutputPerM: 5}},
		{"gemini-robotics-er-2-streaming-preview", ModelPrice{Provider: "google", Model: "gemini-robotics-er-2-streaming-preview", InputPerM: 1, CacheReadPerM: 0, CacheWritePerM: 0, OutputPerM: 5}},
		{"gemini-3.8-flash-tts", ModelPrice{Provider: "google", Model: "gemini-3.8-flash-tts", InputPerM: 0.5, CacheReadPerM: 0, CacheWritePerM: 0, OutputPerM: 9}},
		{"gemini-3.8-flash-lite-tts", ModelPrice{Provider: "google", Model: "gemini-3.8-flash-lite-tts", InputPerM: 0.5, CacheReadPerM: 0, CacheWritePerM: 0, OutputPerM: 6}},
		{"gemini-3.1-flash-tts-preview", ModelPrice{Provider: "google", Model: "gemini-3.1-flash-tts-preview", InputPerM: 1, CacheReadPerM: 0, CacheWritePerM: 0, OutputPerM: 20}},
		{"gemini-2.5-flash-preview-tts", ModelPrice{Provider: "google", Model: "gemini-2.5-flash-preview-tts", InputPerM: 0.5, CacheReadPerM: 0, CacheWritePerM: 0, OutputPerM: 10}},
		{"gemini-2.5-pro-preview-tts", ModelPrice{Provider: "google", Model: "gemini-2.5-pro-preview-tts", InputPerM: 1, CacheReadPerM: 0, CacheWritePerM: 0, OutputPerM: 20}},
		{"gemini-3.5-transcribe", ModelPrice{Provider: "google", Model: "gemini-3.5-transcribe", InputPerM: 2, CacheReadPerM: 0, CacheWritePerM: 0, OutputPerM: 12}},
		{"gemini-3.5-transcribe-live", ModelPrice{Provider: "google", Model: "gemini-3.5-transcribe-live", InputPerM: 3.5, CacheReadPerM: 0, CacheWritePerM: 0, OutputPerM: 21}},
		{"gemini-3.5-live-translate-preview", ModelPrice{Provider: "google", Model: "gemini-3.5-live-translate-preview", InputPerM: 3.5, CacheReadPerM: 0, CacheWritePerM: 0, OutputPerM: 21}},
	}
	for _, tc := range cases {
		for _, alias := range []string{tc.id, "google/" + tc.id, "custom:google/" + tc.id + "[1m]"} {
			t.Run(alias, func(t *testing.T) {
				if got, ok := PriceForModelAlias(alias); !ok || got != tc.price {
					t.Fatalf("PriceForModelAlias(%q) = %+v, %v; want %+v", alias, got, ok, tc.price)
				}
			})
		}
	}
	for _, tc := range []struct{ label, id string }{
		{"Gemini 3.8 Flash", "gemini-3.8-flash"},
		{"Gemini 3.5 Flash", "gemini-3.5-flash"},
		{"Gemini 3.5 Flash-Lite", "gemini-3.5-flash-lite"},
		{"Gemini 3.1 Pro", "gemini-3.1-pro"},
		{"Gemini 4 Argon", "gemini-4-argon"},
	} {
		want, ok := PriceForModelAlias(tc.id)
		if !ok {
			t.Fatalf("missing price for %q", tc.id)
		}
		for _, alias := range []string{tc.label, tc.label + " (High)", "google/" + tc.label + " (Low)", tc.id + "-minimal", "antigravity:" + tc.id + "-high", "custom:google/" + tc.id + "-low[1m]"} {
			if got, ok := PriceForModelAlias(alias); !ok || got != want {
				t.Errorf("display/effort alias %q = %+v, %v; want %+v", alias, got, ok, want)
			}
		}
	}
	for _, alias := range []string{
		"gemini-3.8-flash-lite",
		"gemini-3.8-flash-live",
		"gemini-3.8-flash-preview",
		"gemini-3.8-flash-ultra",
		"gemini-3-8-flash",
		"Gemini 3.8 Flash (Ultra)",
		"Gemini 3.9 Flash (High)",
		"gemini-3.8-flash[1m][2m]",
		"gemini-3.1-pro-preview-extra",
		"gemini-3.1-pro-image",
		"gemini-2.5-pro-preview-05-06",
		"gemini-2.5-flash-lite-preview-09-2025",
		"gemini-2.5-flash-image",
		"gemini-3-pro-image",
		"gemini-nano-banana-2.1",
		"gemini-3.1-flash-image",
		"gemini-3.1-flash-lite-image",
		"gemini-3.8-live",
		"gemini-3.8-live-extended-thinking",
		"gemini-3.1-flash-live-preview",
		"gemini-2.5-flash-native-audio-preview-12-2025",
		"gemini-omni-1.1-flash",
		"gemini-omni-flash-preview",
		"gemini-embedding-2",
		"gemini-4-argon-fast",
		"gemini-4-argon[1m][2m]",
	} {
		if got, ok := PriceForModelAlias(alias); ok {
			t.Errorf("unknown or multimodal alias %q borrowed a rate: %+v", alias, got)
		}
	}
}
