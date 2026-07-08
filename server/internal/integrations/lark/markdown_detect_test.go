package lark

import "testing"

// TestContainsMarkdown pins the heuristic the chat-reply router uses
// to decide between plain text (msg_type=text) and the schema-2.0
// markdown card. We deliberately bias toward false-positives — a
// short prose reply happening to contain a backtick or asterisk
// still renders fine inside the card. False-negatives are the
// expensive case: the user sees raw `**bold**` text.
func TestContainsMarkdown(t *testing.T) {
	t.Parallel()

	plain := []string{
		"",
		"Hello!",
		"sure, on it",
		"Hello, world. How are you?", // bare comma + period, no markdown
		"the build is green",
		"我已经创建了 issue MUL-42", // Chinese + dashed identifier (no `- ` line)
	}
	for _, s := range plain {
		if containsMarkdown(s) {
			t.Errorf("containsMarkdown(%q) = true; want false", s)
		}
	}

	markdown := []string{
		"# Heading",
		"## Second-level",
		"**bold** statement",
		"call __init__ then run",
		"- bullet one\n- bullet two",
		"1. first\n2. second",
		"> quoted line",
		"see [docs](https://example.com)",
		"run `make check` first",
		"```go\nfunc foo() {}\n```",
		"| col1 | col2 |\n|------|------|\n| a    | b    |",
		"---",
		"  - indented bullet",
		"plain prose\nthen a `inline code`\nthen more prose",
	}
	for _, s := range markdown {
		if !containsMarkdown(s) {
			t.Errorf("containsMarkdown(%q) = false; want true", s)
		}
	}
}
