package notification

import (
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
)

// parseWorkspaceID extracts a pgtype.UUID from an event's WorkspaceID string.
func parseWorkspaceID(ev events.Event) pgtype.UUID {
	if ev.WorkspaceID == "" {
		return pgtype.UUID{}
	}
	var id pgtype.UUID
	_ = id.Scan(ev.WorkspaceID)
	return id
}
