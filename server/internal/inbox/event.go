package inbox

import (
	"encoding/json"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// EventItem is the item an inbox:new event carries. Every publisher builds it
// here so the event agrees with the list on the row's section: a client files
// an item without one under "Needs you", which for an update means a banner
// for something the recipient only follows. Callers add the issue projections
// they have (issue_status) to the returned map.
func EventItem(item db.InboxItem) map[string]any {
	return map[string]any{
		"id":             util.UUIDToString(item.ID),
		"workspace_id":   util.UUIDToString(item.WorkspaceID),
		"recipient_type": item.RecipientType,
		"recipient_id":   util.UUIDToString(item.RecipientID),
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
		"section":        EffectiveSection(item.Section, item.Type, item.Read),
		"snoozed_until":  util.TimestampToPtr(item.SnoozedUntil),
	}
}
