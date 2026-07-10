package agentoffice

// Agent Office change-request notifications (FIR-1775).
//
// When someone proposes a versioned edit to an agent's context, the people who
// can review it (the context owner and every named approver, minus the proposer
// themselves) should see it land in their inbox with a deep-link back to the
// agent's Instructions tab — exactly the way a skill change request notifies the
// skill's owner/approvers today (see cerebro_skill_notifications.go).
//
// This file only *publishes* a recipient-scoped event onto the bus. A listener
// in package main (registerCerebroAgentOfficeNotificationListener) consumes it
// and fans it out through the per-user channel matrix via dispatchToMember, so
// the same Inbox / Notifications / Mobile / Desktop preferences that issue and
// skill notifications use apply here too. Inbox is on by default; push channels
// stay opt-in (see defaultChannelChoices in cmd/server/notification_routing.go).

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
)

// EventAgentContextNotify is the bus event a proposed agent-context change emits,
// one per recipient. The cmd/server listener turns each into a routed inbox item.
const EventAgentContextNotify = "cerebro:agent_context:notify"

// inboxTypeAgentContextChangeRequestCreated is both the inbox_item.type stored
// for the row and the routing key (routeKey falls back to the notif type when it
// isn't a skill/split key), so its channel defaults live under this exact string
// in defaultChannelChoices.
const inboxTypeAgentContextChangeRequestCreated = "agent_context_change_request"

// notifyChangeRequestCreated publishes one inbox event per reviewer when a change
// request is proposed. Failures are logged, never returned: a notification must
// never roll back the change request itself.
func (h *Handler) notifyChangeRequestCreated(ctx context.Context, agent cerebrodb.Agent, cr cerebrodb.AgentChangeRequest, proposer pgtype.UUID) {
	if h.Svc.Bus == nil {
		return
	}
	recipients := changeRequestRecipients(agent, proposer)
	if len(recipients) == 0 {
		return
	}

	details := map[string]any{
		"change_request_id": util.UUIDToString(cr.ID),
		"agent_id":          util.UUIDToString(agent.ID),
		"agent_name":        agent.Name,
		"proposer_id":       util.UUIDToString(proposer),
		"title":             cr.Title,
		"description":       cr.Description,
		"base_version":      cr.BaseVersion,
		"proposed_version":  cr.ProposedVersion,
	}
	if cr.WorkSessionID.Valid {
		details["work_session_id"] = util.UUIDToString(cr.WorkSessionID)
		details["session_url"] = fmt.Sprintf("/sessions/%s", util.UUIDToString(cr.WorkSessionID))
	}
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		slog.Warn("agent-office change request: failed to encode inbox details",
			"agent_id", util.UUIDToString(agent.ID), "error", err)
		return
	}

	title := fmt.Sprintf("Change request on %s", agent.Name)
	body := changeRequestBody(agent.Name, cr.BaseVersion, cr.ProposedVersion, cr.Description)
	for _, rid := range recipients {
		h.Svc.Bus.Publish(events.Event{
			Type:        EventAgentContextNotify,
			WorkspaceID: util.UUIDToString(agent.WorkspaceID),
			ActorType:   "member",
			ActorID:     util.UUIDToString(proposer),
			Payload: map[string]any{
				"recipient_id": util.UUIDToString(rid),
				"notif_type":   inboxTypeAgentContextChangeRequestCreated,
				"severity":     "action_required",
				"title":        title,
				"body":         body,
				"details":      string(detailsJSON),
			},
		})
	}
}

// changeRequestBody renders the who/what for the inbox row.
func changeRequestBody(agentName, baseVersion, proposedVersion, description string) string {
	body := fmt.Sprintf("%s → %s on %s", baseVersion, proposedVersion, agentName)
	if description != "" {
		body = body + ": " + description
	}
	return body
}

// changeRequestRecipients returns the deduplicated set of users who should see a
// proposal: the context owner and every approver, minus the proposer themselves.
func changeRequestRecipients(agent cerebrodb.Agent, proposer pgtype.UUID) []pgtype.UUID {
	seen := map[string]struct{}{}
	if proposer.Valid {
		seen[util.UUIDToString(proposer)] = struct{}{}
	}
	out := make([]pgtype.UUID, 0, 1+len(agent.ContextApproverIds))
	add := func(u pgtype.UUID) {
		if !u.Valid {
			return
		}
		k := util.UUIDToString(u)
		if _, ok := seen[k]; ok {
			return
		}
		seen[k] = struct{}{}
		out = append(out, u)
	}
	add(agent.ContextOwnerID)
	for _, a := range agent.ContextApproverIds {
		add(a)
	}
	return out
}
