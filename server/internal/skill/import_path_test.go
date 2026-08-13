package skill

import "testing"

func TestCanonicalRuntimeLocalFilePath(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
		ok    bool
	}{
		{name: "Windows nested path", input: `templates\template.html`, want: "templates/template.html", ok: true},
		{name: "POSIX nested path", input: "references/example.md", want: "references/example.md", ok: true},
		{name: "empty", input: ""},
		{name: "NUL", input: "file\x00.txt"},
		{name: "POSIX absolute", input: "/absolute.txt"},
		{name: "Windows rooted", input: `\absolute.txt`},
		{name: "Windows drive absolute", input: `C:\absolute.txt`},
		{name: "Windows drive relative", input: `C:relative.txt`},
		{name: "UNC", input: `\\server\share\file.txt`},
		{name: "POSIX traversal", input: "../outside.txt"},
		{name: "Windows traversal", input: `..\outside.txt`},
		{name: "embedded traversal", input: `safe\..\outside.txt`},
		{name: "non-canonical separator", input: "safe//file.txt"},
		{name: "dot", input: "."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := CanonicalRuntimeLocalFilePath(tt.input)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("CanonicalRuntimeLocalFilePath(%q) = (%q, %v), want (%q, %v)", tt.input, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestCanonicalRuntimeLocalFilePathsRejectsCollision(t *testing.T) {
	_, err := CanonicalRuntimeLocalFilePaths([]string{`templates\template.html`, "templates/template.html"})
	if err == nil {
		t.Fatal("expected canonical path collision to fail")
	}
}
