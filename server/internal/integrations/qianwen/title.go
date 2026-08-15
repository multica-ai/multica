package qianwen

import (
	"regexp"
	"strings"
)

const qianwenSessionTitleMaxRunes = 30

var qianwenMarkdownLinkPattern = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)

// deriveQianwenSessionTitle mirrors Multica's deterministic new-chat fallback:
// use the first meaningful line, remove common Markdown decoration, collapse
// whitespace, and cap by Unicode code points. The runtime may later generate a
// semantic title through the normal chat UI path, but command submission never
// waits on an LLM merely to create a task.
func deriveQianwenSessionTitle(query string) string {
	firstLine := ""
	for _, line := range strings.Split(query, "\n") {
		if strings.TrimSpace(line) != "" {
			firstLine = strings.TrimSpace(line)
			break
		}
	}
	if firstLine == "" {
		return ""
	}
	cleaned := qianwenMarkdownLinkPattern.ReplaceAllString(firstLine, "$1")
	cleaned = strings.Map(func(r rune) rune {
		switch r {
		case '#', '*', '`', '>', '~', '_':
			return -1
		default:
			return r
		}
	}, cleaned)
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	if cleaned == "" {
		return ""
	}
	runes := []rune(cleaned)
	if len(runes) <= qianwenSessionTitleMaxRunes {
		return cleaned
	}
	return strings.TrimSpace(string(runes[:qianwenSessionTitleMaxRunes-1])) + "…"
}
