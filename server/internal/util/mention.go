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
// for @all mentions. WorkspaceID is non-empty for cross-workspace mentions
// (the ?ws=<uuid> qualifier).
type Mention struct {
	Type        string
	ID          string
	WorkspaceID string // populated for cross-workspace mentions (?ws= qualifier)
}

// wsIdx is the subexpression index of the cross-workspace qualifier (?ws=) group
// in MentionRe. Computed once at init so ParseMentions doesn't call SubexpIndex per-parse.
var wsIdx = MentionRe.SubexpIndex("ws")

// MentionRe matches [@Label](mention://type/id[?ws=<wsUuid>]) or [Label](mention://issue/id[?ws=<wsUuid>]) in markdown.
// The @ prefix is optional to support issue mentions which use [MUL-123](mention://issue/...).
// Uses .+? (non-greedy) instead of [^\]]* so labels containing square brackets
// (e.g. "David[TF]") are matched correctly — the ](mention:// anchor is specific
// enough to prevent over-matching.
//
// The type alternation is built from ValidMentionTypes so that adding a new
// type only requires editing the slice above.
//
// The optional (?:\?ws=(?P<ws>[0-9a-fA-F-]+))? non-capturing group after the id
// group matches the cross-workspace qualifier (e.g. "?ws=abc123") so the backend
// can parse it for structured injection. The id capture group is unchanged; the ws
// group is extracted separately.
var MentionRe = regexp.MustCompile(
	fmt.Sprintf(
		`\[@?(.+?)\]\(mention://(%s)/([0-9a-fA-F-]+|all)(?:\?ws=(?P<ws>[0-9a-fA-F-]+))?\)`,
		strings.Join(ValidMentionTypes, "|"),
	),
)

// IsMentionAll returns true if the mention is an @all mention.
func (m Mention) IsMentionAll() bool {
	return m.Type == "all"
}

// ParseMentions extracts deduplicated mentions from markdown content.
// For cross-workspace mentions (those carrying a ?ws= qualifier), WorkspaceID
// is populated so callers can distinguish same-workspace from cross-workspace.
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
		result = append(result, Mention{
			Type:        m[2],
			ID:          m[3],
			WorkspaceID: m[wsIdx],
		})
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
