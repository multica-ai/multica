package contextduplication

import (
	"strings"
	"testing"
	"time"
)

func TestAnalyze_duplicateSentenceAcrossSections(t *testing.T) {
	shared := "Always write Danish comments and explain technical terms the first time."
	sections := []Section{
		{
			ID:      "workspace",
			Title:   "Workspace Context",
			Content: shared + " Extra workspace rules.",
			Tokens:  EstimateTokens(shared + " Extra workspace rules."),
		},
		{
			ID:      "skills",
			Title:   "Skills",
			Content: shared + " Skill list here.",
			Tokens:  EstimateTokens(shared + " Skill list here."),
		},
	}
	r := Analyze(sections)
	if r.DuplicateTokens <= 0 {
		t.Fatalf("expected duplicate tokens, got %d", r.DuplicateTokens)
	}
	if r.DuplicatePct <= 0 {
		t.Fatalf("expected duplicate pct > 0, got %f", r.DuplicatePct)
	}
	if len(r.TopOverlapSections) == 0 {
		t.Fatal("expected at least one overlap pair")
	}
}

func TestParseSections_splitsHeadings(t *testing.T) {
	md := "# Multica\n\n## Agent Identity\n\nYou are Test.\n\n## Workspace Context\n\nBe kind.\n"
	secs := ParseSections(md)
	if len(secs) < 2 {
		t.Fatalf("expected >=2 sections, got %d", len(secs))
	}
}

func TestAnalyze_performance30kTokens(t *testing.T) {
	para := strings.Repeat("word ", 200) // ~800 chars ~200 tokens per para
	var b strings.Builder
	for i := 0; i < 40; i++ {
		b.WriteString("## Section ")
		b.WriteString(string(rune('A' + i%26)))
		b.WriteString("\n\n")
		if i%5 == 0 {
			b.WriteString("Shared policy: use multica CLI for all platform access. ")
		}
		b.WriteString(para)
		b.WriteString("\n\n")
	}
	sections := ParseSections(b.String())
	start := time.Now()
	r := Analyze(sections)
	elapsed := time.Since(start)
	if r.TotalTokens < 1000 {
		t.Fatalf("expected large prompt, got %d tokens", r.TotalTokens)
	}
	if elapsed > 50*time.Millisecond {
		t.Fatalf("analyze took %v, want <50ms p99 budget", elapsed)
	}
}
