package handler

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/multica-ai/multica-connect/channel"
	"github.com/multica-ai/multica-connect/triage"
)

// Router dispatches classified messages to the appropriate handler.
type Router struct {
	Issue    *IssueHandler
	Note     *NoteHandler
	Reminder *ReminderHandler
	Answer   *AnswerHandler
}

// Response is what a handler returns after processing a message.
type Response struct {
	Text      string
	ParseMode string
}

// Route classifies and handles an incoming message.
func (r *Router) Route(ctx context.Context, msg channel.Message, result *triage.Result) (*Response, error) {
	slog.Info("routing message",
		"category", result.Category,
		"title", result.Title,
		"confidence", result.Confidence,
		"thread_id", msg.ThreadID,
	)

	switch result.Category {
	case triage.CategoryIssue:
		return r.Issue.Handle(ctx, msg, result)
	case triage.CategoryNote:
		return r.Note.Handle(ctx, msg, result)
	case triage.CategoryReminder:
		return r.Reminder.Handle(ctx, msg, result)
	case triage.CategoryAnswer:
		return r.Answer.Handle(ctx, msg, result)
	default:
		return nil, fmt.Errorf("unknown category: %s", result.Category)
	}
}
