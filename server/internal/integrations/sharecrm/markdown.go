package sharecrm

import (
	"regexp"
	"strings"
)

// stripMarkdown reduces common Markdown to plain text for ShareCRM, which
// currently delivers outbound messages as plain text only (Hermes adapter
// parity). Go's RE2 engine has no lookbehind; patterns stay simple.
var (
	reBoldStars   = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	reBoldUnder   = regexp.MustCompile(`__([^_]+)__`)
	reItalicStars = regexp.MustCompile(`\*([^*]+)\*`)
	reItalicUnder = regexp.MustCompile(`_([^_]+)_`)
	reInlineCode  = regexp.MustCompile("`([^`]+)`")
	reFence       = regexp.MustCompile("(?s)```[a-zA-Z0-9]*\\n?")
	reImage       = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)
	reLink        = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	reHeading     = regexp.MustCompile(`(?m)^#{1,6}\s+`)
	reHR          = regexp.MustCompile(`(?m)^[-*_]{3,}\s*$`)
)

func stripMarkdown(text string) string {
	if text == "" {
		return text
	}
	s := text
	s = reBoldStars.ReplaceAllString(s, "$1")
	s = reBoldUnder.ReplaceAllString(s, "$1")
	s = reItalicStars.ReplaceAllString(s, "$1")
	s = reItalicUnder.ReplaceAllString(s, "$1")
	s = reInlineCode.ReplaceAllString(s, "$1")
	s = reFence.ReplaceAllString(s, "")
	s = reImage.ReplaceAllString(s, "$2")
	s = reLink.ReplaceAllString(s, "$1 ($2)")
	s = reHeading.ReplaceAllString(s, "")
	s = reHR.ReplaceAllString(s, "---")
	return strings.TrimSpace(s)
}
