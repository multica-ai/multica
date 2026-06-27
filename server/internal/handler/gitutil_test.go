package handler

import (
	"testing"
)

func TestExtractClosingIdentifiers(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"closes keyword", "Closes MUL-123", []string{"MUL-123"}},
		{"fixes keyword", "Fixes MUL-42", []string{"MUL-42"}},
		{"resolves keyword", "Resolves MUL-7", []string{"MUL-7"}},
		{"case insensitive", "closes mul-1", []string{"MUL-1"}},
		{"no adjacency", "Fix login MUL-1", []string{}},
		{"dedup", "Closes MUL-1\nCloses MUL-1", []string{"MUL-1"}},
		{"multiple", "Closes MUL-1 and Fixes MUL-2", []string{"MUL-1", "MUL-2"}},
		{"colon separator", "Closes: MUL-5", []string{"MUL-5"}},
		{"empty", "", []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractClosingIdentifiers(tc.input)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("index %d: got %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestExtractIdentifiers(t *testing.T) {
	got := extractIdentifiers("Fix MUL-1 and see MUL-2 or FOO-99")
	want := []string{"MUL-1", "MUL-2", "FOO-99"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("index %d: got %q, want %q", i, got[i], want[i])
		}
	}
}
