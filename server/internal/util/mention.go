package util

import (
	"fmt"
	"regexp"
	"strings"
)

// ValidMentionTypes is the canonical list of mention type strings accepted by
// MentionRe. When adding a new mention type, append it here *and* update the
// corresponding frontend list. A JSON fixture at
// server/internal/util/testdata/valid-mention-types.json mirrors this slice
// and is checked by a test below; the TS side has its own parallel test.
var ValidMentionTypes = []string{
	"member",
	"agent",
	"squad",
	"issue",
	"project",
	"all",
	"skill",
}

// Mention represents a parsed @mention from markdown content.
// Type is one of ValidMentionTypes ("member", "agent", "squad", "issue",
// "project", "all", "skill"). ID is the entity UUID, or the literal "all"
// for @all mentions.
type Mention struct {
	Type string
	ID   string
}

// MentionRe matches [@Label](mention://type/id) or [Label](mention://issue/id) in markdown.
// The @ prefix is optional to support issue mentions which use [MUL-123](mention://issue/...).
// Uses .+? (non-greedy) instead of [^\]]* so labels containing square brackets
// (e.g. "David[TF]") are matched correctly — the ](mention:// anchor is specific
// enough to prevent over-matching.
//
// The type alternation is built from ValidMentionTypes so that adding a new
// type only requires editing the slice above.
var MentionRe = regexp.MustCompile(
	fmt.Sprintf(
		`\[@?(.+?)\]\(mention://(%s)/([0-9a-fA-F-]+|all)\)`,
		strings.Join(ValidMentionTypes, "|"),
	),
)

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
		key := m[2] + ":" + m[3]
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, Mention{Type: m[2], ID: m[3]})
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
