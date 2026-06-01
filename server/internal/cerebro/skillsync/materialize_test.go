package skillsync

import (
	"strings"
	"testing"
)

func TestSkillSlug(t *testing.T) {
	cases := map[string]string{
		"Firtal Data":        "firtal-data",
		"  spaced  ":         "spaced",
		"UPPER_Case/Mix":     "upper-case-mix",
		"already-slug":       "already-slug",
		"!!!":                "skill",
		"firtal--data--auto": "firtal-data-auto",
	}
	for in, want := range cases {
		if got := skillSlug(in); got != want {
			t.Errorf("skillSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFrontmatterDomain(t *testing.T) {
	withDomain := "---\nname: x\ndomain: Data\ntags:\n  - a\n---\nbody"
	if got := frontmatterDomain(withDomain); got != "data" {
		t.Errorf("expected data, got %q", got)
	}
	if got := frontmatterDomain("no frontmatter here"); got != "" {
		t.Errorf("expected empty for no frontmatter, got %q", got)
	}
	if got := frontmatterDomain("---\nname: x\n---\nbody"); got != "" {
		t.Errorf("expected empty when no domain key, got %q", got)
	}
}

func TestEnsureFrontmatter(t *testing.T) {
	// No frontmatter: synthesise one with name + description.
	out := ensureFrontmatter("plain body", "my-skill", "does things")
	if !strings.HasPrefix(out, "---\nname: my-skill\n") {
		t.Errorf("expected synthesised frontmatter, got:\n%s", out)
	}
	if !strings.Contains(out, "plain body") {
		t.Errorf("body lost: %s", out)
	}

	// Existing frontmatter with a name: preserved verbatim (curated metadata).
	rich := "---\nname: Keep Me\nowner: jeh@firtal.com\ndomain: data\n---\nbody"
	if got := ensureFrontmatter(rich, "slug", "desc"); got != rich {
		t.Errorf("rich frontmatter mutated:\n%s", got)
	}

	// Frontmatter without a name: inject name as first key, keep the rest.
	noName := "---\ndescription: only desc\n---\nbody"
	got := ensureFrontmatter(noName, "the-slug", "desc")
	if !strings.HasPrefix(got, "---\nname: the-slug\ndescription: only desc\n") {
		t.Errorf("name not injected as first key:\n%s", got)
	}
}

func TestArchiveDomain(t *testing.T) {
	// Existing placement always wins.
	if d := archiveDomain("---\ndomain: data\n---\n", "marketing", "engineering"); d != "marketing" {
		t.Errorf("existing placement should win, got %q", d)
	}
	// Frontmatter domain used when known and not yet placed.
	if d := archiveDomain("---\ndomain: data\n---\n", "", "engineering"); d != "data" {
		t.Errorf("frontmatter domain should be used, got %q", d)
	}
	// Unknown frontmatter domain falls back to default.
	if d := archiveDomain("---\ndomain: nonsense\n---\n", "", "engineering"); d != "engineering" {
		t.Errorf("unknown domain should fall back to default, got %q", d)
	}
	// No frontmatter falls back to default.
	if d := archiveDomain("no fm", "", "engineering"); d != "engineering" {
		t.Errorf("no frontmatter should fall back to default, got %q", d)
	}
}
