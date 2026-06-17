package cerebro

import (
	"strings"
	"testing"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

func TestBumpPatchVersion(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"1.0.0", "1.0.1"},
		{"0.2.9", "0.2.10"},
		{"2.15.3", "2.15.4"},
		{" 1.0.0 ", "1.0.1"},
		{"1.0", "0.0.1"},     // not 3-part → fallback
		{"1.0.x", "0.0.1"},   // non-numeric patch → fallback
		{"", "0.0.1"},        // empty → fallback
		{"garbage", "0.0.1"}, // unparseable → fallback
	}
	for _, c := range cases {
		if got := bumpPatchVersion(c.in); got != c.want {
			t.Errorf("bumpPatchVersion(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestAppendLearnedSuggestionIsAdditive(t *testing.T) {
	original := "# My Skill\n\nDo the thing.\n"
	row := cerebrodb.ListAggregatesAboveThresholdRow{
		DeltaType:  "skill_chain_addition",
		DeltaValue: "firtal-data-lookup",
	}
	got := appendLearnedSuggestion(original, row)

	// The original content must be preserved verbatim at the start — the
	// proposal can only add, never remove or rewrite.
	if !strings.HasPrefix(got, "# My Skill\n\nDo the thing.") {
		t.Fatalf("original content not preserved at start:\n%q", got)
	}
	if !strings.Contains(got, "[Auto-lært tilføjelse]") {
		t.Errorf("expected auto-learned section header in output:\n%q", got)
	}
	if !strings.Contains(got, "firtal-data-lookup") {
		t.Errorf("expected the observed delta value in the suggestion:\n%q", got)
	}
}
