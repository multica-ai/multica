package dingtalk

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// Title is selected context on a later DingTalk native quote. It must retain
// source syntax and destinations, including bytes beyond the display preview.

func TestQuotePreview_ByteBudget(t *testing.T) {
	for _, unit := range []string{"x", "界", "🚀"} {
		for _, length := range []int{63, 64, 65, 252, 253, 256, 257, 512, 16001} {
			body := strings.Repeat(unit, length)
			want := body
			if len(body) > 256 {
				want = strings.Repeat(unit, 252/len(unit)) + "\n..."
			}
			if got := quotePreview(body); got != want || !utf8.ValidString(got) {
				t.Errorf("preview for %d runes = %q, want %q", length, got, want)
			}
		}
	}
	// A byte cutoff can land inside a rune when the text mixes ASCII and
	// multibyte characters; repeating a single rune alone misses this boundary.
	for _, prefix := range []string{"a", "ab", "abc"} {
		for _, unit := range []string{"界", "🚀"} {
			body := prefix + strings.Repeat(unit, 300)
			want := prefix + strings.Repeat(unit, (252-len(prefix))/len(unit)) + "\n..."
			if got := quotePreview(body); got != want || len(got) > 256 || !utf8.ValidString(got) {
				t.Errorf("mixed preview for %q + %q = %q, want %q", prefix, unit, got, want)
			}
		}
	}
}

func TestChunkMarkdown_ShortPassthrough(t *testing.T) {
	body := "a short body"
	got := chunkMarkdown(body)
	if len(got) != 1 || got[0] != body {
		t.Errorf("short body must pass through unchunked: %v", got)
	}
}

func TestChunkMarkdown_SplitsUnderByteBudget(t *testing.T) {
	line := strings.Repeat("x", 1000) + "\n"
	body := strings.Repeat(line, 40) // ~40k bytes
	chunks := chunkMarkdown(body)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		if len(c) > markdownByteBudget {
			t.Errorf("chunk %d exceeds budget: %d bytes", i, len(c))
		}
	}
	// Reassembling the chunks recovers the original text.
	if strings.Join(chunks, "") != body {
		t.Error("rejoined chunks must equal the original body")
	}
}

func TestChunkMarkdown_ReopensCodeFence(t *testing.T) {
	// A code block that straddles the byte budget must be closed at the split and
	// reopened in the next chunk so neither half renders broken.
	codeLine := strings.Repeat("y", 500) + "\n"
	body := "```\n" + strings.Repeat(codeLine, 40) + "```\n"
	chunks := chunkMarkdown(body)
	if len(chunks) < 2 {
		t.Fatalf("expected the long code block to split, got %d chunks", len(chunks))
	}
	for i, c := range chunks {
		if len(c) > markdownByteBudget {
			t.Errorf("chunk %d exceeds budget: %d bytes", i, len(c))
		}
		fences := strings.Count(c, "```")
		if fences%2 != 0 {
			t.Errorf("chunk %d has unbalanced code fences (%d): not self-contained", i, fences)
		}
	}
}

func TestChunkMarkdown_ReopenPreservesLanguage(t *testing.T) {
	// DingTalk highlights a code block by its language info string (```go). When a
	// block straddles the budget, the reopened chunk must carry the SAME info
	// string, or the continuation renders unhighlighted.
	codeLine := strings.Repeat("y", 500) + "\n"
	body := "```go\n" + strings.Repeat(codeLine, 40) + "```\n"
	chunks := chunkMarkdown(body)
	if len(chunks) < 2 {
		t.Fatalf("expected the long code block to split, got %d chunks", len(chunks))
	}
	if !strings.HasPrefix(chunks[1], "```go\n") {
		t.Errorf("continuation chunk must reopen the go fence, got prefix %q", chunks[1][:min(6, len(chunks[1]))])
	}
}

func TestChunkMarkdown_HardSplitsOversizedLine(t *testing.T) {
	body := strings.Repeat("z", markdownByteBudget*2+50) // single line, no newlines
	chunks := chunkMarkdown(body)
	if len(chunks) < 2 {
		t.Fatalf("oversized single line must hard-split, got %d", len(chunks))
	}
	for i, c := range chunks {
		if len(c) > markdownByteBudget {
			t.Errorf("hard-split chunk %d exceeds budget: %d", i, len(c))
		}
	}
	if strings.Join(chunks, "") != body {
		t.Error("hard-split chunks must rejoin to the original")
	}
}

