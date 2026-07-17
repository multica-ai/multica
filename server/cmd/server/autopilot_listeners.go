package main

import (
	"context"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// registerAutopilotListeners hooks into task terminal events to keep autopilot
// runs in sync with the task each run dispatched. Runs are finalized purely by
// task outcome (MUL-4809 §4.1) — issue status no longer ends or fails a run, so
// there is no EventIssueUpdated subscription here anymore.
func registerAutopilotListeners(bus *events.Bus, svc *service.AutopilotService) {
	ctx := context.Background()

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
	// run_only tasks carry autopilot_run_id; create_issue tasks carry only
	// issue_id and are matched to their run via run.task_id + retry lineage.
	if task.AutopilotRunID.Valid {
		svc.SyncRunFromTask(ctx, task)
		return
	}
	svc.SyncRunFromCreateIssueTask(ctx, task)
}
