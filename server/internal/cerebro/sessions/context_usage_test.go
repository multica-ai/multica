package sessions

import "testing"

func TestContextWindowForModel_ExactPerModel(t *testing.T) {
	cases := []struct {
		model string
		want  int64
	}{
		{"claude-opus-4-8", 200_000},
		{"claude-sonnet-4-6", 1_000_000}, // Sonnet 4.x ships the 1M window — the case the old prefix guess got wrong
		{"claude-sonnet-4-5", 1_000_000},
		{"claude-haiku-4-5", 200_000},
		{"gpt-5.5", 272_000},
		{"gpt-5", 272_000},
		{"gemini-2.5-pro", 1_000_000},
		{"Claude-Sonnet-4-5-20251101", 1_000_000}, // dated snapshot resolves to the family row
		{"claude-opus-4-8[1m]", 1_000_000},        // [1m] long-context beta tag → 1M, not the base Opus 200k (the live miss Jesper flagged)
		{"Claude-Opus-4-8[1m]", 1_000_000},        // case-insensitive
		{"claude-opus-4-8[1m]-20260101", 1_000_000}, // [1m] tag wins even with a trailing date
		{"claude-opus-4-8[exp]", 200_000},         // an unrelated bracketed tag strips to the base family (200k)
		{"", 200_000},                             // unknown/empty falls back to the conservative default
		{"some-future-model", 200_000},
	}
	for _, c := range cases {
		if got := contextWindowForModel(c.model); got != c.want {
			t.Errorf("contextWindowForModel(%q) = %d, want %d", c.model, got, c.want)
		}
	}
}

func TestClampPercent(t *testing.T) {
	cases := []struct{ in, want int }{
		{-5, 0}, {0, 0}, {50, 50}, {100, 100}, {137, 100},
	}
	for _, c := range cases {
		if got := clampPercent(c.in); got != c.want {
			t.Errorf("clampPercent(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}
