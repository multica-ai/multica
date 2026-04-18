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
var MentionRe = regexp.MustCompile(`\[@?[^\]]*\]\(mention://(member|agent|issue|all)/([0-9a-fA-F-]+|all)\)`)

// IsMentionAll returns true if the mention is an @all mention.
func (m Mention) IsMentionAll() bool {
	return m.Type == "all"
}

// ParseMentions extracts deduplicated mentions from markdown content.
func ParseMentions(content string) []Mention {
	matches := MentionRe.FindAllStringSubmatch(content, -1)
	seen := make(map[string]bool)
	var result []Mention
	for _, m := range matches {
		key := m[1] + ":" + m[2]
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, Mention{Type: m[1], ID: m[2]})
	}
	return result
}

// ParseFreshDirective checks whether the comment starts with /fresh (optionally
// preceded by mention links). If found, it returns the cleaned content with
// /fresh stripped and isFresh=true. The directive only triggers at the very
// start of the content so that /fresh inside code blocks or mid-sentence is
// ignored.
func ParseFreshDirective(content string) (cleaned string, isFresh bool) {
	s := content
	pos := 0 // byte offset into content consumed by leading mentions

	// Skip past leading mention links.
	for {
		// Trim whitespace between mentions.
		trimmed := 0
		for pos+trimmed < len(s) && (s[pos+trimmed] == ' ' || s[pos+trimmed] == '\t' || s[pos+trimmed] == '\n' || s[pos+trimmed] == '\r') {
			trimmed++
		}
		rest := s[pos+trimmed:]
		loc := MentionRe.FindStringIndex(rest)
		if loc == nil || loc[0] != 0 {
			pos += trimmed
			break
		}
		pos += trimmed + loc[1]
	}

	rest := s[pos:]
	// Trim whitespace between last mention and /fresh.
	trimmedRest := rest
	for len(trimmedRest) > 0 && (trimmedRest[0] == ' ' || trimmedRest[0] == '\t') {
		trimmedRest = trimmedRest[1:]
	}
	if !strings.HasPrefix(trimmedRest, "/fresh") {
		return content, false
	}
	after := trimmedRest[len("/fresh"):]
	if len(after) > 0 && after[0] != ' ' && after[0] != '\n' && after[0] != '\r' && after[0] != '\t' {
		return content, false
	}

	// Strip /fresh from the original content, collapsing surrounding whitespace.
	freshStart := len(s) - len(trimmedRest)
	freshEnd := freshStart + len("/fresh")
	// Consume one trailing space if present.
	if freshEnd < len(s) && s[freshEnd] == ' ' {
		freshEnd++
	}
	prefix := strings.TrimRight(s[:freshStart], " \t")
	suffix := s[freshEnd:]
	if prefix == "" {
		cleaned = suffix
	} else if len(suffix) > 0 && suffix[0] != '\n' && suffix[0] != '\r' {
		cleaned = prefix + " " + suffix
	} else {
		cleaned = prefix + suffix
	}
	return strings.TrimSpace(cleaned), true
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
