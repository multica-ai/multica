package agent

import (
	"math/rand"
	"regexp"
	"testing"
	"testing/quick"
)

func TestKiroToolNameFromTitle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		title string
		want  string
	}{
		// read file variants
		{"read", "read_file"},
		{"read file", "read_file"},
		{"Read file: /path/to/foo.go", "read_file"},

		// write file variants
		{"write", "write_file"},
		{"write file", "write_file"},
		{"Write file: /path/to/bar.go", "write_file"},

		// edit file variants
		{"edit", "edit_file"},
		{"patch", "edit_file"},

		// terminal variants
		{"shell", "terminal"},
		{"bash", "terminal"},
		{"terminal", "terminal"},
		{"run command", "terminal"},
		{"Run command: ls", "terminal"},

		// search variants
		{"search", "search_files"},
		{"grep", "search_files"},
		{"find", "search_files"},

		// glob
		{"glob", "glob"},

		// web search
		{"web search", "web_search"},

		// web fetch variants
		{"fetch", "web_fetch"},
		{"web fetch", "web_fetch"},

		// todo write variants
		{"todo", "todo_write"},
		{"todo write", "todo_write"},

		// empty input
		{"", ""},

		// whitespace only
		{"  ", ""},

		// fallback: snake_case the title
		{"Some Unknown Tool", "some_unknown_tool"},
	}

	for _, tt := range tests {
		got := kiroToolNameFromTitle(tt.title)
		if got != tt.want {
			t.Errorf("kiroToolNameFromTitle(%q) = %q, want %q", tt.title, got, tt.want)
		}
	}
}


// Feature: kiro-cli-runtime, Property 1: Tool name normalization produces valid identifiers
//
// For any non-empty string, kiroToolNameFromTitle returns a non-empty string
// matching ^[a-z][a-z0-9_]*$.
//
// **Validates: Requirements 3.10, 5.3**
func TestKiroToolNameFromTitle_ValidIdentifier(t *testing.T) {
	t.Parallel()

	validID := regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

	f := func(s string) bool {
		if s == "" {
			return true // property only applies to non-empty input
		}
		result := kiroToolNameFromTitle(s)
		if result == "" {
			return false
		}
		return validID.MatchString(result)
	}

	cfg := &quick.Config{
		MaxCount: 200,
		Rand:     rand.New(rand.NewSource(42)),
	}
	if err := quick.Check(f, cfg); err != nil {
		t.Errorf("Property 1 failed: %v", err)
	}
}

// Feature: kiro-cli-runtime, Property 2: Tool name normalization is idempotent
//
// For any string, kiroToolNameFromTitle(kiroToolNameFromTitle(x)) == kiroToolNameFromTitle(x).
//
// **Validates: Requirements 3.10, 5.3**
func TestKiroToolNameFromTitle_Idempotent(t *testing.T) {
	t.Parallel()

	f := func(s string) bool {
		once := kiroToolNameFromTitle(s)
		twice := kiroToolNameFromTitle(once)
		return once == twice
	}

	cfg := &quick.Config{
		MaxCount: 200,
		Rand:     rand.New(rand.NewSource(42)),
	}
	if err := quick.Check(f, cfg); err != nil {
		t.Errorf("Property 2 failed: %v", err)
	}
}
