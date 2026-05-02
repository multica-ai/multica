package main

import (
	"context"
	"log/slog"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// registerAutopilotListeners hooks into issue and task events to keep
// autopilot runs in sync with their linked issues and tasks.
func registerAutopilotListeners(bus *events.Bus, svc *service.AutopilotService) {
	ctx := context.Background()

	// When an issue with origin_type='autopilot' reaches a terminal status,
	// update the corresponding autopilot run.
	bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		statusChanged, _ := payload["status_changed"].(bool)
		if !statusChanged {
			return
		}
		issue, ok := payload["issue"].(handler.IssueResponse)
		if !ok {
			return
		}
		// Only handle statuses that finalize an autopilot run.
		if issue.Status != "done" && issue.Status != "needs_review" && issue.Status != "cancelled" && issue.Status != "blocked" {
			return
		}
		// Load the full issue from DB to check origin_type.
		dbIssue, err := svc.Queries.GetIssue(ctx, parseUUID(issue.ID))
		if err != nil {
			slog.Debug("autopilot listener: failed to load issue", "issue_id", issue.ID, "error", err)
			return
		}
		svc.SyncRunFromIssue(ctx, dbIssue)
	})

	// When a task completes or fails, check if it's an autopilot run_only task.
	bus.Subscribe(protocol.EventTaskCompleted, func(e events.Event) {
		syncRunFromTaskEvent(ctx, svc, e)
	})
	bus.Subscribe(protocol.EventTaskFailed, func(e events.Event) {
		syncRunFromTaskEvent(ctx, svc, e)
	})
	bus.Subscribe(protocol.EventTaskCancelled, func(e events.Event) {
		syncRunFromTaskEvent(ctx, svc, e)
	})
}

func syncRunFromTaskEvent(ctx context.Context, svc *service.AutopilotService, e events.Event) {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return
	}
	taskID, ok := payload["task_id"].(string)
	if !ok || taskID == "" {
		return
	}
	task, err := svc.Queries.GetAgentTask(ctx, parseUUID(taskID))
	if err != nil {
		return
	}
	if !task.AutopilotRunID.Valid {
		return
	}
	svc.SyncRunFromTask(ctx, task)
}
