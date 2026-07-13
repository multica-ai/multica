package daemon

import "testing"

func TestExtractSlashSkills(t *testing.T) {
	t.Run("parses basic link", func(t *testing.T) {
		refs := ExtractSlashSkills("please [/deploy](slash://skill/abc-123) this")
		if len(refs) != 1 {
			t.Fatalf("expected 1 ref, got %d", len(refs))
		}
		if refs[0].Label != "deploy" || refs[0].ID != "abc-123" {
			t.Fatalf("unexpected ref: %+v", refs[0])
		}
	})

	t.Run("parses escaped brackets", func(t *testing.T) {
		refs := ExtractSlashSkills(`[/deploy\[prod\]](slash://skill/x)`)
		if len(refs) != 1 || refs[0].Label != "deploy[prod]" {
			t.Fatalf("unexpected refs: %+v", refs)
		}
	})

	t.Run("deduplicates by ID", func(t *testing.T) {
		refs := ExtractSlashSkills("[/a](slash://skill/same) and [/b](slash://skill/same)")
		if len(refs) != 1 {
			t.Fatalf("expected 1 ref after dedupe, got %d", len(refs))
		}
	})

	t.Run("ignores slash action links", func(t *testing.T) {
		refs := ExtractSlashSkills("[/x](slash://action/y)")
		if len(refs) != 0 {
			t.Fatalf("expected 0 refs for action link, got %d", len(refs))
		}
	})

	t.Run("ignores normal markdown links", func(t *testing.T) {
		refs := ExtractSlashSkills("[docs](https://example.com)")
		if len(refs) != 0 {
			t.Fatalf("expected 0 refs for normal link, got %d", len(refs))
		}
	})

	t.Run("ignores mention links", func(t *testing.T) {
		refs := ExtractSlashSkills("[@user](mention://member/id)")
		if len(refs) != 0 {
			t.Fatalf("expected 0 refs for mention link, got %d", len(refs))
		}
	})

	t.Run("extracts multiple distinct skills", func(t *testing.T) {
		refs := ExtractSlashSkills("[/a](slash://skill/id-1) and [/b](slash://skill/id-2)")
		if len(refs) != 2 {
			t.Fatalf("expected 2 refs, got %d", len(refs))
		}
	})
}

func TestExtractSlashSkillsDoesNotMatchPartialProtocol(t *testing.T) {
	for _, md := range []string{
		"[/x](slash://y)",
		"[/x](slash://skills/y)",
		"[/x](slash://skill-extra/y)",
	} {
		if refs := ExtractSlashSkills(md); len(refs) != 0 {
			t.Errorf("expected 0 refs for %q, got %d", md, len(refs))
		}
	}
}
