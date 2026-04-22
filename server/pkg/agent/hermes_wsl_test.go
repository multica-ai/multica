package agent

import "testing"

func TestWindowsToWSLPath(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "drive letter path",
			input: `C:\Users\foo\project`,
			want:  "/mnt/c/Users/foo/project",
		},
		{
			name:  "drive root",
			input: `C:\`,
			want:  "/mnt/c/",
		},
		{
			name:  "lowercase drive letter",
			input: `d:\work\repo`,
			want:  "/mnt/d/work/repo",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "already posix path",
			input: "/mnt/c/Users/foo",
			want:  "/mnt/c/Users/foo",
		},
		{
			name:  "relative path",
			input: "relative/path",
			want:  "relative/path",
		},
		{
			name:  "dot path",
			input: ".",
			want:  ".",
		},
		{
			name:  "drive letter with forward slashes",
			input: `E:\some/mixed\path`,
			want:  "/mnt/e/some/mixed/path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := windowsToWSLPath(tt.input)
			if got != tt.want {
				t.Errorf("windowsToWSLPath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
