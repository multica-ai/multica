package util

import (
	"regexp"
	"strings"
)

// Mention represents a parsed @mention from markdown content.
type Mention struct {
	Type string // "member", "agent", "issue", or "all"
	ID   string // user_id, agent_id, issue_id, or "all"
}

// MentionRe matches [@Label](mention://type/id) or [Label](mention://issue/id) in markdown.
// The @ prefix is optional to support issue mentions which use [MUL-123](mention://issue/...).
// Uses .+? (non-greedy) instead of [^\]]* so labels containing square brackets
// (e.g. "David[TF]") are matched correctly — the ](mention:// anchor is specific
// enough to prevent over-matching.
var MentionRe = regexp.MustCompile(`\[@?(.+?)\]\(mention://(member|agent|squad|issue|all)/([0-9a-fA-F-]+|all)\)`)

// IsMentionAll returns true if the mention is an @all mention.
func (m Mention) IsMentionAll() bool {
	return m.Type == "all"
}

// codeRanges returns the [start, end) byte ranges of content that sit inside
// markdown code: fenced code blocks (``` or ~~~, with info strings and fences
// longer than three characters) and inline code spans (including
// multi-backtick spans). An unterminated fence suppresses everything after it.
func codeRanges(content string) [][2]int {
	var ranges [][2]int

	lineStart := 0
	fenceChar := byte(0)
	fenceLen := 0
	for lineStart <= len(content) {
		lineEnd := lineStart
		for lineEnd < len(content) && content[lineEnd] != '\n' {
			lineEnd++
		}
		line := content[lineStart:lineEnd]

		if fenceChar != 0 {
			trimmed := strings.TrimLeft(line, " \t")
			if len(trimmed) >= fenceLen {
				closing := true
				for i := 0; i < fenceLen; i++ {
					if trimmed[i] != fenceChar {
						closing = false
						break
					}
				}
				if closing && strings.TrimLeft(trimmed[fenceLen:], " \t") == "" {
					ranges = append(ranges, [2]int{lineStart, lineEnd})
					fenceChar = 0
					fenceLen = 0
				}
			}
			if fenceChar != 0 {
				ranges = append(ranges, [2]int{lineStart, lineEnd})
			}
		} else if fc, fl, ok := openingFence(line); ok {
			fenceChar = fc
			fenceLen = fl
			ranges = append(ranges, [2]int{lineStart, lineEnd})
		} else {
			ranges = append(ranges, inlineCodeRanges(line, lineStart)...)
		}

		if lineEnd == len(content) {
			break
		}
		lineStart = lineEnd + 1
	}
	return ranges
}

// openingFence reports whether the line opens a fenced code block, returning
// the fence character and its length.
func openingFence(line string) (byte, int, bool) {
	indent := 0
	for indent < len(line) && indent < 3 && (line[indent] == ' ' || line[indent] == '\t') {
		indent++
	}
	rest := line[indent:]
	if len(rest) < 3 || (rest[0] != '`' && rest[0] != '~') {
		return 0, 0, false
	}
	n := 0
	for n < len(rest) && rest[n] == rest[0] {
		n++
	}
	if n < 3 {
		return 0, 0, false
	}
	return rest[0], n, true
}

// inlineCodeRanges returns the ranges of inline code spans on a single line.
func inlineCodeRanges(line string, offset int) [][2]int {
	var ranges [][2]int
	i := 0
	for i < len(line) {
		if line[i] != '`' {
			i++
			continue
		}
		n := 0
		for i+n < len(line) && line[i+n] == '`' {
			n++
		}
		closer := strings.Index(line[i+n:], strings.Repeat("`", n))
		if closer < 0 {
			i += n
			continue
		}
		end := i + n + closer + n
		ranges = append(ranges, [2]int{offset + i, offset + end})
		i = end
	}
	return ranges
}

func inRanges(pos int, ranges [][2]int) bool {
	for _, r := range ranges {
		if pos >= r[0] && pos < r[1] {
			return true
		}
	}
	return false
}

// ParseMentions extracts deduplicated mentions from markdown content.
// Mentions inside fenced code blocks or inline code spans are ignored — they
// document the mention syntax rather than trigger an agent run. Mentions in
// blockquotes are still returned on purpose.
func ParseMentions(content string) []Mention {
	matches := MentionRe.FindAllStringSubmatchIndex(content, -1)
	code := codeRanges(content)
	seen := make(map[string]bool)
	var result []Mention
	for _, m := range matches {
		if inRanges(m[0], code) {
			continue
		}
		key := content[m[4]:m[5]] + ":" + content[m[6]:m[7]]
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, Mention{Type: content[m[4]:m[5]], ID: content[m[6]:m[7]]})
	}
	return result
}

// HasMentionAll returns true if any mention in the slice is an @all mention.
func HasMentionAll(mentions []Mention) bool {
	for _, m := range mentions {
		if m.IsMentionAll() {
			return true
		}
	}
	return false
}
