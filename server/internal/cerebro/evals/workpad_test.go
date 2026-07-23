package evals

import "testing"

func TestHasWorkpad(t *testing.T) {
	cases := []struct {
		name string
		desc string
		want bool
	}{
		{"valid workpad with open items", "## Workpad\n\n- [ ] Stage 1\n- [ ] Stage 2\n\n---\n\nOriginal description.", true},
		{"valid workpad all checked", "## Workpad\n\n- [x] Stage 1\n- [x] Stage 2\n\n---\n\nDone.", true},
		{"mixed checked and unchecked", "## Workpad\n- [x] Done step\n- [ ] Next step\n---\nBody", true},
		{"heading case-insensitive", "## workpad\n- [ ] step\n---\nBody", true},
		{"no divider still valid", "## Workpad\n- [ ] only step", true},
		{"asterisk checklist", "## Workpad\n* [ ] step\n---\nBody", true},
		{"prose before checklist tolerated", "## Workpad\nPlan for this issue:\n- [ ] step\n---\nBody", true},
		{"empty description", "", false},
		{"no workpad section", "Just a normal description with\n- [ ] a checklist somewhere", false},
		{"heading not first", "Intro text first.\n\n## Workpad\n- [ ] step", false},
		{"empty checklist", "## Workpad\n\n---\n\nOriginal description.", false},
		{"checklist only below divider", "## Workpad\nSome prose\n---\n- [ ] hidden below divider", false},
		{"checkbox without content", "## Workpad\n- [ ] \n---\nBody", false},
		{"heading with trailing words", "## Workpad notes\n- [ ] step", false},
		{"crlf line endings", "## Workpad\r\n- [ ] step\r\n---\r\nBody", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, reason := HasWorkpad(tc.desc)
			if got != tc.want {
				t.Fatalf("HasWorkpad(%q) = %v (%s), want %v", tc.desc, got, reason, tc.want)
			}
		})
	}
}
