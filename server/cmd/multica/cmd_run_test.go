package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExtractTitleAndBody covers the prompt-parsing rules for `multica run`
// when no explicit --title is given: single-line prompts become the title
// verbatim; multi-line prompts split first non-blank line as title, rest as
// description; overrides win; empty prompts stay empty.
func TestExtractTitleAndBody(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		prompt    string
		override  string
		wantTitle string
		wantBody  string
	}{
		{
			name:      "single line becomes title",
			prompt:    "fix the login bug",
			wantTitle: "fix the login bug",
			wantBody:  "",
		},
		{
			name:      "multi-line splits first line as title",
			prompt:    "refactor auth\n\nchange JWT to PASETO everywhere",
			wantTitle: "refactor auth",
			wantBody:  "change JWT to PASETO everywhere",
		},
		{
			name:      "blank leading lines are skipped",
			prompt:    "\n\nactual title\nwith body",
			wantTitle: "actual title",
			wantBody:  "with body",
		},
		{
			name:      "override takes precedence and body is full prompt",
			prompt:    "anything\nhere",
			override:  "My Title",
			wantTitle: "My Title",
			wantBody:  "anything\nhere",
		},
		{
			name:      "empty prompt empty title",
			prompt:    "",
			wantTitle: "",
			wantBody:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTitle, gotBody := extractTitleAndBody(tt.prompt, tt.override)
			if gotTitle != tt.wantTitle {
				t.Errorf("title: got %q, want %q", gotTitle, tt.wantTitle)
			}
			if gotBody != tt.wantBody {
				t.Errorf("body: got %q, want %q", gotBody, tt.wantBody)
			}
		})
	}
}

// TestTruncateTitle ensures very long single-line prompts never overflow the
// issue-title column. The cutoff should land on a word boundary when
// possible and append an ellipsis.
func TestTruncateTitle(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("word ", 40)
	got := truncateTitle(long)
	// 80 chars max + up to 3 bytes for the UTF-8 "…".
	if len(got) > 83 {
		t.Errorf("truncated title too long: %d chars", len(got))
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("expected ellipsis suffix, got %q", got)
	}

	// Short titles are untouched.
	if truncateTitle("tiny") != "tiny" {
		t.Errorf("short title should pass through")
	}
}

// TestNormalizePath verifies tilde expansion and absolute-path conversion.
// These are user-facing path semantics that must behave identically across
// shells, so we pin them in tests.
func TestNormalizePath(t *testing.T) {
	t.Parallel()
	home, _ := os.UserHomeDir()

	// Tilde expands to home.
	got, err := normalizePath("~/code/foo")
	if err != nil {
		t.Fatalf("normalizePath(~): %v", err)
	}
	if got != filepath.Join(home, "code", "foo") {
		t.Errorf("tilde didn't expand: got %q, want %q", got, filepath.Join(home, "code", "foo"))
	}

	// Relative path becomes absolute.
	cwd, _ := os.Getwd()
	got, err = normalizePath("./relative")
	if err != nil {
		t.Fatalf("normalizePath(relative): %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("expected absolute path, got %q", got)
	}
	if !strings.HasSuffix(got, filepath.Join(cwd, "relative")) {
		t.Errorf("relative path resolved wrong: got %q", got)
	}

	// Absolute path stays absolute.
	got, err = normalizePath("/var/tmp")
	if err != nil {
		t.Fatalf("normalizePath(abs): %v", err)
	}
	if got != "/var/tmp" {
		t.Errorf("absolute path changed: got %q", got)
	}
}

// TestFirstLine helps produce a good preview string for interactive menus;
// it must not panic on empty input and must strip whitespace.
func TestFirstLine(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"":              "",
		"  hello  ":     "hello",
		"first\nsecond": "first",
		"\n\nbody":      "body",
	}
	for in, want := range cases {
		if got := firstLine(in); got != want {
			t.Errorf("firstLine(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestIsGitRepo_False guards against false positives: empty temp dir should
// never be recognized as a git repo.
func TestIsGitRepo_False(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if isGitRepo(dir) {
		t.Errorf("empty temp dir should not be a git repo")
	}
}
