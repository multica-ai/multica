package main

import (
	"context"
	"log/slog"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// registerIntegrationListeners subscribes to issue update events to sync
// status back to external trackers (Linear, GitHub) when Multica issues
// reach terminal states.
func registerIntegrationListeners(bus *events.Bus, integrationSvc *service.IntegrationService) {
	ctx := context.Background()

	bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}

		issue, ok := payload["issue"].(handler.IssueResponse)
		if !ok {
			return
		}

		// Only sync on status changes to meaningful states.
		changes, _ := payload["changes"].(map[string]any)
		if _, hasStatus := changes["status"]; !hasStatus {
			return
		}

		switch issue.Status {
		case "done", "cancelled", "in_progress", "in_review":
			// proceed
		default:
			return
		}

		// Sync in background to avoid blocking the event bus.
		go func() {
			issueID, err := util.ParseUUID(issue.ID)
			if err != nil {
				slog.Debug("integration listener: invalid issue id", "issue_id", issue.ID, "error", err)
				return
			}
			dbIssue, err := integrationSvc.Queries.GetIssue(ctx, issueID)
			if err != nil {
				slog.Debug("integration listener: issue not found", "issue_id", issue.ID)
				return
			}

			if err := integrationSvc.SyncStatusToExternal(ctx, dbIssue); err != nil {
				slog.Warn("integration listener: sync failed", "issue_id", issue.ID, "error", err)
			}
		}()
	})
}
