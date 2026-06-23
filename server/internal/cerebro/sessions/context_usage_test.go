package sessions

import "testing"

func TestContextWindowForModel_ExactPerModel(t *testing.T) {
	cases := []struct {
		model string
		want  int64
	}{
		{"claude-opus-4-8", 1_000_000},   // current Opus ships the 1M window as standard (FIR-1931)
		{"claude-opus-4-7", 1_000_000},
		{"claude-opus-4-6", 1_000_000},
		{"claude-opus-4-5", 200_000},     // older Opus stays conservative until a 1M window is confirmed
		{"claude-sonnet-4-6", 1_000_000}, // Sonnet 4.x ships the 1M window — the case the old prefix guess got wrong
		{"claude-sonnet-4-5", 1_000_000},
		{"claude-haiku-4-5", 200_000},
		{"gpt-5.5", 272_000},
		{"gpt-5", 272_000},
		{"gemini-2.5-pro", 1_000_000},
		{"Claude-Sonnet-4-5-20251101", 1_000_000},   // dated snapshot resolves to the family row
		{"claude-opus-4-8[1m]", 1_000_000},          // [1m] long-context tag → 1M
		{"Claude-Opus-4-8[1m]", 1_000_000},          // case-insensitive
		{"claude-opus-4-8[1m]-20260101", 1_000_000}, // [1m] tag wins even with a trailing date
		{"claude-opus-4-8[exp]", 1_000_000},         // an unrelated bracketed tag strips to the base family (now 1M)
		{"claude-opus-4-5[exp]", 200_000},           // older Opus base still resolves to 200k after stripping a tag
		{"", 200_000},                               // unknown/empty falls back to the conservative default
		{"some-future-model", 200_000},
	}
	for _, c := range cases {
		if got := contextWindowForModel(c.model); got != c.want {
			t.Errorf("contextWindowForModel(%q) = %d, want %d", c.model, got, c.want)
		}
	}
}

func TestComputeContextUsage_CountsCachedPrompt(t *testing.T) {
	// FIR-1839 1D regression: Jesper's screenshot — a warm session on
	// claude-opus-4-8[1m] with only 8k fresh input but 409.2k served from cache.
	// The old formula counted `input` alone → 0% used and a nonsense 100% cache
	// share. The whole prompt is input + cache_read + cache_write.
	ctx, maxCtx, used, cacheShare := computeContextUsage(8_000, 409_200, 0, "claude-opus-4-8[1m]")
	if ctx != 417_200 {
		t.Errorf("context tokens = %d, want 417200", ctx)
	}
	if maxCtx != 1_000_000 {
		t.Errorf("max context = %d, want 1000000", maxCtx)
	}
	if used != 41 { // 417200 / 1_000_000 = 41.72% → 41, not 0
		t.Errorf("used percent = %d, want 41", used)
	}
	if cacheShare != 98 { // 409200 / 417200 = 98%, not a clamped 100
		t.Errorf("cache share = %d, want 98", cacheShare)
	}
}

func TestComputeContextUsage_NoTokens(t *testing.T) {
	ctx, _, used, cacheShare := computeContextUsage(0, 0, 0, "claude-opus-4-8")
	if ctx != 0 || used != 0 || cacheShare != 0 {
		t.Errorf("zero usage = (%d,%d,%d), want all zero", ctx, used, cacheShare)
	}
}

func TestComputeContextFootprint_LastTurnNotLifetimeSum(t *testing.T) {
	// FIR-1856: the Codex screenshot showed 1955k/272k → pinned at 100% because
	// the indicator used the lifetime sum (total_token_usage). With the last-turn
	// footprint instead, a gpt-5.5 session whose final prompt is 110k (100k of it
	// cached) reads ~40% — not 100%. footprintInput already includes the cached
	// tokens (OpenAI accounting), so cacheRead must NOT be added on top.
	ctx, maxCtx, used, cacheShare := computeContextFootprint(110_000, 100_000, "gpt-5.5")
	if ctx != 110_000 {
		t.Errorf("context tokens = %d, want 110000 (no double-count of cached)", ctx)
	}
	if maxCtx != 272_000 {
		t.Errorf("max context = %d, want 272000", maxCtx)
	}
	if used != 40 { // 110000 / 272000 = 40.4% → 40, not the bugged 100
		t.Errorf("used percent = %d, want 40", used)
	}
	if cacheShare != 90 { // 100000 / 110000 = 90%
		t.Errorf("cache share = %d, want 90", cacheShare)
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
