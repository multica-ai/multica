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
)

// Derive is the single deterministic title policy for a Chat's first message,
// shared by first-party clients (which create sessions untitled) and channels:
// first non-empty line, Markdown stripped, whitespace collapsed, capped at 30
// code points with a single ellipsis.
func Derive(body string) string {
	line := ""
	for _, candidate := range strings.Split(body, "\n") {
		if strings.TrimSpace(candidate) != "" {
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
