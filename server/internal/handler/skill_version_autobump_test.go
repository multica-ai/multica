package handler

import "testing"

// CEREBRO-PATCH(skill-version-autobump): tests for the direct-edit version bump.

func TestParseSkillFrontmatterVersion(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{"plain", "---\nname: x\nversion: 1.9.0\n---\nbody", "1.9.0"},
		{"quoted", "---\nversion: \"2.0.1\"\n---\n", "2.0.1"},
		{"single-quoted", "---\nversion: '0.3.0'\n---\n", "0.3.0"},
		{"absent", "---\nname: x\n---\nbody", ""},
		{"no-frontmatter", "just a body, no fences", ""},
		{"unterminated", "---\nversion: 1.0.0\nbody without closing fence", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseSkillFrontmatterVersion(c.content); got != c.want {
				t.Fatalf("parseSkillFrontmatterVersion(%q) = %q, want %q", c.content, got, c.want)
			}
		})
	}
}

func TestBumpPatch(t *testing.T) {
	cases := map[string]string{
		"1.0.0":   "1.0.1",
		"1.2.9":   "1.2.10",
		"0.0.0":   "0.0.1",
		"2.10.99": "2.10.100",
		"garbage": "1.0.1",
	}
	for in, want := range cases {
		if got := bumpPatch(in); got != want {
			t.Fatalf("bumpPatch(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNextSkillVersion(t *testing.T) {
	cases := []struct {
		name    string
		current string
		content string
		want    string
	}{
		// The reported bug: frontmatter is ahead of a stuck current_version.
		{"adopts-higher-frontmatter", "1.0.0", "---\nversion: 1.9.0\n---\n", "1.9.0"},
		// No frontmatter version -> patch bump so an edit is never invisible.
		{"patch-bump-when-no-frontmatter", "1.0.0", "---\nname: x\n---\nbody", "1.0.1"},
		// Frontmatter equal to current -> patch bump (author forgot to bump).
		{"patch-bump-when-equal", "1.2.3", "---\nversion: 1.2.3\n---\n", "1.2.4"},
		// Frontmatter lower than current -> ignore, patch bump (stay monotonic).
		{"patch-bump-when-lower", "2.0.0", "---\nversion: 1.0.0\n---\n", "2.0.1"},
		// Invalid frontmatter value -> patch bump from current.
		{"patch-bump-when-invalid-frontmatter", "1.0.0", "---\nversion: not-a-version\n---\n", "1.0.1"},
		// Legacy non-semver current with a valid frontmatter version -> adopt it.
		{"adopts-frontmatter-over-legacy-current", "draft", "---\nversion: 1.0.0\n---\n", "1.0.0"},
		// Legacy non-semver current, no frontmatter version -> seed a baseline.
		{"seed-baseline-for-legacy", "draft", "---\nname: x\n---\n", "1.0.1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := nextSkillVersion(c.current, c.content); got != c.want {
				t.Fatalf("nextSkillVersion(%q, ...) = %q, want %q", c.current, got, c.want)
			}
		})
	}
}
