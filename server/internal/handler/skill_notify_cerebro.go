package handler

// FIR-1587 — human-readable skill notification bodies. Cerebro-zone file
// (`_cerebro.go` suffix) so it is carved out of the upstream zone. The inbox
// body must say WHO did it, WHAT changed, and WHY — with real display names
// instead of short UUIDs, the proposer's reason, and a pointer to the session
// where the change was proposed so the full trail is one click away.

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
)

// skillActorName resolves a human display name for an inbox actor (the
// proposer, forker, or assigner). It tries the user table first, then agents,
// so a body never leaks a raw UUID. Falls back to "Someone" when unknown.
func (h *Handler) skillActorName(ctx context.Context, id pgtype.UUID) string {
	if !id.Valid || h.Queries == nil {
		return "Someone"
	}
	if u, err := h.Queries.GetUser(ctx, id); err == nil && strings.TrimSpace(u.Name) != "" {
		return u.Name
	}
	if a, err := h.Queries.GetAgent(ctx, id); err == nil && strings.TrimSpace(a.Name) != "" {
		return a.Name
	}
	return "Someone"
}

// skillChangeRequestBody builds the who / what / why line shown in the inbox.
// The "why" is the proposer's reason (the change-request description). When the
// reason is missing we say so explicitly — the proposer is expected to explain
// why and reference the session it came from. When a session is attached we
// point at it so the owner can open the discussion behind the proposal.
func skillChangeRequestBody(actor, skillName, baseVersion, proposedVersion, why string, hasSession bool) string {
	b := fmt.Sprintf("%s proposed an update to %s (v%s → v%s)",
		actor, skillName, baseVersion, proposedVersion)
	why = strings.TrimSpace(why)
	if why != "" {
		b += ". Why: " + why
	} else {
		b += ". No reason given yet — ask the proposer to explain why."
	}
	if hasSession {
		b += " · Proposed in a recorded work session — open the proposal for the trail."
	}
	return b
}

func skillForkedBody(actor, parentName, forkedName string) string {
	return fmt.Sprintf("%s forked %s into a new skill, %s", actor, parentName, forkedName)
}

func skillAgentAssignedBody(actor, skillName, agentName string) string {
	return fmt.Sprintf("%s assigned %s to the agent %s", actor, skillName, agentName)
}
