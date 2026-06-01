package inbox

import (
	"strings"
	"testing"
)

// FIR-2641 — when a comment-reminder is created without explicit text, the
// server suggests a one-line snippet derived from the comment body. The UI
// pre-fills the same suggestion and lets the user edit it before saving.

func TestSuggestReminderText_EmptyFallsBackToGeneric(t *testing.T) {
	for _, in := range []string{"", "   ", "\n\t  \n"} {
		if got := suggestReminderText(in); got != "Reminder about this comment" {
			t.Errorf("suggestReminderText(%q) = %q, want generic fallback", in, got)
		}
	}
}

func TestSuggestReminderText_CollapsesWhitespace(t *testing.T) {
	got := suggestReminderText("  Husk  at\nsvare\tTine   i morgen ")
	want := "Husk at svare Tine i morgen"
	if got != want {
		t.Errorf("suggestReminderText collapse = %q, want %q", got, want)
	}
}

func TestSuggestReminderText_TruncatesByRunesWithEllipsis(t *testing.T) {
	long := strings.Repeat("a", reminderTextMaxRunes+50)
	got := suggestReminderText(long)
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("expected ellipsis suffix, got %q", got)
	}
	// One ellipsis rune on top of the truncated body.
	if n := len([]rune(got)); n != reminderTextMaxRunes+1 {
		t.Errorf("truncated length = %d runes, want %d", n, reminderTextMaxRunes+1)
	}
}

func TestSuggestReminderText_CountsRunesNotBytes(t *testing.T) {
	// Multi-byte runes (æ = 2 bytes) must not be truncated early. A string of
	// exactly the rune budget stays intact, untruncated.
	exact := strings.Repeat("æ", reminderTextMaxRunes)
	got := suggestReminderText(exact)
	if strings.HasSuffix(got, "…") {
		t.Errorf("string at rune budget should not be truncated: %q", got)
	}
	if n := len([]rune(got)); n != reminderTextMaxRunes {
		t.Errorf("length = %d runes, want %d", n, reminderTextMaxRunes)
	}
}
