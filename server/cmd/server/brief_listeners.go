package main

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/inbox"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// notifyBriefReady raises a brief_ready inbox item (severity attention) for
// each workspace owner. Admins can publish and decide, but the brief is the
// boss's, so only owners are announced to. The publisher is not skipped: an
// owner who publishes by hand still gets the item.
func notifyBriefReady(ctx context.Context, queries *db.Queries, bus *events.Bus, e events.Event) {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return
	}
	brief, ok := payload["brief"].(handler.BriefResponse)
	if !ok {
		return
	}
	owners, err := queries.ListWorkspaceOwnerUserIDs(ctx, parseUUID(e.WorkspaceID))
	if err != nil {
		slog.Error("brief:created notification: failed to list owners", "workspace_id", e.WorkspaceID, "error", err)
		return
	}
	ownerIDs := make([]string, len(owners))
	for i, id := range owners {
		ownerIDs[i] = util.UUIDToString(id)
	}
	prefs := loadUserPrefs(ctx, queries, e.WorkspaceID, ownerIDs)
	details, _ := json.Marshal(map[string]string{"brief_id": brief.ID, "kind": brief.Kind})

	for _, ownerID := range ownerIDs {
		if p, ok := prefs[ownerID]; ok && isNotifMuted(p, "brief_ready") {
			continue
		}
		item, inserted, err := inbox.CreateIfEligible(ctx, queries, db.CreateInboxItemParams{
			ID:            dbid.NewV7(),
			WorkspaceID:   parseUUID(e.WorkspaceID),
			RecipientType: "member",
			RecipientID:   parseUUID(ownerID),
			Type:          "brief_ready",
			Severity:      "attention",
			IssueID:       pgtype.UUID{},
			Title:         brief.Title,
			Body:          util.StrToText(service.BriefHeadline(brief.Body)),
			ActorType:     util.StrToText(e.ActorType),
			ActorID:       optionalUUID(e.ActorID),
			Details:       details,
		})
		if err != nil {
			slog.Error("brief:created notification: inbox item failed", "brief_id", brief.ID, "recipient_id", ownerID, "error", err)
			continue
		}
		if !inserted {
			continue
		}
		bus.Publish(events.Event{
			Type:        protocol.EventInboxNew,
			WorkspaceID: e.WorkspaceID,
			ActorType:   e.ActorType,
			ActorID:     e.ActorID,
			Payload:     map[string]any{"item": inboxItemToResponse(item)},
		})
	}
}
