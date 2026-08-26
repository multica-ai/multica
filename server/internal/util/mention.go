package util

import (
	"regexp"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
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

var mentionDestRe = regexp.MustCompile(`^mention://(member|agent|squad|issue|all)/([0-9a-fA-F-]+|all)$`)

// IsMentionAll returns true if the mention is an @all mention.
func (m Mention) IsMentionAll() bool {
	return m.Type == "all"
}

// ParseMentions extracts deduplicated mentions from markdown content.
// Mentions inside fenced code blocks or inline code spans are ignored — they
// document the mention syntax rather than trigger an agent run. Mentions in
// blockquotes are still returned on purpose.
//
// The walk uses goldmark's CommonMark AST and only considers Link nodes, so
// code spans, fenced blocks (including nested-in-blockquote and CommonMark
// fence-length rules), and multiline code spans are excluded without a
// hand-rolled scanner.
func ParseMentions(content string) []Mention {
	source := []byte(content)
	doc := goldmark.New().Parser().Parse(text.NewReader(source))
	seen := make(map[string]bool)
	var result []Mention
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		link, ok := n.(*ast.Link)
		if !ok {
			return ast.WalkContinue, nil
		}
		m := mentionDestRe.FindStringSubmatch(string(link.Destination))
		if len(m) != 3 {
			return ast.WalkContinue, nil
		}
		key := m[1] + ":" + m[2]
		if seen[key] {
			return ast.WalkContinue, nil
		}
		seen[key] = true
		result = append(result, Mention{Type: m[1], ID: m[2]})
		return ast.WalkContinue, nil
	})
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
