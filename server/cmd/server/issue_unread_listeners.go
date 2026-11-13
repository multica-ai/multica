package main

import (
	"context"
	"log/slog"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// registerIssueUnreadListeners marks an issue unread for its subscribed members
// when an agent run on it ends, and tells each of them so their sidebar
// refetches. A failed attempt with a retry pending is not an end.
func registerIssueUnreadListeners(bus *events.Bus, queries *db.Queries) {
	ctx := context.Background()

	bus.Subscribe(protocol.EventTaskCompleted, func(e events.Event) {
		markIssueUnreadForRun(ctx, bus, queries, e, "completed")
	})

	bus.Subscribe(protocol.EventTaskFailed, func(e events.Event) {
		payload, _ := e.Payload.(map[string]any)
		if retryPending, _ := payload["retry_pending"].(bool); retryPending {
			return
		}
		markIssueUnreadForRun(ctx, bus, queries, e, "failed")
	})
}

func markIssueUnreadForRun(ctx context.Context, bus *events.Bus, queries *db.Queries, e events.Event, outcome string) {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return
	}
	issueID, _ := payload["issue_id"].(string)
	issueUUID, err := util.ParseUUID(issueID)
	if err != nil {
		return
	}
	agentID, _ := payload["agent_id"].(string)
	taskID, _ := payload["task_id"].(string)

	rows, err := queries.MarkIssueUnreadForSubscribers(ctx, db.MarkIssueUnreadForSubscribersParams{
		Outcome: outcome,
		AgentID: optionalUUID(agentID),
		TaskID:  optionalUUID(taskID),
		IssueID: issueUUID,
	})
	if err != nil {
		slog.Error("issue unread: failed to mark subscribers", "issue_id", issueID, "error", err)
		return
	}
	for _, row := range rows {
		workspaceID := util.UUIDToString(row.WorkspaceID)
		bus.Publish(events.Event{
			Type:        protocol.EventIssueUnreadChanged,
			WorkspaceID: workspaceID,
			ActorType:   "agent",
			ActorID:     agentID,
			Payload: map[string]any{
				"workspace_id": workspaceID,
				"recipient_id": util.UUIDToString(row.UserID),
				"issue_id":     issueID,
				"unread":       true,
			},
		})
	}
}
