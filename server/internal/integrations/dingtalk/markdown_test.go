package dingtalk

import (
	"strings"
	"testing"
)

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
		if len(c) > markdownByteBudget+len("```\n\n```") {
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
