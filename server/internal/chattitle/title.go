package chattitle

import (
	"regexp"
	"strings"
	"unicode"
)

const deterministicTitleLimit = 30

var (
	fencePrefix   = regexp.MustCompile("^(?:`{3,}|~{3,})")
	inlineFence   = regexp.MustCompile("```.*?```")
	markdownMarks = regexp.MustCompile("[#*`>~_]")
	markdownLink  = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
	titleSpace    = regexp.MustCompile(`[[:space:]]+`)
	// A line of nothing but mention links, such as the page a message was
	// sent from, which clients put ahead of what the user wrote.
	mentionsOnly = regexp.MustCompile(`^(?:\[(?:\\.|[^\]\\])*\]\(mention://[^)]*\)\s*)+$`)
)

// Derive is the single deterministic title policy for a Chat's first message,
// shared by first-party clients (which create sessions untitled) and channels:
// first non-empty line outside fenced code, preferring lines with text beyond
// mentions. The title omits Markdown, collapses whitespace, and is capped at
// 30 code points with a single ellipsis.
func Derive(body string) string {
	return derive(body, true)
}

// DeriveFromFilename applies the title cleanup to an attachment filename.
func DeriveFromFilename(filename string) string {
	return derive(filename, false)
}

func derive(body string, skipBlockFences bool) string {
	line := ""
	openFence := ""
	for _, candidate := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(candidate)
		if trimmed == "" {
			continue
		}
		if skipBlockFences {
			fence := fencePrefix.FindString(trimmed)
			if openFence != "" {
				if fence == trimmed && strings.HasPrefix(fence, openFence) {
					openFence = ""
				}
				continue
			}
			// An opening backtick fence requires a backtick-free info string.
			if fence != "" && (fence[0] == '~' || !strings.ContainsRune(trimmed[len(fence):], '`')) {
				openFence = fence
				continue
			}
		}
		if line == "" {
			line = candidate
		}
		if !mentionsOnly.MatchString(trimmed) {
			line = candidate
			break
		}
	}
	line = inlineFence.ReplaceAllString(line, " ")
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
