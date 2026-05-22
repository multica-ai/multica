package main

import "testing"

func TestIsSkillsShBatchImportURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{"canonical two segment", "https://skills.sh/everyinc/compound-engineering-plugin", true},
		{"no scheme two segment", "skills.sh/everyinc/compound-engineering-plugin", true},
		{"http two segment", "http://skills.sh/owner/repo", true},
		{"trailing slash two segment", "https://skills.sh/owner/repo/", true},
		{"single skill three segment", "https://skills.sh/owner/repo/skill", false},
		{"no scheme single skill three segment", "skills.sh/owner/repo/skill", false},
		{"four segment path", "https://skills.sh/owner/repo/skill/extra", false},
		{"github two segment", "https://github.com/owner/repo", false},
		{"bare owner repo", "owner/repo", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSkillsShBatchImportURL(tt.raw); got != tt.want {
				t.Fatalf("isSkillsShBatchImportURL(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestIntValDefaultsMissingOrWrongShape(t *testing.T) {
	if got := intVal(map[string]any{"imported": float64(3)}, "imported"); got != 3 {
		t.Fatalf("intVal float64 = %d, want 3", got)
	}
	if got := intVal(map[string]any{}, "imported"); got != 0 {
		t.Fatalf("intVal missing = %d, want 0", got)
	}
	if got := intVal(map[string]any{"imported": "3"}, "imported"); got != 0 {
		t.Fatalf("intVal wrong type = %d, want 0", got)
	}
}
