package handler

import (
	"context"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/multica-ai/multica-connect/channel"
	"github.com/multica-ai/multica-connect/multica"
	"github.com/multica-ai/multica-connect/triage"
)

type AnswerHandler struct {
	Claude       anthropic.Client
	MulticClient *multica.Client
	Model        string
}

func (h *AnswerHandler) Handle(ctx context.Context, msg channel.Message, result *triage.Result) (*Response, error) {
	model := h.Model
	if model == "" {
		model = anthropic.ModelClaudeHaiku4_5_20251001
	}

	// Get an answer from Claude.
	resp, err := h.Claude.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     model,
		MaxTokens: 1024,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(msg.Text)),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("claude answer: %w", err)
	}

	var answer string
	for _, block := range resp.Content {
		if block.Type == "text" {
			answer = block.Text
			break
		}
	}

	// Also save to Multica as a chat session for inbox visibility.
	session, err := h.MulticClient.CreateChatSession(ctx, result.Title)
	if err == nil {
		h.MulticClient.SendChatMessage(ctx, session.ID, msg.Text)
		h.MulticClient.SendChatMessage(ctx, session.ID, answer)
	}

	return &Response{Text: answer, ParseMode: "Markdown"}, nil
}
