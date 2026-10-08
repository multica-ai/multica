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
//
// The name key's spelling must not change the route: `"name":`, `'name':`, and
// a valueless `name:` are the same top-level key as a bare `name: x`, so a
// block carrying any of them already has a name. Misreading them as nameless
// injects a second `name` above the malformed block — a duplicate mapping key,
// so the output stays unloadable instead of being healed (MUL-5529).
func TestEnsureSkillFrontmatterReSynthesizesInvalidYAML(t *testing.T) {
	t.Parallel()

	const body = "# Heading\n\nReal skill body.\n"
	cases := []struct {
		name     string
		nameLine string
	}{
		{name: "bare name key", nameLine: "name: keep-me"},
		{name: "double quoted name key", nameLine: `"name": keep-me`},
		{name: "single quoted name key", nameLine: `'name': keep-me`},
		{name: "name key with no value", nameLine: "name:"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// `description: bad: value here` is the exact failure mode from
			// the issue: the second `: ` makes YAML treat the tail as a
			// nested mapping.
			broken := "---\n" + tc.nameLine + "\ndescription: bad: value here\n---\n\n" + body
			if isFrontmatterValidYAML(broken) {
				t.Fatalf("fixture parses; it no longer exercises the malformed branch:\n%s", broken)
			}

			got := ensureSkillFrontmatter(broken, "my-slug", "DB description: with a colon")

			// parseFrontmatter fails the test on output a strict runtime
			// would reject — including the duplicate `name` this guards.
			fm := parseFrontmatter(t, got)
			if name, _ := fm["name"].(string); name != "my-slug" {
				t.Errorf("name = %#v, want %q\noutput:\n%s", fm["name"], "my-slug", got)
			}
			if desc, _ := fm["description"].(string); desc != "DB description: with a colon" {
				t.Errorf("description = %#v, want the DB description verbatim\noutput:\n%s", fm["description"], got)
			}
			if !strings.Contains(got, "Real skill body.") {
				t.Errorf("body was dropped during re-synthesis:\n%s", got)
			}
		})
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

// Valid frontmatter survives re-synthesis: every key keeps its upstream
// formatting and only `name` is retargeted at the slug. Re-synthesis proper is
// still reserved for the broken case.
//
// `name` cannot be left alone because runtimes disagree on which field
// identifies a skill (OpenCode: frontmatter `name`; Claude: directory name), so
// an upstream value that differs from the slug makes the skill answer to two
// names depending on the runtime (MUL-5529).
func TestEnsureSkillFrontmatterRetargetsNameAndKeepsOtherKeys(t *testing.T) {
	t.Parallel()

	valid := "---\nname: upstream\ndescription: \"colon: safe because quoted\"\nextra-key: kept\n---\n\nbody\n"
	want := "---\nname: my-slug\ndescription: \"colon: safe because quoted\"\nextra-key: kept\n---\n\nbody\n"

	if got := ensureSkillFrontmatter(valid, "my-slug", "ignored"); got != want {
		t.Errorf("frontmatter name was not retargeted;\n got: %q\nwant: %q", got, want)
	}
}

// A YAML value can span several lines. Replacing only the key's own line
// leaves the continuation behind, and YAML folds it into the new value:
// `name: >-\n  upstream` became "my-slug upstream", so the directory name and
// the frontmatter name diverged again — exactly the invariant this rewrite
// exists to hold.
//
// These assert on the *parsed* value rather than the output text, because the
// text's first line looked correct in every one of these cases.
func TestEnsureSkillFrontmatterReplacesMultiLineNameValues(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		content string
	}{
		{
			name:    "folded block scalar",
			content: "---\nname: >-\n  upstream-name\ndescription: kept\n---\n\nbody\n",
		},
		{
			name:    "literal block scalar",
			content: "---\nname: |-\n  upstream-name\ndescription: kept\n---\n\nbody\n",
		},
		{
			name:    "multi-line plain scalar",
			content: "---\nname: upstream\n  continued\ndescription: kept\n---\n\nbody\n",
		},
		{
			name:    "wrapped quoted scalar",
			content: "---\nname: \"upstream\n  continued\"\ndescription: kept\n---\n\nbody\n",
		},
		{
			name:    "value entirely on the next line",
			content: "---\nname:\n  upstream-name\ndescription: kept\n---\n\nbody\n",
		},
		// A quoted scalar may wrap onto a line at the SAME indentation as its
		// key, and so may a flow collection. Indentation therefore cannot bound
		// a YAML value: an indentation rule stops at the key's line and strands
		// the remainder, so the rewrite emitted invalid YAML rather than a
		// wrong-but-parseable name.
		{
			name:    "double quoted continued at same indent",
			content: "---\nname: \"upstream\ncontinued\"\ndescription: kept\n---\n\nbody\n",
		},
		{
			name:    "single quoted continued at same indent",
			content: "---\nname: 'upstream\ncontinued'\ndescription: kept\n---\n\nbody\n",
		},
		{
			name:    "flow sequence continued at same indent",
			content: "---\nname: [a,\nb]\ndescription: kept\n---\n\nbody\n",
		},
		{
			name:    "flow mapping continued at same indent",
			content: "---\nname: {a: 1,\nb: 2}\ndescription: kept\n---\n\nbody\n",
		},
		{
			name:    "name is the last key",
			content: "---\ndescription: kept\nname: \"upstream\ncontinued\"\n---\n\nbody\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Assert the *input* is valid YAML first. Otherwise a case could
			// pass for the wrong reason — ensureSkillFrontmatter re-synthesizes
			// malformed blocks, which would produce the right name without ever
			// exercising the surgical path these cases exist to cover.
			parseFrontmatter(t, tc.content)

			got := ensureSkillFrontmatter(tc.content, "my-slug", "")

			fm := parseFrontmatter(t, got)
			if name, _ := fm["name"].(string); name != "my-slug" {
				t.Errorf("parsed name = %#v, want %q\noutput:\n%s", fm["name"], "my-slug", got)
			}
			// Neighbouring keys still survive the surgery.
			if desc, _ := fm["description"].(string); desc != "kept" {
				t.Errorf("description = %#v, want %q\noutput:\n%s", fm["description"], "kept", got)
			}
			if !strings.Contains(got, "body") {
				t.Errorf("body was dropped:\n%s", got)
			}
		})
	}
}

