package execenv

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// parseFrontmatter is a strict YAML parse of a SKILL.md frontmatter block, used
// by the tests to assert the writer always emits something a strict runtime
// (e.g. Codex) can load. It mirrors how those runtimes read the file: take the
// text between the leading `---` and the next `---`, and yaml.Unmarshal it.
func parseFrontmatter(t *testing.T, content string) map[string]any {
	t.Helper()
	if !strings.HasPrefix(content, "---\n") {
		t.Fatalf("content does not start with a frontmatter block:\n%s", content)
	}
	rest := content[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		t.Fatalf("frontmatter has no closing delimiter:\n%s", content)
	}
	var m map[string]any
	if err := yaml.Unmarshal([]byte(rest[:end]), &m); err != nil {
		t.Fatalf("frontmatter is not valid YAML: %v\nblock:\n%s", err, rest[:end])
	}
	return m
}

// TestEnsureSkillFrontmatterReSynthesizesInvalidYAML is the regression guard for
// the bug this change set fixes: a SKILL.md whose frontmatter has a `name` but
// is not valid YAML (an unquoted `: ` in the description is the canonical case)
// must be rewritten into a parseable block instead of being shipped as-is, or a
// strict runtime drops the whole skill on load.
func TestEnsureSkillFrontmatterReSynthesizesInvalidYAML(t *testing.T) {
	t.Parallel()

	const body = "# Heading\n\nReal skill body.\n"
	// `description: bad: value` is the exact failure mode from the issue: the
	// second `: ` makes YAML treat the tail as a nested mapping.
	broken := "---\nname: keep-me\ndescription: bad: value here\n---\n\n" + body

	got := ensureSkillFrontmatter(broken, "my-slug", "DB description: with a colon")

	fm := parseFrontmatter(t, got)
	if name, _ := fm["name"].(string); name != "my-slug" {
		t.Errorf("name = %#v, want %q", fm["name"], "my-slug")
	}
	if desc, _ := fm["description"].(string); desc != "DB description: with a colon" {
		t.Errorf("description = %#v, want the DB description verbatim", fm["description"])
	}
	if !strings.Contains(got, "Real skill body.") {
		t.Errorf("body was dropped during re-synthesis:\n%s", got)
	}
}

// When the existing frontmatter is invalid AND the DB description is empty, the
// writer still has to produce parseable YAML — just with name alone. (An empty
// description is dropped rather than emitted as `description: ""`.)
func TestEnsureSkillFrontmatterReSynthesizesInvalidYAMLWithEmptyDescription(t *testing.T) {
	t.Parallel()

	broken := "---\nname: keep-me\ndescription: bad: value\n---\n\nbody text\n"

	got := ensureSkillFrontmatter(broken, "my-slug", "")

	fm := parseFrontmatter(t, got)
	if name, _ := fm["name"].(string); name != "my-slug" {
		t.Errorf("name = %#v, want %q", fm["name"], "my-slug")
	}
	if _, present := fm["description"]; present {
		t.Errorf("description should be omitted when DB description is empty, got %#v", fm["description"])
	}
	if !strings.Contains(got, "body text") {
		t.Errorf("body was dropped during re-synthesis:\n%s", got)
	}
}

// Valid frontmatter that already carries a name must survive untouched —
// re-synthesis is only for the broken case, so upstream import formatting is
// preserved on the happy path.
func TestEnsureSkillFrontmatterLeavesValidYAMLUntouched(t *testing.T) {
	t.Parallel()

	valid := "---\nname: upstream\ndescription: \"colon: safe because quoted\"\nextra-key: kept\n---\n\nbody\n"

	if got := ensureSkillFrontmatter(valid, "my-slug", "ignored"); got != valid {
		t.Errorf("valid frontmatter was rewritten;\n got: %q\nwant: %q", got, valid)
	}
}

// frontmatterParts must agree on where a block ends regardless of how the
// closing delimiter is terminated. Before the shared helper, the validity check
// and the strip path used different close patterns (`\n---` vs `\n---\n`), so an
// EOF- or CRLF-terminated block was detected as present by one and missed by the
// other — leaving a stale block behind on re-synthesis.
func TestFrontmatterPartsClosingDelimiterVariants(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		content  string
		wantFM   string
		wantBody string
		wantOK   bool
	}{
		{
			name:     "newline terminated with blank line",
			content:  "---\nname: x\n---\n\nbody",
			wantFM:   "name: x",
			wantBody: "\nbody",
			wantOK:   true,
		},
		{
			name:     "closing delimiter at EOF",
			content:  "---\nname: x\n---",
			wantFM:   "name: x",
			wantBody: "",
			wantOK:   true,
		},
		{
			name:     "crlf terminated",
			content:  "---\r\nname: x\r\n---\r\nbody",
			wantFM:   "name: x\r",
			wantBody: "body",
			wantOK:   true,
		},
		{
			name:     "horizontal rule is not a delimiter",
			content:  "---\nname: x\n----\n---\nbody",
			wantFM:   "name: x\n----",
			wantBody: "body",
			wantOK:   true,
		},
		{
			name:     "no closing delimiter keeps full content as body",
			content:  "---\nname: x\nbody without close",
			wantFM:   "",
			wantBody: "---\nname: x\nbody without close",
			wantOK:   false,
		},
		{
			name:     "no opening delimiter",
			content:  "no frontmatter here",
			wantFM:   "",
			wantBody: "no frontmatter here",
			wantOK:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fm, body, ok := frontmatterParts(tc.content)
			if ok != tc.wantOK || fm != tc.wantFM || body != tc.wantBody {
				t.Errorf("frontmatterParts(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tc.content, fm, body, ok, tc.wantFM, tc.wantBody, tc.wantOK)
			}
		})
	}
}
