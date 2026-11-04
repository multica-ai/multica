package skill

import "testing"

func TestIsSafeFilePath(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		want     bool
	}{
		{name: "file", filePath: "reference.md", want: true},
		{name: "nested file", filePath: "references/example.md", want: true},
		{name: "dot-prefixed file", filePath: "..notes.md", want: true},
		{name: "empty", filePath: "", want: false},
		{name: "NUL", filePath: "file\x00.txt", want: false},
		{name: "slash rooted", filePath: "/abs/file.txt", want: false},
		{name: "slash UNC", filePath: "//server/share/file.txt", want: false},
		{name: "relative backslash", filePath: `agents\openai.yaml`, want: false},
		{name: "backslash rooted", filePath: `\abs\file.txt`, want: false},
		{name: "Windows drive absolute with backslashes", filePath: `C:\abs\file.txt`, want: false},
		{name: "Windows drive absolute with slashes", filePath: "C:/abs/file.txt", want: false},
		{name: "Windows drive relative", filePath: `C:relative.txt`, want: false},
		{name: "backslash UNC", filePath: `\\server\share\file.txt`, want: false},
		{name: "parent traversal with slashes", filePath: "../escape.txt", want: false},
		{name: "parent traversal with backslashes", filePath: `..\escape.txt`, want: false},
		{name: "embedded traversal", filePath: "safe/../escape.txt", want: false},
		{name: "duplicate separators", filePath: "safe//file.txt", want: false},
		{name: "dot segment", filePath: "safe/./file.txt", want: false},
		{name: "dot", filePath: ".", want: false},
		{name: "trailing slash", filePath: "safe/", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsSafeFilePath(tt.filePath); got != tt.want {
				t.Fatalf("IsSafeFilePath(%q) = %v, want %v", tt.filePath, got, tt.want)
			}
		})
	}
}