// An anchor on the name value defeats the surgical rewrite: replacing the line
// removes `&skill_name`, so an alias pointing at it no longer resolves and the
// post-condition check correctly rejects the result. What must NOT happen next
// is a drop to bare re-synthesis, which emits only name and description.
//
// The stakes are higher than lost formatting. `disable-model-invocation: true`
// is the author's instruction that a runtime must not surface this skill on its
// own; if the written SKILL.md loses it, native discovery advertises a skill
// that was deliberately hidden — the opposite of the author's intent.
func TestEnsureSkillFrontmatterKeepsPolicyKeysWhenSurgicalRewriteFails(t *testing.T) {
	t.Parallel()

	in := "---\nname: &skill_name upstream\ndescription: *skill_name\ndisable-model-invocation: true\ncustom-key: kept\n---\n\nbody\n"

	// The input is valid YAML, so this exercises the fallback rather than the
	// invalid-YAML re-synthesis path.
	parseFrontmatter(t, in)

	got := ensureSkillFrontmatter(in, "my-slug", "")

	fm := parseFrontmatter(t, got)
	if name, _ := fm["name"].(string); name != "my-slug" {
		t.Errorf("name = %#v, want %q\noutput:\n%s", fm["name"], "my-slug", got)
	}
	if fm["disable-model-invocation"] != true {
		t.Errorf("disable-model-invocation was dropped (%#v); the written skill would be exposed to native discovery\noutput:\n%s", fm["disable-model-invocation"], got)
	}
	if custom, _ := fm["custom-key"].(string); custom != "kept" {
		t.Errorf("custom-key = %#v, want %q\noutput:\n%s", fm["custom-key"], "kept", got)
	}
	// The alias resolved to "upstream" before the rename and must still, rather
	// than following name to the slug. See
	// TestEnsureSkillFrontmatterKeepsAliasedValuesWhenNameIsRenamed for why
	// letting it follow is a policy bypass and not just a cosmetic difference.
	if desc, _ := fm["description"].(string); desc != "upstream" {
		t.Errorf("aliased description = %#v, want %q\noutput:\n%s", fm["description"], "upstream", got)
	}
	// The written file must still read as hidden to the visibility check that
	// gates whether a skill is advertised.
	if !skillDisablesModelInvocation(got) {
		t.Errorf("written skill no longer reads as disable-model-invocation:\n%s", got)
	}
	if !strings.Contains(got, "body") {
		t.Errorf("body was dropped:\n%s", got)
	}
}

