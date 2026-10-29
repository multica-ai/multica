package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// registerChatIssueListeners posts the status moves of an issue created from a
// chat message into that message's thread (MUL-8357), so the person who asked
// sees the work progress where they asked for it.
func registerChatIssueListeners(bus *events.Bus, queries *db.Queries) {
	bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		if changed, _ := payload["status_changed"].(bool); !changed {
			return
		}
		issue, ok := payload["issue"].(handler.IssueResponse)
		if !ok {
			return
		}
		ctx := context.Background()
		link, err := queries.GetChatMessageIssue(ctx, parseUUID(issue.ID))
		if errors.Is(err, pgx.ErrNoRows) {
			return
		}
		if err != nil {
			slog.Warn("chat issue listener: load link failed", "issue_id", issue.ID, "error", err)
			return
		}
		prevStatus, _ := payload["prev_status"].(string)
		content := fmt.Sprintf("[%s](mention://issue/%s) moved to **%s**.", issue.Identifier, issue.ID, statusLabel(issue.Status))
		if prevStatus != "" {
			content = fmt.Sprintf("[%s](mention://issue/%s) moved from %s to **%s**.", issue.Identifier, issue.ID, statusLabel(prevStatus), statusLabel(issue.Status))
		}
		if err := handler.PostChatIssueEvent(ctx, queries, bus, link, content); err != nil {
			slog.Warn("chat issue listener: post event failed", "issue_id", issue.ID, "error", err)
		}
	})
}
