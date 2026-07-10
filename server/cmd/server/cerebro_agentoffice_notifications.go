package main

// CEREBRO-PATCH(agent-office-notif-channels): FIR-1775 — route Agent Office
// change-request notifications through the same per-user channel matrix
// (inbox / notifications / mobile / desktop) as issue and skill notifications.
// The agent-office handler (server/internal/cerebro/agentoffice) resolves the
// reviewers (context owner + approvers, minus the proposer) and publishes one
// EventAgentContextNotify per recipient; this listener turns each into a routed
// inbox item via dispatchToMember.
//
// An agent context is not an issue, so the inbox_item carries no issue_id — the
// inbox UI deep-links from details.agent_id back to the agent's Instructions
// tab, mirroring the skill listener in cerebro_skill_notifications.go.

import (
	"context"
	"log/slog"

	cerebroagentoffice "github.com/multica-ai/multica/server/internal/cerebro/agentoffice"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// registerCerebroAgentOfficeNotificationListener wires the agent-context →
// notification fan-out onto the channel-routing engine.
func registerCerebroAgentOfficeNotificationListener(bus *events.Bus, queries *db.Queries) {
	bus.Subscribe(cerebroagentoffice.EventAgentContextNotify, func(e events.Event) {
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
			slog.Debug("agent-office notification suppressed by channel prefs",
				"recipient_id", recipientID, "type", notifType)
		}
	})
}
