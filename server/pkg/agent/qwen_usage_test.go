package agent

import "testing"

func TestQwenTokenUsageExcludesCacheReads(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		raw  qwenUsage
		want TokenUsage
	}{
		{"uncached", qwenUsage{InputTokens: 1000, OutputTokens: 50}, TokenUsage{InputTokens: 1000, OutputTokens: 50}},
		{"mixed", qwenUsage{InputTokens: 1000, OutputTokens: 50, CacheReadInputTokens: 600}, TokenUsage{InputTokens: 400, OutputTokens: 50, CacheReadTokens: 600}},
		{"all_cached", qwenUsage{InputTokens: 600, CacheReadInputTokens: 600}, TokenUsage{CacheReadTokens: 600}},
		{"cache_exceeds_input", qwenUsage{InputTokens: 5, OutputTokens: 2, CacheReadInputTokens: 10}, TokenUsage{OutputTokens: 2, CacheReadTokens: 10}},
		{"empty", qwenUsage{}, TokenUsage{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := qwenTokenUsage(&tc.raw); got != tc.want {
				t.Fatalf("qwenTokenUsage(%+v) = %+v, want %+v", tc.raw, got, tc.want)
			}
		})
	}
}