// lookupPath resolves a dotted key path through nested frontmatter mappings,
// returning nil when any segment is missing.
func lookupPath(fm map[string]any, path string) any {
	var cur any = fm
	for _, seg := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur, ok = m[seg]
		if !ok {
			return nil
		}
	}
	return cur
}

// Keeping the anchor alive is not enough: a document can express another
// setting by *reusing* the name's value, and then renaming the anchored node
// silently rewrites that setting too. Here `disable-model-invocation` is an
// alias of a name whose value is "true", so binding the alias to the renamed
// node turns the skill visible again — the same exposure as dropping the key,
// reached by a different route.
//
// Preserving a key is therefore not the invariant. Preserving each key's
// resolved *value* is.
func TestEnsureSkillFrontmatterKeepsAliasedValuesWhenNameIsRenamed(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		content string
		// alsoAliased names further keys whose resolved value must still be
		// the anchor's original "true" — asserting the invariant on every
		// alias, not only the one that happens to gate visibility.
		alsoAliased []string
	}{
		{
			name:    "alias carries the policy value",
			content: "---\nname: &shared \"true\"\ndisable-model-invocation: *shared\ncustom-key: kept\n---\n\nbody\n",
		},
		{
			name:        "alias nested inside another mapping",
			content:     "---\nname: &shared \"true\"\nmeta:\n  inner: *shared\ndisable-model-invocation: *shared\n---\n\nbody\n",
			alsoAliased: []string{"meta.inner"},
		},
		{
			name:        "two aliases of the same anchor",
			content:     "---\nname: &shared \"true\"\ndisable-model-invocation: *shared\nother: *shared\n---\n\nbody\n",
			alsoAliased: []string{"other"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Valid YAML in, so this is the node-rebuild fallback rather than
			// the invalid-YAML re-synthesis path.
			parseFrontmatter(t, tc.content)

			// The skill starts out hidden from model invocation...
			if !skillDisablesModelInvocation(tc.content) {
				t.Fatalf("fixture is not hidden to begin with; the test proves nothing")
			}

			got := ensureSkillFrontmatter(tc.content, "my-slug", "")

			// ...and must still be hidden afterwards.
			if !skillDisablesModelInvocation(got) {
				t.Errorf("rewrite exposed a hidden skill\noutput:\n%s", got)
			}

			fm := parseFrontmatter(t, got)
			if name, _ := fm["name"].(string); name != "my-slug" {
				t.Errorf("name = %#v, want %q\noutput:\n%s", fm["name"], "my-slug", got)
			}
			if _, stillAliased := fm["disable-model-invocation"]; !stillAliased {
				t.Errorf("disable-model-invocation disappeared\noutput:\n%s", got)
			}
			// Every other alias of the same anchor must also hold its
			// pre-rename value, not just the one gating visibility.
			for _, path := range tc.alsoAliased {
				if v := lookupPath(fm, path); v != "true" {
					t.Errorf("aliased %s = %#v, want %q\noutput:\n%s", path, v, "true", got)
				}
			}
			if strings.Contains(got, "*shared") || strings.Contains(got, "&shared") {
				t.Errorf("anchor/alias survived instead of being materialized\noutput:\n%s", got)
			}
		})
	}
}

