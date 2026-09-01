package handler

import (
	"runtime"
	"testing"
)

// TestValidateFilePath covers the path-safety helper that every skill file
// endpoint shares. Cross-platform cases (Windows-style absolute paths and
// backslash-separated traversal) only hold on the platform whose filepath
// semantics recognize them, so each is skipped where it does not apply.
func TestValidateFilePath(t *testing.T) {
	isWindows := runtime.GOOS == "windows"

	cases := []struct {
		name    string
		path    string
		want    bool
		windows bool // when true, only assert want on Windows; skip elsewhere
	}{
		{"empty", "", false, false},
		{"simple file", "a.md", true, false},
		{"nested path", "sub/guide.md", true, false},
		{"posix traversal", "../evil.md", false, false},
		{"posix absolute", "/etc/passwd", false, false},
		{"posix traversal deep", "../../etc/passwd", false, false},
		{"traversal looking only", "..", false, false},
		{"windows traversal", `..\evil.md`, false, false},
		{"windows-led backslash", `\evil.md`, false, false},
		{"windows absolute", `C:\windows\system32`, false, true},
		{"unc share", `\\server\share\x.md`, false, true},
		{"relative clean", "docs/../a.md", true, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.windows && !isWindows {
				t.Skip("Windows path semantics only")
			}
			if got := validateFilePath(tc.path); got != tc.want {
				t.Errorf("validateFilePath(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}
