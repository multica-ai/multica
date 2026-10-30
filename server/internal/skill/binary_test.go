package skill

import "testing"

func TestIsLikelyBinaryFilePath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		// The Office formats runtime-local skill bundles actually carry.
		{"references/report.docx", true},
		{"references/data.xlsx", true},
		{"deck.pptx", true},
		{"legacy.doc", true},
		{"spec.pdf", true},
		// Other non-text payloads.
		{"assets/logo.png", true},
		{"fonts/Inter.woff2", true},
		{"bundle.zip", true},
		{"clip.mp4", true},
		{"helper.so", true},
		{"cache.sqlite3", true},
		// Extension matching is case-insensitive: Windows skill dirs routinely
		// carry upper-cased extensions.
		{"references/REPORT.DOCX", true},
		{"Assets/Logo.PNG", true},
		// Text payloads must pass through — skipping these would silently drop
		// the reference material a skill exists to provide.
		{"SKILL.md", false},
		{"references/notes.md", false},
		{"scripts/run.sh", false},
		{"config.json", false},
		{"data.csv", false},
		{"template.html", false},
		{"main.py", false},
		// No extension, and a dotfile whose name is not an extension.
		{"Makefile", false},
		{".gitignore", false},
		{"", false},
		// A directory-looking path with a binary extension is still binary.
		{"nested/dir/archive.tar", true},
	}

	for _, tt := range tests {
		if got := IsLikelyBinaryFilePath(tt.path); got != tt.want {
			t.Errorf("IsLikelyBinaryFilePath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}