// Whether a name exists is a question about the parsed document, not about how
// the key happens to be spelled. A lexical scan only recognizes a bare `name:`
// with a value on the same line, so a quoted key or an empty value read as
// "nameless" and got a second `name` injected above them.
//
// The result is a duplicate mapping key, which strict loaders reject outright:
// the skill does not merely carry the wrong name, it fails to load at all.
func TestEnsureSkillFrontmatterRecognizesQuotedAndEmptyNameKeys(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		content string
	}{
		{
			name:    "double quoted key",
			content: "---\n\"name\": upstream\ndisable-model-invocation: true\ncustom-key: kept\n---\n\nbody\n",
		},
		{
			name:    "single quoted key",
			content: "---\n'name': upstream\ndisable-model-invocation: true\ncustom-key: kept\n---\n\nbody\n",
		},
		{
			name:    "key present with no value",
			content: "---\nname:\ndisable-model-invocation: true\ncustom-key: kept\n---\n\nbody\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			parseFrontmatter(t, tc.content)

			got := ensureSkillFrontmatter(tc.content, "my-slug", "")

			// parseFrontmatter fails the test on a duplicate `name`, which is
			// the exact symptom this guards.
			fm := parseFrontmatter(t, got)
			if name, _ := fm["name"].(string); name != "my-slug" {
				t.Errorf("name = %#v, want %q\noutput:\n%s", fm["name"], "my-slug", got)
			}
			if fm["disable-model-invocation"] != true {
				t.Errorf("disable-model-invocation = %#v, want true\noutput:\n%s", fm["disable-model-invocation"], got)
			}
			if custom, _ := fm["custom-key"].(string); custom != "kept" {
				t.Errorf("custom-key = %#v, want %q\noutput:\n%s", fm["custom-key"], "kept", got)
			}
			if !skillDisablesModelInvocation(got) {
				t.Errorf("written skill no longer reads as hidden:\n%s", got)
			}
		})
	}
}

// Comments and blank lines between the name entry and the next key are not part
// of the value, so the rewrite must step over them rather than absorb them into
// the replaced span.
func TestEnsureSkillFrontmatterKeepsCommentsAroundName(t *testing.T) {
	t.Parallel()

	in := "---\nname: >-\n  upstream\n\n# keep this comment\ndescription: kept\n---\n\nbody\n"
	want := "---\nname: my-slug\n\n# keep this comment\ndescription: kept\n---\n\nbody\n"

	if got := ensureSkillFrontmatter(in, "my-slug", ""); got != want {
		t.Errorf("comment or spacing was swallowed;\n got: %q\nwant: %q", got, want)
	}
}

// A block scalar may legitimately contain a line that looks like a comment.
// Because block scalar content is always indented, the "step over comments"
// rule must only skip *unindented* ones — otherwise this content is left
// stranded outside the replaced span.
func TestEnsureSkillFrontmatterHandlesHashInsideBlockScalarName(t *testing.T) {
	t.Parallel()

	in := "---\nname: |-\n  upstream\n  # not a comment\ndescription: kept\n---\n\nbody\n"

	got := ensureSkillFrontmatter(in, "my-slug", "")

	fm := parseFrontmatter(t, got)
	if name, _ := fm["name"].(string); name != "my-slug" {
		t.Errorf("parsed name = %#v, want %q\noutput:\n%s", fm["name"], "my-slug", got)
	}
	if strings.Contains(got, "# not a comment") {
		t.Errorf("block scalar content was stranded outside the replaced span:\n%s", got)
	}
}

// The rewrite is byte-surgical: a CRLF block keeps its line endings instead of
// acquiring a lone LF on the name line.
func TestEnsureSkillFrontmatterPreservesCRLFOnNameRewrite(t *testing.T) {
	t.Parallel()

	valid := "---\r\nname: upstream\r\ndescription: kept\r\n---\r\n\r\nbody\r\n"
	want := "---\r\nname: my-slug\r\ndescription: kept\r\n---\r\n\r\nbody\r\n"

	if got := ensureSkillFrontmatter(valid, "my-slug", "ignored"); got != want {
		t.Errorf("CRLF frontmatter mangled;\n got: %q\nwant: %q", got, want)
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
