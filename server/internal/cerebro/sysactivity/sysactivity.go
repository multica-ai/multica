// Package sysactivity records System Activity timeline events — system-generated
// activity_log entries that render in the issue timeline (e.g. a colleague
// requesting a cross-actor agent run, or the owner approving it). It mirrors how
// the wakeup feature records its own "wakeup_scheduled" activity, so the
// approval chain Jesper described — request → system activity; approve → system
// activity that triggers the agent — leaves a visible, auditable trail. TECH-3533.
package sysactivity

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Action constants for the cross-actor private-agent run flow.
const (
	ActionAgentRunRequested = "agent_run_requested"
	ActionAgentRunApproved  = "agent_run_approved"
)

// Record writes a system activity_log row for the given issue and broadcasts it
// so connected clients render it in the timeline. Best-effort: any failure is
// logged and swallowed — a missing audit line must never block the action that
// triggered it. actorType is "member" or "agent"; actorID is that actor.
func Record(ctx context.Context, queries *db.Queries, bus *events.Bus, workspaceID, issueID, actorID pgtype.UUID, actorType, action string, details map[string]any) {
	if queries == nil {
		return
	}
	payload, err := json.Marshal(details)
	if err != nil {
		slog.Warn("sysactivity details marshal failed", "action", action, "error", err)
		return
	}
	activity, err := queries.CreateActivity(ctx, db.CreateActivityParams{
		WorkspaceID: workspaceID,
		IssueID:     issueID,
		ActorType:   pgtype.Text{String: actorType, Valid: actorType != ""},
		ActorID:     actorID,
		Action:      action,
		Details:     payload,
	})
	if err != nil {
		slog.Warn("sysactivity create failed", "action", action, "error", err)
		return
	}
	if bus == nil {
		return
	}
	bus.Publish(events.Event{
		Type:        protocol.EventActivityCreated,
		WorkspaceID: util.UUIDToString(workspaceID),
		ActorType:   actorType,
		ActorID:     util.UUIDToString(actorID),
		Payload: map[string]any{
			"issue_id": util.UUIDToString(issueID),
			"entry": map[string]any{
				"type":       "activity",
				"id":         util.UUIDToString(activity.ID),
				"actor_type": actorType,
				"actor_id":   util.UUIDToString(actorID),
				"action":     activity.Action,
				"details":    json.RawMessage(activity.Details),
				"created_at": util.TimestampToString(activity.CreatedAt),
			},
		},
	})
}
