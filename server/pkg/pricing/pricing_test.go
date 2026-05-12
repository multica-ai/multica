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
			// Opus 4.7 list pricing: 1M input @ $5/Mtok + 1M output @ $25/Mtok
			// = 500 + 2500 = 3000 cents.
			name:  "opus 4-7 — 1M in / 1M out",
			model: "claude-opus-4-7",
			usage: Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000},
			want:  3000,
		},
		{
			// Opus 4.1 still at the older $15/$75 tier: 1500 + 7500 = 9000 cents.
			name:  "opus 4-1 — 1M in / 1M out (legacy tier)",
			model: "claude-opus-4-1",
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
			// Haiku 4.5: 10k in / 10k out = 1.0 + 5.0 = 6.0 cents → ceil 6.
			name:  "haiku 4-5 — small task rounds up",
			model: "claude-haiku-4-5",
			usage: Usage{InputTokens: 10_000, OutputTokens: 10_000},
			want:  6,
		},
		{
			// Cache-heavy Opus: 1M cache_read @ 50 cents/Mtok = 50 cents.
			name:  "opus cache-read",
			model: "claude-opus-4-7",
			usage: Usage{CacheReadTokens: 1_000_000},
			want:  50,
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

// CEREBRO-PATCH(pricing-test-anthropic-rates-2026-05): expectations updated
// for the corrected Opus 4.5+ rates and the new date-suffix stripping in
// lookup() shipped alongside the pricing fix in JEH-996.
func TestComputeCents_StripsDateSnapshotSuffix(t *testing.T) {
	// Dated snapshots (`claude-opus-4-7-20251030`, `claude-sonnet-4-6-2026-01-15`,
	// `claude-haiku-4-5-latest`) share pricing with the family. The lookup
	// strips the suffix on miss so they don't fall through to the worst-case
	// fallback (which is fine for safety, but obscures unmapped models in
	// the Known() diagnostic).
	cases := []struct {
		dated, base string
	}{
		{"claude-opus-4-7-20251030", "claude-opus-4-7"},
		{"claude-sonnet-4-6-2026-01-15", "claude-sonnet-4-6"},
		{"claude-haiku-4-5-latest", "claude-haiku-4-5"},
	}
	usage := Usage{InputTokens: 100_000, OutputTokens: 100_000}
	for _, c := range cases {
		t.Run(c.dated, func(t *testing.T) {
			want := ComputeCents(c.base, usage)
			got := ComputeCents(c.dated, usage)
			if got != want {
				t.Errorf("ComputeCents(%q) = %d, want %d (== ComputeCents(%q))", c.dated, got, want, c.base)
			}
			if !Known(c.dated) {
				t.Errorf("Known(%q) = false, expected true after date-suffix strip", c.dated)
			}
		})
	}
}

func TestComputeCents_UnknownModelUsesWorstCase(t *testing.T) {
	// Unknown model should be priced as Opus 4.1 (the fail-safe fallback —
	// the priciest entry in the table) so budget caps over-estimate rather
	// than under-estimate.
	usage := Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000}
	got := ComputeCents("future-model-not-yet-priced", usage)
	want := ComputeCents("claude-opus-4-1", usage)
	if got != want {
		t.Errorf("unknown model should fall back to opus 4.1 (%d), got %d", want, got)
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
