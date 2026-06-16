// Package privateagentrun implements the cerebro side of FIR-2385: when a
// member tags a private agent they don't own, the trigger gate still refuses
// to wake the agent, but instead of dropping the tag silently we record a
// run-request in the agent owner's inbox. The owner runs it from there.
//
// All creation + dedup + flag-gating lives here, in the cerebro zone, so the
// upstream comment handler only carries a small marked hook (the
// MentionTriggerGate authz answer stays a pure boolean — Sara, FIR-2385).
package privateagentrun

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/sysactivity"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// flagKey gates the whole FIR-2385 behavior. Default-on (registry.ts); the
// server only ever sees an explicit per-user override.
const flagKey = "cerebro_private_agent_requests"

// Service creates private-agent run-requests in the owner's inbox.
type Service struct {
	Queries *db.Queries
	Cerebro *cerebrodb.Queries
	Bus     *events.Bus
}

// New constructs the service. The router wires it onto the upstream Handler
// seam (handler.PrivateAgentRunRequesterInvoker). queries is the upstream sqlc
// querier used to record the System Activity timeline event (TECH-3533).
func New(queries *db.Queries, cerebro *cerebrodb.Queries, bus *events.Bus) *Service {
	return &Service{Queries: queries, Cerebro: cerebro, Bus: bus}
}

// RequestPrivateAgentRun implements handler.PrivateAgentRunRequesterInvoker.
// It is called from the comment mention path only when the trigger gate has
// already refused a *member* who is not the agent owner. Side-effect only:
// errors are logged, never surfaced to the tagging member (the tag itself is
// not an error).
func (s *Service) RequestPrivateAgentRun(ctx context.Context, workspaceID string, agentID, ownerID, issueID, commentID, requesterID pgtype.UUID, agentName string) error {
	if s == nil || s.Cerebro == nil {
		return nil
	}
	if !ownerID.Valid || !agentID.Valid || !issueID.Valid || !commentID.Valid {
		return nil
	}
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return nil
	}
	// Gate on the requester's flag (the same user whose ListAgents view shows
	// the agent as locked). Off → no request, restoring the silent-drop
	// behavior. No override row → default-on.
	if !s.enabled(ctx, wsUUID, requesterID) {
		return nil
	}

	agentIDStr := util.UUIDToString(agentID)
	commentIDStr := util.UUIDToString(commentID)
	if _, err := s.Cerebro.FindPrivateAgentRunRequest(ctx, cerebrodb.FindPrivateAgentRunRequestParams{
		WorkspaceID: wsUUID,
		RecipientID: ownerID,
		Column3:     agentIDStr,
		Column4:     commentIDStr,
	}); err == nil {
		// Dedup: the owner already has an open request for this (agent, comment).
		return nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		slog.Error("private-agent run-request dedup lookup failed", "agent_id", agentIDStr, "error", err)
		return err
	}

	title := agentName
	if title == "" {
		title = "Private agent"
	}
	details, _ := json.Marshal(map[string]string{
		"agent_id":     agentIDStr,
		"comment_id":   commentIDStr,
		"requester_id": util.UUIDToString(requesterID),
	})
	item, err := s.Cerebro.CreatePrivateAgentRunRequest(ctx, cerebrodb.CreatePrivateAgentRunRequestParams{
		WorkspaceID: wsUUID,
		RecipientID: ownerID,
		IssueID:     issueID,
		Title:       title,
		Body:        pgtype.Text{},
		ActorID:     requesterID,
		Details:     details,
	})
	if err != nil {
		slog.Error("private-agent run-request create failed", "agent_id", agentIDStr, "error", err)
		return err
	}
	s.publishInboxNew(item, workspaceID)
	// Record the request as a visible System Activity timeline event — the
	// "request → system activity + inbox message" half of the model. Best-effort.
	sysactivity.Record(ctx, s.Queries, s.Bus, wsUUID, issueID, requesterID, "member", sysactivity.ActionAgentRunRequested, map[string]any{
		"agent_id":   agentIDStr,
		"agent_name": agentName,
		"owner_id":   util.UUIDToString(ownerID),
		"comment_id": commentIDStr,
	})
	return nil
}

// enabled reports whether the flag is on for the requester. No override row
// means default-on; a DB error fails closed (no request) but is logged.
func (s *Service) enabled(ctx context.Context, workspaceID, userID pgtype.UUID) bool {
	if !userID.Valid {
		return true
	}
	on, err := s.Cerebro.GetCerebroFeatureFlag(ctx, cerebrodb.GetCerebroFeatureFlagParams{
		WorkspaceID: workspaceID,
		UserID:      userID,
		FlagKey:     flagKey,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return true
	}
	if err != nil {
		slog.Error("private-agent run-request flag lookup failed", "error", err)
		return false
	}
	return on
}

// publishInboxNew fans the new item out on the bus, scoped to the owner so
// only their inbox refetches. Mirrors TaskService.publishQuickCreateInbox.
func (s *Service) publishInboxNew(item cerebrodb.InboxItem, workspaceID string) {
	if s.Bus == nil {
		return
	}
	ownerID := util.UUIDToString(item.RecipientID)
	resp := map[string]any{
		"id":             util.UUIDToString(item.ID),
		"workspace_id":   util.UUIDToString(item.WorkspaceID),
		"recipient_type": item.RecipientType,
		"recipient_id":   ownerID,
		"type":           item.Type,
		"severity":       item.Severity,
		"issue_id":       util.UUIDToPtr(item.IssueID),
		"title":          item.Title,
		"body":           util.TextToPtr(item.Body),
		"read":           item.Read,
		"archived":       item.Archived,
		"created_at":     util.TimestampToString(item.CreatedAt),
		"actor_type":     util.TextToPtr(item.ActorType),
		"actor_id":       util.UUIDToPtr(item.ActorID),
		"details":        json.RawMessage(item.Details),
		"route":          item.Route,
	}
	s.Bus.Publish(events.Event{
		Type:            protocol.EventInboxNew,
		WorkspaceID:     workspaceID,
		ActorType:       "member",
		ActorID:         util.UUIDToString(item.ActorID),
		Payload:         map[string]any{"item": resp},
		AudienceUserIDs: []string{ownerID},
	})
}
