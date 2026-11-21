package chattitle

import "testing"

// The page a message is sent from goes ahead of it as a line of mention
// links. Taken as the first line, that link was the whole title ("DEV-43").
func TestDeriveSkipsALineOfMentions(t *testing.T) {
	tests := []struct{ input, want string }{
		{"[DEV-43](mention://issue/issue-1)\n\nWhy is this failing?", "Why is this failing?"},
		{"[Q3 \\[beta\\]](mention://project/p-1) [DEV-43](mention://issue/issue-1)\nCompare these", "Compare these"},
		// A message of only mentions is named after them.
		{"[DEV-43](mention://issue/issue-1)", "DEV-43"},
		// A line that says something beside its mention stays the title.
		{"[DEV-43](mention://issue/issue-1) is blocked\nwhy?", "DEV-43 is blocked"},
		// Ordinary links are not mentions.
		{"[docs](https://example.com)\nsecond line", "docs"},
	}
	for _, tc := range tests {
		if got := Derive(tc.input); got != tc.want {
			t.Errorf("Derive(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
