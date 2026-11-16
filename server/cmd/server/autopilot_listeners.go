package main

import (
	"context"
	"log/slog"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// autopilotIssueSync loads the stored issue and hands it to SyncRunFromIssue.
// Tests replace it to observe that the listener reached the sync without a
// database. The payload status is only a pre-filter; this call always re-reads.
var autopilotIssueSync = func(ctx context.Context, svc *service.AutopilotService, issueID string) {
	dbIssue, err := svc.Queries.GetIssue(ctx, parseUUID(issueID))
	if err != nil {
		slog.Debug("autopilot listener: failed to load issue", "issue_id", issueID, "error", err)
		return
	}
	svc.SyncRunFromIssue(ctx, dbIssue)
}

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
		// HTTP UpdateIssue sends handler.IssueResponse. Background writers
		// (broadcastIssueUpdated) send the IssueToMap map. The id and status
		// are the only fields this gate needs; the stored row decides the run.
		issueID, status, ok := autopilotIssueIDAndStatus(payload["issue"])
		if !ok {
			return
		}
		// Skip the issue load for routine forward progress. Built-in keys that
		// never settle a run stay out. Only done, in_review, and cancelled continue.
		// A custom status inherits its canonical behavior, but resolving
		// that needs a catalog read, so non-built-in keys pass and
		// SyncRunFromIssue normalizes the
		// stored row. (MUL-6243)
		if issuestatus.IsBuiltIn(status) &&
			status != "done" && status != "in_review" &&
			status != "cancelled" {
			return
		}
		autopilotIssueSync(ctx, svc, issueID)
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

// autopilotIssueIDAndStatus reads the issue id and status from either payload
// shape. A missing id or a non-string status is not an issue update this
// listener can act on.
func autopilotIssueIDAndStatus(v any) (string, string, bool) {
	if issue, ok := v.(handler.IssueResponse); ok {
		return issue.ID, issue.Status, issue.ID != ""
	}
	m, ok := v.(map[string]any)
	if !ok {
		return "", "", false
	}
	id, _ := m["id"].(string)
	status, _ := m["status"].(string)
	if id == "" || status == "" {
		return "", "", false
	}
	return id, status, true
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
	if task.AutopilotRunID.Valid {
		svc.SyncRunFromTask(ctx, task)
		return
	}
	// create_issue tasks link to their run only through issue_id. Completion
	// settles the run as success; terminal failure settles it as failed.
	if e.Type == protocol.EventTaskCompleted || e.Type == protocol.EventTaskFailed {
		svc.SyncRunFromLinkedIssueTask(ctx, task)
	}
}
