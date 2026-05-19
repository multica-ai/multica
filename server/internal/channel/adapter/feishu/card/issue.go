// Package card — issue.go renders the "Issue 详情" interactive card.
//
// Responsibilities:
//   - Map an IssueData (identifier / title / status / assignee / summary /
//     URL) to a Feishu interactive card with header, status line, summary
//     body, and a "查看 Issue" button.
//   - Auto-truncate Summary to 200 runes via TruncateRunes — callers do
//     NOT need to clip the field themselves.
//
// Boundaries:
//   - No DB / API / SDK access — IssueData is the only input; render
//     output is JSON via Card.Render(). Producers (outbound aggregator
//     T14, channel facade) build IssueData; this file does not know who
//     they are.
package card

import "fmt"

// IssueData holds the fields needed to render an Issue detail card.
//
// Identifier and Title are required; missing values fall back to a
// neutral placeholder so an upstream bug never produces an empty card
// header. Summary is automatically truncated to 200 runes by IssueCard.
type IssueData struct {
	// Identifier is the human-readable issue id, e.g. "STA-2".
	Identifier string
	// Title is the issue title.
	Title string
	// Status is the current status (e.g. "todo", "in_progress", "done").
	Status string
	// AssigneeName is the assignee's display name. Empty if unassigned.
	AssigneeName string
	// Summary is a short summary of the issue description; automatically
	// truncated to 200 runes by IssueCard.
	Summary string
	// URL is the web link to the issue detail page.
	URL string
}

// IssueCard builds a Feishu interactive card for an Issue.
// Layout: title / status (+ assignee) / summary (auto-truncated) / link button.
func IssueCard(d IssueData) *Card {
	identifier := d.Identifier
	if identifier == "" {
		identifier = "ISSUE"
	}
	title := d.Title
	if title == "" {
		title = "(无标题)"
	}
	c := NewCard(fmt.Sprintf("[%s] %s", identifier, title), "blue")

	status := d.Status
	if status == "" {
		status = "(未知)"
	}
	statusLine := fmt.Sprintf("**状态**: %s", status)
	if d.AssigneeName != "" {
		statusLine += fmt.Sprintf("　　**负责人**: %s", d.AssigneeName)
	}
	c.AddMarkdown(statusLine)

	if d.Summary != "" {
		c.AddHR()
		c.AddMarkdown(TruncateRunes(d.Summary, summaryMaxRunes))
	}

	if d.URL != "" {
		c.AddHR()
		c.AddButton("查看 Issue", d.URL)
	}

	return c
}
