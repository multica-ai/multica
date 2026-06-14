package main

// CEREBRO-PATCH(cerebro-note-mentions-file): TECH-3421 — notify people tagged in
// a note. The cerebro Notes handler (server/internal/cerebro/note) publishes
// EventNoteMentioned with the newly-tagged member IDs; this listener turns each
// into a routed "mentioned" inbox item, reusing the exact engine + per-user
// notification settings ("mentioned" → "comments" group) that comment mentions
// use. The note reference rides in the item's details JSON — inbox_item.issue_id
// is an issue foreign key and a note is an artifact, not an issue — and the
// inbox UI deep-links from details.note_id.

import (
	"context"
	"encoding/json"
	"log/slog"

	cerebronote "github.com/multica-ai/multica/server/internal/cerebro/note"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// registerCerebroNoteMentionListener wires the note → notification fan-out.
func registerCerebroNoteMentionListener(bus *events.Bus, queries *db.Queries) {
	bus.Subscribe(cerebronote.EventNoteMentioned, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		noteID, _ := payload["note_id"].(string)
		noteTitle, _ := payload["note_title"].(string)
		memberIDs := noteMentionMemberIDs(payload["member_ids"])
		if noteID == "" || len(memberIDs) == 0 {
			return
		}

		title := noteTitle
		if title == "" {
			title = "New note"
		}
		details, _ := json.Marshal(map[string]any{
			"note_id":    noteID,
			"note_title": noteTitle,
		})

		ctx := context.Background()
		dispatched := 0
		for _, uid := range memberIDs {
			if uid == "" || uid == e.ActorID {
				continue
			}
			if dispatchToMember(ctx, queries, bus, inboxItemDraft{
				WorkspaceID:   e.WorkspaceID,
				RecipientType: "member",
				RecipientID:   uid,
				NotifType:     "mentioned",
				Severity:      "info",
				Title:         title,
				Details:       details,
				Actor:         e,
			}) {
				dispatched++
			}
		}
		slog.Info("note mention notifications dispatched",
			"note_id", noteID, "tagged", len(memberIDs), "delivered", dispatched)
	})
}

// noteMentionMemberIDs coerces the event payload's member_ids into []string,
// tolerating both the in-process []string and a deserialized []any of strings.
func noteMentionMemberIDs(v any) []string {
	switch s := v.(type) {
	case []string:
		return s
	case []any:
		out := make([]string, 0, len(s))
		for _, x := range s {
			if str, ok := x.(string); ok {
				out = append(out, str)
			}
		}
		return out
	default:
		return nil
	}
}
