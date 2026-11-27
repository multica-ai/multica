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

// A fenced code block can precede the sentence that names the chat. Its
// language tag is not a title, and neither is any code inside the fence.
func TestDeriveSkipsFencedCodeBlocks(t *testing.T) {
	tests := []struct{ input, want string }{
		{"```go\nfmt.Println(\"hi\")\n```\nWhat should I fix?", "What should I fix?"},
		{"```\nplain code\n```\nSecond line", "Second line"},
		{"```go\nfmt.Println(\"hi\")\n```", ""},
		{"Before the fence\n```go\ncode\n```", "Before the fence"},
		{"Use ```code``` here", "Use here"},
		{"```code``` here", "here"},
		{"How do I type ``` in markdown?\n```go\ncode\n```\nthanks", "How do I type in markdown?"},
		{"Why does ``` break?\nsee below\n```go\ncode\n```", "Why does break?"},
		{"  ~~~go\ncode\n  ~~~\nAfter the block", "After the block"},
		{"```go\ncode without a closing fence", ""},
		{"````md\n```go\ncode\n```\n`````\nAfter the outer fence", "After the outer fence"},
		{"```go\n~~~\n```not a closing fence\ncode\n```\nAfter the block", "After the block"},
		{"[DEV-43](mention://issue/issue-1)\n```go\ncode\n```\nWhy?", "Why?"},
	}
	for _, tc := range tests {
		if got := Derive(tc.input); got != tc.want {
			t.Errorf("Derive(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestDeriveFromFilenameKeepsFencePrefixedNames(t *testing.T) {
	for _, filename := range []string{"```report.md", "~~~report.md"} {
		if got := DeriveFromFilename(filename); got != "report.md" {
			t.Errorf("DeriveFromFilename(%q) = %q, want %q", filename, got, "report.md")
		}
	}
}
