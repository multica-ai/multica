package cerebro

// CEREBRO-PATCH(skill-autolearn-test): TECH-3692 — unit tests for the
// auto-learn switch helpers. (Cerebro-zone file; marker added for parity.)

import "testing"

func TestParseSkillMetadataAutoLearnDefault(t *testing.T) {
	// No frontmatter at all → default on.
	if got := ParseSkillMetadata("# just a heading").AutoLearn; !got {
		t.Fatalf("AutoLearn default = %v, want true", got)
	}
	// Frontmatter without an auto_learn line → default on.
	content := "---\nname: x\ndomain: data\n---\nbody"
	if got := ParseSkillMetadata(content).AutoLearn; !got {
		t.Fatalf("AutoLearn (no line) = %v, want true", got)
	}
}

func TestParseSkillMetadataAutoLearnExplicit(t *testing.T) {
	off := "---\nname: x\nauto_learn: false\n---\nbody"
	if got := ParseSkillMetadata(off).AutoLearn; got {
		t.Fatalf("AutoLearn (explicit false) = %v, want false", got)
	}
	on := "---\nname: x\nauto_learn: true\n---\nbody"
	if got := ParseSkillMetadata(on).AutoLearn; !got {
		t.Fatalf("AutoLearn (explicit true) = %v, want true", got)
	}
}

func TestContentHasExplicitAutoLearn(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{"no frontmatter", "# heading\nbody", false},
		{"frontmatter without line", "---\nname: x\ndomain: data\n---\nbody", false},
		{"explicit true", "---\nname: x\nauto_learn: true\n---\nbody", true},
		{"explicit false", "---\nname: x\nauto_learn: false\n---\nbody", true},
		{"commented out", "---\nname: x\n# auto_learn: true\n---\nbody", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ContentHasExplicitAutoLearn(tc.content); got != tc.want {
				t.Fatalf("ContentHasExplicitAutoLearn = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIsAutoLearnEnabled(t *testing.T) {
	cases := []struct {
		name string
		raw  []byte
		want bool
	}{
		{"empty metadata defaults on", nil, true},
		{"explicit true", []byte(`{"auto_learn":true}`), true},
		{"explicit false", []byte(`{"auto_learn":false}`), false},
		{"malformed defaults on", []byte(`not json`), true},
		{"present but no auto_learn key", []byte(`{"domain":"data"}`), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsAutoLearnEnabled(tc.raw); got != tc.want {
				t.Fatalf("IsAutoLearnEnabled(%s) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}
