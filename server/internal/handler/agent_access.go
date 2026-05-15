package handler

import (
	"context"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func (h *Handler) canTriggerPrivateAgent(ctx context.Context, agent db.Agent, actorType, actorID string) bool {
	if agent.Visibility != "private" {
		return true
	}

	ownerID := uuidToString(agent.OwnerID)
	switch actorType {
	case "member":
		if ownerID == actorID {
			return true
		}
		allowed, err := h.Queries.IsAgentAllowedPrincipal(ctx, db.IsAgentAllowedPrincipalParams{
			AgentID:     agent.ID,
			PrincipalID: parseUUID(actorID),
		})
		return err == nil && allowed
	case "agent":
		triggerAgent, err := h.Queries.GetAgent(ctx, parseUUID(actorID))
		return err == nil && uuidToString(triggerAgent.OwnerID) == ownerID
	default:
		return false
	}
}
