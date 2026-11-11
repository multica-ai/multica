package chattitle

import (
	"regexp"
	"strings"
	"unicode"
)

const deterministicTitleLimit = 30

var (
	markdownFence = regexp.MustCompile("```.*?```")
	markdownMarks = regexp.MustCompile("[#*`>~_]")
	markdownLink  = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
	titleSpace    = regexp.MustCompile(`[[:space:]]+`)
	// A line of nothing but mention links, such as the page a message was
	// sent from, which clients put ahead of what the user wrote.
	mentionsOnly = regexp.MustCompile(`^(?:\[(?:\\.|[^\]\\])*\]\(mention://[^)]*\)\s*)+$`)
)

// Derive is the single deterministic title policy for a Chat's first message,
// shared by first-party clients (which create sessions untitled) and channels:
// first non-empty line that is not only mentions (or the first non-empty line
// when every line is), Markdown stripped, whitespace collapsed, capped at 30
// code points with a single ellipsis.
func Derive(body string) string {
	line := ""
	for _, candidate := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(candidate)
		if trimmed == "" {
			continue
		}
		if line == "" {
			line = candidate
		}
		if !mentionsOnly.MatchString(trimmed) {
			line = candidate
			break
		}
	}
	line = markdownFence.ReplaceAllString(line, " ")
	line = markdownMarks.ReplaceAllString(line, "")
	line = markdownLink.ReplaceAllString(line, "$1")
	line = strings.TrimSpace(titleSpace.ReplaceAllString(line, " "))
	if line == "" {
		return ""
	}
	runes := []rune(line)
	if len(runes) <= deterministicTitleLimit {
		return line
	}
	return strings.TrimRightFunc(string(runes[:deterministicTitleLimit-1]), unicode.IsSpace) + "…"
}
