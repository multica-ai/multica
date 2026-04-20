package agent

import "testing"

func TestNewReturnsKimiBackend(t *testing.T) {
	t.Parallel()
	b, err := New("kimi", Config{ExecutablePath: "/nonexistent/kimi"})
	if err != nil {
		t.Fatalf("New(kimi) error: %v", err)
	}
	if _, ok := b.(*kimiBackend); !ok {
		t.Fatalf("expected *kimiBackend, got %T", b)
	}
}

func TestKimiToolNameFromTitle(t *testing.T) {
	t.Parallel()
	tests := []struct {
		title string
		kind  string
		want  string
	}{
		{"Read file: /tmp/foo.go", "", "read_file"},
		{"read", "", "read_file"},
		{"Write: /tmp/bar.go", "", "write_file"},
		{"Edit", "", "edit_file"},
		{"Patch: /tmp/x", "", "edit_file"},
		{"Shell: ls -la", "", "terminal"},
		{"Bash", "", "terminal"},
		{"Run command: pwd", "", "terminal"},
		{"Search: foo", "", "search_files"},
		{"Glob: *.go", "", "glob"},
		{"Web search: golang acp", "", "web_search"},
		{"Fetch: https://example.com", "", "web_fetch"},
		{"Todo Write", "", "todo_write"},
		// Fallback: snake_case the title.
		{"Custom Thing", "", "custom_thing"},
		// Empty title falls back to kind.
		{"", "read", "read"},
	}
	for _, tt := range tests {
		got := kimiToolNameFromTitle(tt.title, tt.kind)
		if got != tt.want {
			t.Errorf("kimiToolNameFromTitle(%q, %q) = %q, want %q", tt.title, tt.kind, got, tt.want)
		}
	}
}
