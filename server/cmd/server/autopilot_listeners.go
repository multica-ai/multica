package main

import (
	"context"
	"log/slog"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// registerAutopilotListeners hooks into issue and task events to keep autopilot
// runs in sync. Which listener actually finalizes a create_issue run is decided at
// event time by the AutopilotTaskDrivenRuns gate (MUL-4809 §4.1 P0-3), so BOTH the
// issue-status listener (legacy) and the task-terminal listeners (task-driven) are
// registered and self-gate: exactly one path is live for a given process.
func registerAutopilotListeners(bus *events.Bus, svc *service.AutopilotService) {
	ctx := context.Background()

	// Legacy path: when an autopilot-origin issue reaches a run-finalizing status,
	// SyncRunFromIssue finalizes the run — but only while the task-driven gate is
	// off (it self-gates to a no-op when on). Registered so a gate-off pod finalizes
	// runs like the old pods it runs alongside during a rolling deploy.
	bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		if statusChanged, _ := payload["status_changed"].(bool); !statusChanged {
			return
		}
		issue, ok := payload["issue"].(handler.IssueResponse)
		if !ok {
			return
		}
		// Cheap pre-filter: only these statuses finalize a run (SyncRunFromIssue's
		// switch), so skip the DB load for any other transition.
		if issue.Status != "done" && issue.Status != "in_review" && issue.Status != "cancelled" && issue.Status != "blocked" {
			return
		}
		dbIssue, err := svc.Queries.GetIssue(ctx, parseUUID(issue.ID))
		if err != nil {
			slog.Debug("autopilot listener: failed to load issue", "issue_id", issue.ID, "error", err)
			return
		}
		svc.SyncRunFromIssue(ctx, dbIssue)
	})

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
	// run_only tasks carry autopilot_run_id and are ALWAYS task-driven (unchanged by
	// §4.1). create_issue tasks carry only issue_id; SyncRunFromCreateIssueTask
	// self-gates so it finalizes only when the task-driven gate is on.
	if task.AutopilotRunID.Valid {
		svc.SyncRunFromTask(ctx, task)
		return
	}
	svc.SyncRunFromCreateIssueTask(ctx, task)
}
