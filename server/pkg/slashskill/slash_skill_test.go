package slashskill

import "testing"

func TestExtract(t *testing.T) {
	t.Run("parses and deduplicates durable markers", func(t *testing.T) {
		refs := Extract(`please [/deploy\[prod\]](slash://skill/a) and [/again](slash://skill/a)`)
		if len(refs) != 1 || refs[0].ID != "a" || refs[0].Label != "deploy[prod]" {
			t.Fatalf("unexpected refs: %+v", refs)
		}
	})

	t.Run("ignores adjacent protocols", func(t *testing.T) {
		for _, markdown := range []string{
			"[/x](slash://action/y)",
			"[/x](slash://skills/y)",
			"[docs](https://example.com)",
		} {
			if refs := Extract(markdown); len(refs) != 0 {
				t.Fatalf("expected no refs for %q, got %+v", markdown, refs)
			}
		}
	})
}
