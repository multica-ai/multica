// Package card — comment.go renders the "新评论" interactive card.
//
// Responsibilities:
//   - Map a CommentData (issue context + author + body + URL) to a Feishu
//     interactive card with header, author/issue line, comment body, and
//     a "回复评论" button.
//   - Auto-truncate Body to 200 runes via TruncateRunes — callers do NOT
//     need to clip the field themselves.
//
// Boundaries:
//   - No DB / API / SDK access — CommentData is the only input; render
//     output is JSON via Card.Render(). Producers (outbound aggregator
//     T14, channel facade) build CommentData; this file does not know
//     who they are.
package card

import "fmt"

// CommentData holds the fields needed to render a Comment card.
//
// IssueIdentifier / IssueTitle / AuthorName fall back to neutral
// placeholders if empty so the rendered card never has dangling brackets
// or "by " with no name. Body is automatically truncated to 200 runes by
// CommentCard.
type CommentData struct {
	// IssueIdentifier is the human-readable issue id, e.g. "STA-2".
	IssueIdentifier string
	// IssueTitle is the issue title for context.
	IssueTitle string
	// AuthorName is the comment author's display name.
	AuthorName string
	// Body is the comment text; automatically truncated to 200 runes by
	// CommentCard.
	Body string
	// URL is the web link to the issue detail page.
	URL string
}

// CommentCard builds a Feishu interactive card for a new comment.
// Layout: header / author + issue context / body (auto-truncated) /
// reply button.
func CommentCard(d CommentData) *Card {
	identifier := d.IssueIdentifier
	if identifier == "" {
		identifier = "ISSUE"
	}
	issueTitle := d.IssueTitle
	if issueTitle == "" {
		issueTitle = "(无标题)"
	}
	author := d.AuthorName
	if author == "" {
		author = "(匿名)"
	}

	c := NewCard(fmt.Sprintf("[%s] 新评论", identifier), "turquoise")

	authorLine := fmt.Sprintf("**%s** 评论了 [%s] %s", author, identifier, issueTitle)
	c.AddMarkdown(authorLine)

	c.AddHR()
	c.AddMarkdown(TruncateRunes(d.Body, summaryMaxRunes))

	if d.URL != "" {
		c.AddHR()
		c.AddButton("回复评论", d.URL)
	}

	return c
}
