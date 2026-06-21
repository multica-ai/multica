package main

// CEREBRO-PATCH(skill-notif-channels): FIR-1587 — route skill notifications
// (change request created/reviewed, fork, agent-assigned) through the same
// per-user channel matrix as issue notifications. The skill handler
// (server/internal/handler/skill_ownership.go) applies the per-skill
// notify_change_requests / notify_forks / notify_agent_assigned gate and then
// publishes EventSkillNotify with one payload per recipient; this listener
// turns each into a routed inbox item via dispatchToMember, so the same Inbox /
// Notifications / Mobile / Desktop preferences (and Web Push fan-out) that
// issue notifications use now apply to skill events too.
//
// A skill is not an issue, so the inbox_item carries no issue_id — the inbox UI
// deep-links from details.skill_id / details.forked_skill_id (same shape the
// hardcoded inbox-only path used before this change), mirroring the
// note-mention listener in cerebro_note_mentions.go.

import (
	"context"
	"log/slog"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// registerCerebroSkillNotificationListener wires the skill → notification
// fan-out onto the channel-routing engine.
func registerCerebroSkillNotificationListener(bus *events.Bus, queries *db.Queries) {
	bus.Subscribe(protocol.EventSkillNotify, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		recipientID, _ := payload["recipient_id"].(string)
		notifType, _ := payload["notif_type"].(string)
		title, _ := payload["title"].(string)
		if recipientID == "" || notifType == "" {
			return
		}
		severity, _ := payload["severity"].(string)
		body, _ := payload["body"].(string)
		detailsStr, _ := payload["details"].(string)

		dispatched := dispatchToMember(context.Background(), queries, bus, inboxItemDraft{
			WorkspaceID:   e.WorkspaceID,
			RecipientType: "member",
			RecipientID:   recipientID,
			NotifType:     notifType,
			Severity:      severity,
			Title:         title,
			Body:          body,
			Details:       []byte(detailsStr),
			Actor:         e,
		})
		if !dispatched {
			slog.Debug("skill notification suppressed by channel prefs",
				"recipient_id", recipientID, "type", notifType)
		}
	})
}
