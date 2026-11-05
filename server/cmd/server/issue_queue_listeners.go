package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const issueQueuePromoteTimeout = 30 * time.Second

// registerIssueQueueListeners starts the next queued backlog issues as soon as
// an issue reaches a finished status. The scheduler sweep covers every write
// that does not publish this event.
func registerIssueQueueListeners(bus *events.Bus, h *handler.Handler) {
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
		// Built-in keys decide here; a custom key may be done/closed, so it goes
		// through and the query reads its category.
		if issuestatus.IsBuiltIn(issue.Status) && issue.Status != issuestatus.Done && issue.Status != issuestatus.Cancelled {
			return
		}
		workspaceID := parseUUID(issue.WorkspaceID)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), issueQueuePromoteTimeout)
			defer cancel()
			if _, err := h.PromoteQueuedIssues(ctx, workspaceID, false); err != nil {
				slog.Warn("issue queue: promote failed", "error", err, "issue_id", issue.ID)
			}
		}()
	})
}
