package pricing

// CEREBRO-PATCH(pricing-pricing-test): cerebro modification of upstream file

import "testing"

func TestComputeCents_KnownModels(t *testing.T) {
	tests := []struct {
		name  string
		model string
		usage Usage
		want  int64
	}{
		{
			// 1M input + 1M output @ Opus rates = 1500 + 7500 = 9000 cents.
			name:  "opus 4-7 — 1M in / 1M out",
			model: "claude-opus-4-7",
			usage: Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000},
			want:  9000,
		},
		{
			// Sonnet: 100k in / 100k out = 30 + 150 = 180 cents.
			name:  "sonnet 4-6 — 100k in / 100k out",
			model: "claude-sonnet-4-6",
			usage: Usage{InputTokens: 100_000, OutputTokens: 100_000},
			want:  180,
		},
		{
			// Haiku: 10k in / 10k out = 0.8 + 4 = 4.8 → ceil 5.
			name:  "haiku 4-5 — small task rounds up",
			model: "claude-haiku-4-5",
			usage: Usage{InputTokens: 10_000, OutputTokens: 10_000},
			want:  5,
		},
		{
			// Cache-heavy Opus: 1M cache_read @ 150 cents/Mtok = 150 cents.
			name:  "opus cache-read",
			model: "claude-opus-4-7",
			usage: Usage{CacheReadTokens: 1_000_000},
			want:  150,
		},
		{
			// gpt-5 has no cache-write line (writes are free).
			name:  "gpt-5 cache-write is free",
			model: "gpt-5",
			usage: Usage{CacheWriteTokens: 1_000_000},
			want:  0,
		},
		{
			// Empty usage → 0, no NaN, no panic.
			name:  "zero usage",
			model: "claude-opus-4-7",
			usage: Usage{},
			want:  0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ComputeCents(tc.model, tc.usage)
			if got != tc.want {
				t.Errorf("ComputeCents(%q, %+v) = %d, want %d", tc.model, tc.usage, got, tc.want)
			}
		})
	}
}

func TestComputeCents_NormalizesModelName(t *testing.T) {
	want := ComputeCents("claude-opus-4-7", Usage{InputTokens: 100_000})
	if got := ComputeCents("  Claude-Opus-4-7  ", Usage{InputTokens: 100_000}); got != want {
		t.Errorf("expected case/whitespace normalization, want %d got %d", want, got)
	}
}

func TestComputeCents_UnknownModelUsesWorstCase(t *testing.T) {
	// Unknown model should be priced as Opus (the fail-safe fallback) so
	// budget caps over-estimate rather than under-estimate.
	usage := Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000}
	got := ComputeCents("future-model-not-yet-priced", usage)
	want := ComputeCents("claude-opus-4-7", usage)
	if got != want {
		t.Errorf("unknown model should fall back to opus (%d), got %d", want, got)
	}
}

func TestKnown(t *testing.T) {
	if !Known("claude-opus-4-7") {
		t.Error("expected claude-opus-4-7 to be known")
	}
	if Known("future-model") {
		t.Error("expected future-model to be unknown")
	}
}
