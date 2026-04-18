package util

import "testing"

func TestParseFreshDirective(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		input   string
		want    string
		isFresh bool
	}{
		{
			name:    "bare /fresh with message",
			input:   "/fresh do something",
			want:    "do something",
			isFresh: true,
		},
		{
			name:    "bare /fresh only",
			input:   "/fresh",
			want:    "",
			isFresh: true,
		},
		{
			name:    "mention then /fresh",
			input:   "[@Agent](mention://agent/550e8400-e29b-41d4-a716-446655440000) /fresh plan the next step",
			want:    "[@Agent](mention://agent/550e8400-e29b-41d4-a716-446655440000) plan the next step",
			isFresh: true,
		},
		{
			name:    "multiple mentions then /fresh",
			input:   "[@A](mention://agent/aaaa) [@B](mention://agent/bbbb) /fresh go",
			want:    "[@A](mention://agent/aaaa) [@B](mention://agent/bbbb) go",
			isFresh: true,
		},
		{
			name:    "mid-sentence /fresh is not a directive",
			input:   "please /fresh this",
			want:    "please /fresh this",
			isFresh: false,
		},
		{
			name:    "/freshwater is not a directive (word boundary)",
			input:   "/freshwater analysis",
			want:    "/freshwater analysis",
			isFresh: false,
		},
		{
			name:    "empty string",
			input:   "",
			want:    "",
			isFresh: false,
		},
		{
			name:    "leading whitespace then /fresh",
			input:   "  /fresh do it",
			want:    "do it",
			isFresh: true,
		},
		{
			name:    "mention with /fresh and newline",
			input:   "[@Bot](mention://agent/1234) /fresh\ndo the thing",
			want:    "[@Bot](mention://agent/1234)\ndo the thing",
			isFresh: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, isFresh := ParseFreshDirective(tc.input)
			if isFresh != tc.isFresh {
				t.Fatalf("ParseFreshDirective(%q): isFresh = %v, want %v", tc.input, isFresh, tc.isFresh)
			}
			if got != tc.want {
				t.Fatalf("ParseFreshDirective(%q): cleaned = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
