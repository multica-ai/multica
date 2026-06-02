package skill

import "testing"

func TestIsReservedContentPath(t *testing.T) {
	reserved := []string{
		"SKILL.md",        // canonical
		"skill.md",        // case-insensitive
		"SKILL.MD",        // case-insensitive
		"./SKILL.md",      // non-canonical, resolves to SKILL.md
		"foo/../SKILL.md", // non-canonical, resolves to SKILL.md
	}
	for _, p := range reserved {
		if !IsReservedContentPath(p) {
			t.Errorf("IsReservedContentPath(%q) = false, want true", p)
		}
	}

	notReserved := []string{
		"README.md",
		"docs/SKILL.md", // genuine nested file, will not collide with the primary
		"skills/SKILL.md",
		"SKILL.md.bak",
		"",
	}
	for _, p := range notReserved {
		if IsReservedContentPath(p) {
			t.Errorf("IsReservedContentPath(%q) = true, want false", p)
		}
	}
}
