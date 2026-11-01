package service

import "testing"

func TestMarkdownSection(t *testing.T) {
	doc := "## Summary\n\nS\n\n## Recommendation\n\nDo it.\nCarefully.\n\n## Options considered\n\n- a\n"
	if got := markdownSection(doc, "Recommendation"); got != "Do it.\nCarefully." {
		t.Fatalf("got %q", got)
	}
	if got := markdownSection("## Recommendation\n\nfirst", "Recommendation"); got != "first" {
		t.Fatalf("leading section: %q", got)
	}
	if got := markdownSection(doc, "Missing"); got != "" {
		t.Fatalf("got %q", got)
	}
}
