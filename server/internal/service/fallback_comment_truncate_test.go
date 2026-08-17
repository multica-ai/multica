package service

import (
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

	t.Run("raw execution-stream dump is replaced with a safe notice", func(t *testing.T) {
		t.Parallel()
		// Reproduce the reporter's fingerprint: first-turn narration followed by
		// hundreds of repeated `tool call` lines and a tail answer — the shape a
		// 200KB+ dump takes. None of that untrusted output may reach the comment.
		var b strings.Builder
		b.WriteString("I'll start by reading the issue context and the relevant files.\n")
		for i := 0; i < 40000; i++ {
			b.WriteString("tool call\n")
		}
		b.WriteString("FINAL ANSWER THAT MUST NOT BE MISTAKEN FOR TRUSTED OUTPUT")
		dump := b.String()
		if utf8.RuneCountInString(dump) < 200_000 {
			t.Fatalf("test fixture too small: %d runes", utf8.RuneCountInString(dump))
		}

		got := truncateFallbackCommentBody(dump, maxSynthesizedFallbackCommentRunes)

		if n := utf8.RuneCountInString(got); n > 256 {
			t.Fatalf("safe notice is unexpectedly large: %d runes", n)
		}
		for _, leaked := range []string{"I'll start", "tool call", "FINAL ANSWER"} {
			if strings.Contains(got, leaked) {
				t.Fatalf("safe notice leaked raw output %q: %q", leaked, got)
			}
		}
		if !strings.Contains(got, "not posted") {
			t.Fatalf("safe notice does not explain that output was withheld: %q", got)
		}
		if !strings.Contains(got, "Execution log") {
			t.Fatalf("safe notice does not direct the user to the task run: %q", got)
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
		// One rune over the cap is replaced rather than leaking a raw excerpt.
		over := strings.Repeat("你", maxSynthesizedFallbackCommentRunes+10)
		got := truncateFallbackCommentBody(over, maxSynthesizedFallbackCommentRunes)
		if !utf8.ValidString(got) {
			t.Fatalf("safe notice is invalid UTF-8")
		}
		if strings.Contains(got, "你") {
			t.Fatalf("safe notice leaked a multibyte raw-output excerpt")
		}
	})
}
