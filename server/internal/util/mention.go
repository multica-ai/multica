package util

import "regexp"

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

// IssueReferences extracts the deduplicated set of issue IDs referenced via
// issue mention links ([MUL-123](mention://issue/<id>)) in markdown content,
// preserving first-seen order.
//
// Issue references are pure cross-references: unlike member/agent/squad
// mentions they never notify anyone and never trigger an agent. This helper
// exists so callers that persist issue relations/backlinks (ITT-237) can
// pull the referenced issues out of content without re-implementing the
// mention parsing — and without ever touching the notification path.
//
// Deduplication is enforced here rather than relying on ParseMentions, so the
// "deduplicated set" contract holds regardless of upstream parsing changes.
func IssueReferences(content string) []string {
	var ids []string
	seen := make(map[string]bool)
	for _, m := range ParseMentions(content) {
		if m.Type != "issue" || seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		ids = append(ids, m.ID)
	}
	return ids
}
