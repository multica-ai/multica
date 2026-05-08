package agent_title

import (
	"context"
	"strings"
	"testing"
)

// Tests focus on the heuristic + Build wrapper, which is what runs in
// every environment without ANTHROPIC_API_KEY set. The LLM path is
// covered by manual smoke-tests against a real key.

func TestBuild_EmptyComment_UsesFallback(t *testing.T) {
	b := New()
	got := b.Build(context.Background(), "   \n  ", "#firtal-test")
	if got != "#firtal-test" {
		t.Fatalf("expected fallback, got %q", got)
	}
}

func TestBuild_EmptyEverything_ReturnsPlaceholder(t *testing.T) {
	b := New()
	got := b.Build(context.Background(), "", "")
	if got != "Agent task" {
		t.Fatalf("expected placeholder, got %q", got)
	}
}

func TestBuild_HeuristicStripsMentionMarkdown(t *testing.T) {
	b := &Builder{} // apiKey empty → heuristic
	got := b.Build(context.Background(), "[@Lando](mention://agent/123) lav et summary af CAC for uge 18", "fallback")
	if got != "lav et summary af CAC for uge 18" {
		t.Fatalf("got %q", got)
	}
}

func TestBuild_HeuristicStripsBareMention(t *testing.T) {
	b := &Builder{}
	got := b.Build(context.Background(), "@Lando hvad er status på P&L rapporten?", "fallback")
	if got != "hvad er status på P&L rapporten?" {
		t.Fatalf("got %q", got)
	}
}

func TestBuild_HeuristicTakesFirstLine(t *testing.T) {
	b := &Builder{}
	got := b.Build(context.Background(), "Hej Lando\n\nKan du hjælpe med rapporten?", "fallback")
	if got != "Hej Lando" {
		t.Fatalf("got %q", got)
	}
}

func TestBuild_HeuristicTruncatesLongComment(t *testing.T) {
	b := &Builder{}
	long := strings.Repeat("a", 200)
	got := b.Build(context.Background(), long, "fallback")
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("expected truncation marker, got %q", got)
	}
	rs := []rune(got)
	if len(rs) > titleMaxRunes+1 { // +1 for ellipsis
		t.Fatalf("expected ≤ %d runes, got %d (%q)", titleMaxRunes+1, len(rs), got)
	}
}

func TestCleanTitle_StripsQuotesAndPeriod(t *testing.T) {
	cases := map[string]string{
		`"Summary of CAC week 18."`: "Summary of CAC week 18",
		`'Summary of CAC week 18'`:  "Summary of CAC week 18",
		` Summary of CAC week 18. `: "Summary of CAC week 18",
		`Already clean`:             "Already clean",
	}
	for in, want := range cases {
		if got := cleanTitle(in); got != want {
			t.Errorf("cleanTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStripMentions_PreservesPunctuation(t *testing.T) {
	got := stripMentions("@Lando, hvordan ser CAC ud?")
	if got != "hvordan ser CAC ud?" {
		t.Fatalf("got %q", got)
	}
}
