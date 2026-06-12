package lark

import (
	"regexp"
	"strings"
)

// markdownPatterns enumerate the syntax shapes we treat as evidence
// that the agent's reply is markdown rather than prose. Each pattern
// is intentionally conservative — better to false-positive (route
// plain text through the markdown card, which still renders fine)
// than to false-negative (leave `**bold**` characters visible in
// the user's transcript).
//
// Patterns are compiled once at package init; containsMarkdown is on
// the chat-reply hot path.
var markdownPatterns = []*regexp.Regexp{
	regexp.MustCompile("(?m)^#{1,6}[ \t]"),               // headings: # H1, ###### H6
	regexp.MustCompile("(?m)^[ \t]*[-*+][ \t]"),          // unordered list
	regexp.MustCompile("(?m)^[ \t]*\\d+\\.[ \t]"),        // ordered list
	regexp.MustCompile("(?m)^>[ \t]"),                    // blockquote
	regexp.MustCompile("(?m)^[ \t]*(?:---|\\*\\*\\*|___)[ \t]*$"), // hr / thematic break
	regexp.MustCompile("\\*\\*[^*\\n]+\\*\\*"),           // **bold**
	regexp.MustCompile("__[^_\\n]+__"),                   // __bold__
	regexp.MustCompile("(?m)^[ \t]*\\|.+\\|[ \t]*$"),     // table row (must have | on both ends)
	regexp.MustCompile("\\[[^\\]\\n]+\\]\\([^)\\n]+\\)"), // [text](url) link / image
}

// containsMarkdown returns true when the body almost certainly
// contains markdown syntax that Lark's plain-text `msg_type=text`
// renderer would leave un-rendered (showing raw asterisks, hashes,
// pipes, etc.). On true, the chat-reply router upgrades to the
// schema-2.0 interactive card path so the user sees formatted text.
//
// Fast-path tokens (backtick, asterisk, pipe, hash, leading dash on
// any line) are checked first; only on a hit do we run the slower
// regex pass. Empty strings short-circuit to false so an empty
// agent reply does not get wrapped in a card.
func containsMarkdown(s string) bool {
	if s == "" {
		return false
	}
	// Fenced code block — strong markdown signal, cheap substring check.
	if strings.Contains(s, "```") {
		return true
	}
	// Inline code: only count `…` runs that look paired and contain a
	// non-space char. Bare backticks (e.g. quoting a single keystroke
	// in prose) shouldn't trigger.
	if i := strings.Index(s, "`"); i >= 0 {
		if j := strings.Index(s[i+1:], "`"); j > 0 {
			return true
		}
	}
	for _, re := range markdownPatterns {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}
