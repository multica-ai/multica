package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExtractTitleAndBody pins the prompt-parsing rules for `multica run`
// when no explicit --title is given. Short single-line prompts become a
// bare title; long single-line prompts truncate the title AND preserve the
// full text in the body; multi-line prompts split first non-blank line as
// title with the full prompt as body; overrides win outright.
func TestExtractTitleAndBody(t *testing.T) {
	t.Parallel()

	longPrompt := "investigate if the stripe webhooks handle duplicated events or events about the same thing happening like when a checkout session is completed also a subscription event might send"

	tests := []struct {
		name      string
		prompt    string
		override  string
		wantTitle string
		wantBody  string
	}{
		{
			name:      "short single line: title only",
			prompt:    "fix the login bug",
			wantTitle: "fix the login bug",
			wantBody:  "",
		},
		{
			name:      "long single line: truncated title + full body",
			prompt:    longPrompt,
			// Title must end with an ellipsis and be under the cap.
			wantTitle: "",       // asserted via length/prefix below
			wantBody:  longPrompt, // full prompt preserved
		},
		{
			name:      "multi-line: first line as title, full prompt as body",
			prompt:    "refactor auth\n\nchange JWT to PASETO everywhere",
			wantTitle: "refactor auth",
			wantBody:  "refactor auth\n\nchange JWT to PASETO everywhere",
		},
		{
			name:      "leading blank lines are skipped",
			prompt:    "\n\nactual title\nwith body",
			wantTitle: "actual title",
			// Outer TrimSpace normalizes leading whitespace before storing.
			wantBody: "actual title\nwith body",
		},
		{
			name:      "override takes precedence; body is full prompt",
			prompt:    "anything\nhere",
			override:  "My Title",
			wantTitle: "My Title",
			wantBody:  "anything\nhere",
		},
		{
			name:      "empty prompt stays empty",
			prompt:    "",
			wantTitle: "",
			wantBody:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTitle, gotBody := extractTitleAndBody(tt.prompt, tt.override)
			// The long-prompt case checks title shape, not exact value.
			if tt.name == "long single line: truncated title + full body" {
				if len(gotTitle) > maxTitleLen+3 { // +3 bytes for UTF-8 "…"
					t.Errorf("title too long: %d chars, cap %d", len(gotTitle), maxTitleLen)
				}
				if !strings.HasSuffix(gotTitle, "…") {
					t.Errorf("title should end with ellipsis, got %q", gotTitle)
				}
				if !strings.HasPrefix(tt.prompt, strings.TrimSuffix(gotTitle, "…")) {
					t.Errorf("title should be a prefix of the prompt, got %q", gotTitle)
				}
			} else if gotTitle != tt.wantTitle {
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
	// maxTitleLen byte cap + up to 3 bytes for the UTF-8 "…".
	if len(got) > maxTitleLen+3 {
		t.Errorf("truncated title too long: %d chars, cap %d", len(got), maxTitleLen+3)
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
