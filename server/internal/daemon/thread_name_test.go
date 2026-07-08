package daemon

import (
	"strings"
	"testing"
)

func TestDeriveTaskThreadNamePrefersClaimedThreadName(t *testing.T) {
	t.Parallel()

	got := deriveTaskThreadName(Task{
		ThreadName:            "  Fix login redirect  ",
		TriggerCommentContent: "please look at this comment",
		ChatMessage:           "chat fallback",
	})
	if got != "Fix login redirect" {
		t.Fatalf("thread name = %q, want %q", got, "Fix login redirect")
	}
}

func TestDeriveTaskThreadNameFallsBackToTaskContext(t *testing.T) {
	t.Parallel()

	got := deriveTaskThreadName(Task{QuickCreatePrompt: "create issue for billing sync"})
	if got != "create issue for billing sync" {
		t.Fatalf("thread name = %q, want quick-create prompt", got)
	}
}

func TestNormalizeThreadNameCollapsesWhitespaceAndTruncates(t *testing.T) {
	t.Parallel()

	input := "first line\n\t" + strings.Repeat("x", codexThreadNameMaxRunes+20)
	got := normalizeThreadName(input, codexThreadNameMaxRunes)
	if strings.ContainsAny(got, "\n\t") {
		t.Fatalf("thread name still contains raw whitespace: %q", got)
	}
	if len([]rune(got)) != codexThreadNameMaxRunes {
		t.Fatalf("thread name rune length = %d, want %d", len([]rune(got)), codexThreadNameMaxRunes)
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("thread name should end with ellipsis marker, got %q", got)
	}
}
