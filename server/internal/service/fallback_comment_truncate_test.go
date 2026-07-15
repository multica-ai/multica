package service

import (
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestTruncateFallbackCommentBody pins the completion-fallback comment cap that
// keeps a runaway raw-stream Output off the issue thread (GH #5455).
func TestTruncateFallbackCommentBody(t *testing.T) {
	t.Parallel()

	t.Run("short body passes through unchanged", func(t *testing.T) {
		t.Parallel()
		// A real final message — multi-line, well under the cap — must be stored
		// verbatim, newlines intact (unlike the summary flattening path).
		body := "I fixed the bug in the parser.\n\n- root cause: off-by-one\n- added a regression test"
		if got := truncateFallbackCommentBody(body, maxSynthesizedFallbackCommentRunes); got != body {
			t.Fatalf("short body was altered:\n got: %q\nwant: %q", got, body)
		}
	})

	t.Run("body exactly at the cap is untouched", func(t *testing.T) {
		t.Parallel()
		body := strings.Repeat("x", maxSynthesizedFallbackCommentRunes)
		if got := truncateFallbackCommentBody(body, maxSynthesizedFallbackCommentRunes); got != body {
			t.Fatalf("body at cap was truncated: len(got)=%d", utf8.RuneCountInString(got))
		}
	})

	t.Run("raw execution-stream dump is truncated to head plus marker", func(t *testing.T) {
		t.Parallel()
		// Reproduce the reporter's fingerprint: first-turn narration followed by
		// hundreds of repeated `tool call` lines — the shape a 200KB+ dump takes.
		var b strings.Builder
		b.WriteString("I'll start by reading the issue context and the relevant files.\n")
		for i := 0; i < 40000; i++ {
			b.WriteString("tool call\n")
		}
		dump := b.String()
		if utf8.RuneCountInString(dump) < 200_000 {
			t.Fatalf("test fixture too small: %d runes", utf8.RuneCountInString(dump))
		}

		got := truncateFallbackCommentBody(dump, maxSynthesizedFallbackCommentRunes)

		// The stored comment must be bounded, not the multi-hundred-KB tail.
		if n := utf8.RuneCountInString(got); n > maxSynthesizedFallbackCommentRunes+256 {
			t.Fatalf("truncated body still too large: %d runes", n)
		}
		// The head — the actual narration — is preserved.
		if !strings.HasPrefix(got, "I'll start by reading the issue context") {
			t.Fatalf("head narration lost: %q", got[:60])
		}
		// A truncation marker naming the omitted length is appended.
		if !strings.Contains(got, "output truncated") {
			t.Fatalf("truncation marker missing: %q", got[len(got)-120:])
		}
		omitted := utf8.RuneCountInString(dump) - maxSynthesizedFallbackCommentRunes
		if !strings.Contains(got, strconv.Itoa(omitted)) {
			t.Fatalf("omitted-count %d not reported in marker: %q", omitted, got[len(got)-160:])
		}
	})

	t.Run("cap counts runes not bytes for multibyte content", func(t *testing.T) {
		t.Parallel()
		// A body of maxRunes multibyte runes is > maxRunes bytes but == maxRunes
		// runes, so it must NOT be truncated — the boundary is rune-based.
		body := strings.Repeat("你", maxSynthesizedFallbackCommentRunes)
		if got := truncateFallbackCommentBody(body, maxSynthesizedFallbackCommentRunes); got != body {
			t.Fatalf("multibyte body at rune cap was wrongly truncated")
		}
		// One rune over the cap truncates and never splits a rune.
		over := strings.Repeat("你", maxSynthesizedFallbackCommentRunes+10)
		got := truncateFallbackCommentBody(over, maxSynthesizedFallbackCommentRunes)
		if !utf8.ValidString(got) {
			t.Fatalf("truncation split a multibyte rune, produced invalid UTF-8")
		}
	})
}
