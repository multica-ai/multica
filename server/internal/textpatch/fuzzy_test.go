package textpatch

import (
	"errors"
	"testing"
)

func TestFuzzyReplace(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		oldText     string
		newText     string
		wantContent string
		wantErr     error
		wantStrat   string
	}{
		{
			name:        "exact match",
			content:     "hello world",
			oldText:     "hello",
			newText:     "goodbye",
			wantContent: "goodbye world",
			wantStrat:   "exact",
		},
		{
			name:        "exact match multiline",
			content:     "line1\nline2\nline3",
			oldText:     "line2",
			newText:     "replaced",
			wantContent: "line1\nreplaced\nline3",
			wantStrat:   "exact",
		},
		{
			name:        "whitespace tolerant - extra spaces",
			content:     "foo  bar  baz",
			oldText:     "foo bar baz",
			newText:     "replaced",
			wantContent: "replaced",
			wantStrat:   "whitespace_normalized",
		},
		{
			name:        "whitespace tolerant - tabs vs spaces",
			content:     "foo\tbar",
			oldText:     "foo bar",
			newText:     "replaced",
			wantContent: "replaced",
			wantStrat:   "whitespace_normalized",
		},
		{
			name:        "line trimmed - leading/trailing whitespace",
			content:     "  hello  \n  world  ",
			oldText:     "hello\nworld",
			newText:     "replaced",
			wantContent: "replaced",
			wantStrat:   "line_trimmed",
		},
		{
			name:        "indentation flexible - trailing content preserved",
			content:     "    def foo():  # comment\n        pass",
			oldText:     "def foo():  # comment\n    pass",
			newText:     "def bar():\n    return 1",
			wantContent: "def bar():\n    return 1",
			wantStrat:   "line_trimmed",
		},
		{
			name:    "not found",
			content: "hello world",
			oldText: "nonexistent",
			newText: "replacement",
			wantErr: ErrNotFound,
		},
		{
			name:    "ambiguous - multiple matches",
			content: "foo bar foo bar",
			oldText: "foo",
			newText: "baz",
			wantErr: ErrAmbiguous,
		},
		{
			name:    "empty search",
			content: "hello",
			oldText: "",
			newText: "world",
			wantErr: ErrEmptySearch,
		},
		{
			name:    "identical search and replace",
			content: "hello",
			oldText: "hello",
			newText: "hello",
			wantErr: ErrIdentical,
		},
		{
			name:        "exact preserves surrounding content",
			content:     "before\ntarget line\nafter",
			oldText:     "target line",
			newText:     "new line",
			wantContent: "before\nnew line\nafter",
			wantStrat:   "exact",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := FuzzyReplace(tt.content, tt.oldText, tt.newText)

			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error %v, got nil", tt.wantErr)
				}
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected error %v, got %v", tt.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Content != tt.wantContent {
				t.Errorf("content = %q, want %q", result.Content, tt.wantContent)
			}
			if result.Strategy != tt.wantStrat {
				t.Errorf("strategy = %q, want %q", result.Strategy, tt.wantStrat)
			}
		})
	}
}
