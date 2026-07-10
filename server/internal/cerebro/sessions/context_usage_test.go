package sessions

import "testing"

func TestContextWindowForModel_ExactPerModel(t *testing.T) {
	cases := []struct {
		model string
		want  int64
	}{
		{"claude-fable-5", 1_000_000},  // Fable 5 ships the 1M window as standard (FIR-2580)
		{"claude-opus-4-8", 1_000_000}, // current Opus ships the 1M window as standard (FIR-1931)
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
	ctx, maxCtx, used, cacheShare, approximate := computeContextUsage(8_000, 409_200, 0, "claude-opus-4-8[1m]")
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
	if !approximate { // the cumulative fallback is always approximate (FIR-1931 Fix C)
		t.Errorf("approximate = false, want true for the cumulative fallback")
	}
}

func TestComputeContextUsage_NoTokens(t *testing.T) {
	ctx, _, used, cacheShare, _ := computeContextUsage(0, 0, 0, "claude-opus-4-8")
	if ctx != 0 || used != 0 || cacheShare != 0 {
		t.Errorf("zero usage = (%d,%d,%d), want all zero", ctx, used, cacheShare)
	}
}

// TestComputeContextUsage_ClampsTokensToWindow proves the FIR-1931 Fix C display
// bug is gone: a long warm-cache run whose cumulative cache_read alone is several
// times the window must never report a token figure larger than the window. The
// percent already clamped to 100, but the raw token count (6986k) leaked into the
// bar as "6986k / 1000k". It is now clamped and flagged approximate.
func TestComputeContextUsage_ClampsTokensToWindow(t *testing.T) {
	ctx, maxCtx, used, _, approximate := computeContextUsage(0, 6_986_000, 0, "claude-opus-4-8")
	if maxCtx != 1_000_000 {
		t.Fatalf("max context = %d, want 1000000", maxCtx)
	}
	if ctx != 1_000_000 {
		t.Errorf("context tokens = %d, want 1000000 (clamped to the window, not 6986000)", ctx)
	}
	if used != 100 {
		t.Errorf("used percent = %d, want 100", used)
	}
	if !approximate {
		t.Errorf("approximate = false, want true for the over-window cumulative fallback")
	}
}

func TestComputeContextFootprint_LastTurnNotLifetimeSum(t *testing.T) {
	// FIR-1856: the Codex screenshot showed 1955k/272k → pinned at 100% because
	// the indicator used the lifetime sum (total_token_usage). With the last-turn
	// footprint instead, a gpt-5.5 session whose final prompt is 110k (100k of it
	// cached) reads ~40% — not 100%. footprintInput already includes the cached
	// tokens (OpenAI accounting), so cacheRead must NOT be added on top.
	ctx, maxCtx, used, cacheShare, approximate := computeContextFootprint(110_000, 100_000, "gpt-5.5")
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
	if approximate { // an exact last-turn footprint is never approximate
		t.Errorf("approximate = true, want false for the last-turn footprint")
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

// FIR-1931: loadIssue must accept the human identifier ("FIR-1931"), not only a
// UUID. identifierNumber is the parser that decides identifier-vs-UUID; if it
// rejects a real identifier the context bar 400s and falls back to "estimate"
// even though real footprint data exists.
func TestIdentifierNumber(t *testing.T) {
	cases := []struct {
		in     string
		want   int32
		wantOK bool
	}{
		{"FIR-1931", 1931, true},
		{"MUL-3375", 3375, true},
		{"A-1", 1, true},
		{"MULTI-WORD-42", 42, true},                        // only the trailing -NUMBER matters
		{"b669a722-2e15-445a-b6a5-a5cdb576a989", 0, false}, // a UUID is not an identifier
		{"FIR-", 0, false},
		{"-1931", 0, false},
		{"FIR1931", 0, false},
		{"FIR-0", 0, false},
		{"FIR-12a", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		got, ok := identifierNumber(c.in)
		if ok != c.wantOK || got != c.want {
			t.Errorf("identifierNumber(%q) = (%d, %t), want (%d, %t)", c.in, got, ok, c.want, c.wantOK)
		}
	}
}
