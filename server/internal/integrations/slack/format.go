package slack

import (
	"encoding/json"
	"fmt"
	"strings"
)

// MessageParams holds the data needed to build a Slack notification.
type MessageParams struct {
	IssueKey   string // e.g. "FRG-42"
	IssueTitle string
	PrevStatus string
	NewStatus  string
	ActorName  string
	IssueURL   string
}

// BuildMessage produces a Slack incoming-webhook JSON body for an issue status change.
func BuildMessage(p MessageParams) string {
	text := fmt.Sprintf(
		"📋 *[%s] %s*\nStatus: *%s* → *%s*\nBy: %s\n<%s|View issue>",
		escapeSlackMarkdown(p.IssueKey),
		escapeSlackMarkdown(p.IssueTitle),
		statusLabel(p.PrevStatus),
		statusLabel(p.NewStatus),
		escapeSlackMarkdown(p.ActorName),
		p.IssueURL,
	)
	b, _ := json.Marshal(map[string]string{"text": text})
	return string(b)
}

var statusDisplayNames = map[string]string{
	"backlog":     "Backlog",
	"todo":        "Todo",
	"in_progress": "In Progress",
	"in_review":   "In Review",
	"done":        "Done",
	"blocked":     "Blocked",
	"cancelled":   "Archive",
}

func statusLabel(s string) string {
	if label, ok := statusDisplayNames[s]; ok {
		return label
	}
	return s
}

// escapeSlackMarkdown escapes characters that have special meaning in Slack mrkdwn.
func escapeSlackMarkdown(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
