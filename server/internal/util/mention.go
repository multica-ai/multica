package util

import "regexp"

// Mention represents a parsed @mention from markdown content.
type Mention struct {
	Type string // "member", "agent", "issue", "all", or "broadcast"
	ID   string // user_id, agent_id, issue_id, "all", or tag name / "all" for broadcast
}

// MentionRe matches [@Label](mention://type/id) or [Label](mention://issue/id) in markdown.
// The @ prefix is optional to support issue mentions which use [MUL-123](mention://issue/...).
// Uses .+? (non-greedy) instead of [^\]]* so labels containing square brackets
// (e.g. "David[TF]") are matched correctly — the ](mention:// anchor is specific
// enough to prevent over-matching.
// The broadcast type uses a tag name (lowercase alphanumeric/hyphen/underscore) or "all" as the ID.
var MentionRe = regexp.MustCompile(`\[@?(.+?)\]\(mention://(member|agent|issue|all|broadcast)/([0-9a-fA-F-]+|all|[a-z][a-z0-9_-]*)\)`)

// IsMentionAll returns true if the mention is an @all mention.
func (m Mention) IsMentionAll() bool {
	return m.Type == "all"
}

// IsBroadcast returns true if the mention is a broadcast (@@) mention.
func (m Mention) IsBroadcast() bool {
	return m.Type == "broadcast"
}

// BroadcastTag returns the tag name for a scoped broadcast mention (@@tagname),
// or empty string for an unscoped broadcast (@@).
func (m Mention) BroadcastTag() string {
	if m.Type != "broadcast" {
		return ""
	}
	if m.ID == "all" {
		return ""
	}
	return m.ID
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

// HasBroadcastMention returns true if any mention in the slice is a broadcast (@@) mention.
func HasBroadcastMention(mentions []Mention) bool {
	for _, m := range mentions {
		if m.IsBroadcast() {
			return true
		}
	}
	return false
}
