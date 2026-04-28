package handler

import (
	"context"
	"fmt"

	"github.com/multica-ai/multica-connect/channel"
	"github.com/multica-ai/multica-connect/multica"
	"github.com/multica-ai/multica-connect/triage"
)

type IssueHandler struct {
	Client  *multica.Client
	BaseURL string // Multica web URL for links
}

func (h *IssueHandler) Handle(ctx context.Context, msg channel.Message, result *triage.Result) (*Response, error) {
	desc := result.Description
	if desc == "" {
		desc = msg.Text
	}

	issue, err := h.Client.CreateIssue(ctx, multica.CreateIssueRequest{
		Title:       result.Title,
		Description: &desc,
		Priority:    result.Priority,
	})
	if err != nil {
		return nil, fmt.Errorf("create issue: %w", err)
	}

	identifier := fmt.Sprintf("%s-%d", issue.Prefix, issue.Number)
	text := fmt.Sprintf("🎫 Issue oprettet: *%s*\n\"%s\"\nPriority: %s",
		identifier, issue.Title, result.Priority)

	if h.BaseURL != "" {
		text += fmt.Sprintf("\n[Åbn i Multica](%s)", h.BaseURL)
	}

	return &Response{Text: text, ParseMode: "Markdown"}, nil
}