func TestChunkMarkdown_OversizedLineInsideCodeFence(t *testing.T) {
	// An oversized line inside an open code fence must be split into self-contained
	// code blocks (no plain-text leak, no stray empty code-block messages).
	oversized := strings.Repeat("a", markdownByteBudget*2+50)
	body := "```\n" + oversized + "\n```\n"
	chunks := chunkMarkdown(body)
	if len(chunks) < 2 {
		t.Fatalf("oversized fenced line must split, got %d chunks", len(chunks))
	}
	for i, c := range chunks {
		if len(c) > markdownByteBudget {
			t.Errorf("chunk %d exceeds budget: %d bytes", i, len(c))
		}
		if fences := strings.Count(c, "```"); fences%2 != 0 {
			t.Errorf("chunk %d has unbalanced code fences (%d): not self-contained", i, fences)
		}
		if isBlankChunk(c) {
			t.Errorf("chunk %d is an empty code block", i)
		}
	}
}

func TestChunkMarkdown_LongFenceInfoCannotExhaustPieceBudget(t *testing.T) {
	// Keep the opening line just inside the normal-line budget, then force the
	// following code line through hardSplit. Before the continuation fence was
	// bounded, this made pieceBudget negative and panicked.
	fence := "```" + strings.Repeat("x", markdownContentByteBudget-5)
	body := fence + "\n" + strings.Repeat("\u754c", markdownByteBudget) + "\n```\n"
	chunks := chunkMarkdown(body)
	if len(chunks) < 2 {
		t.Fatalf("pathological fenced body must split, got %d chunks", len(chunks))
	}
	for i, chunk := range chunks {
		if len(chunk) > markdownByteBudget {
			t.Errorf("chunk %d exceeds budget: %d bytes", i, len(chunk))
		}
		if fences := strings.Count(chunk, "```"); fences%2 != 0 {
			t.Errorf("chunk %d has unbalanced fences: %d", i, fences)
		}
	}
}

func TestHardSplitDefendsAgainstInvalidBudget(t *testing.T) {
	for _, budget := range []int{-1, 0, 1} {
		pieces := hardSplit("\u754c\u754c", budget)
		if got := strings.Join(pieces, ""); got != "\u754c\u754c" {
			t.Fatalf("hardSplit budget %d rejoined to %q", budget, got)
		}
		for _, piece := range pieces {
			if !utf8.ValidString(piece) {
				t.Fatalf("hardSplit budget %d produced invalid UTF-8 %q", budget, piece)
			}
		}
	}
}

func TestQuotePreviewPreservesLiteralParagraphsAndByteBudget(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{"", ""}, {" \n\t", ""},
		{"**literal** [link](url)\nsecond line\n\nLater paragraph", "**literal** [link](url)\nsecond line\n\nLater paragraph"},
		{"First\r\n \t\r\nLater", "First\n \t\nLater"},
		{strings.Repeat("界", 86), strings.Repeat("界", 84) + "\n..."},
		{strings.Repeat("x", 256), strings.Repeat("x", 256)},
		{strings.Repeat("x", 256) + "\n\nLater", strings.Repeat("x", 252) + "\n..."},
		{"Already shortened\n...", "Already shortened\n..."},
	} {
		got := quotePreview(tc.body)
		if got != tc.want || len(got) > 256 || !utf8.ValidString(got) {
			t.Fatalf("quote preview=%q, want=%q", got, tc.want)
		}
		if quotePreview(got) != got {
			t.Fatal("rendering an already stored preview changed it")
		}
	}
}

func TestMarkdownTitle(t *testing.T) {
	cases := map[string]string{
		"# Heading one\nbody":   "Heading one",
		"\n\n## Second\nmore":   "Second",
		"no heading here":       defaultMarkdownTitle,
		"plain line\n# late":    defaultMarkdownTitle, // first non-empty line is not a heading
		"###   spaced   \nbody": "spaced",
	}
	for body, want := range cases {
		if got := markdownTitle(body); got != want {
			t.Errorf("markdownTitle(%q) = %q, want %q", body, got, want)
		}
	}
}
