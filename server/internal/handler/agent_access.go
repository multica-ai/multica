package handler

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// canAccessPrivateAgent gates read-only surfaces for private agents: viewing
// the agent's details, task history, and configuration.
//
// OPE-817: all workspace members may VIEW any agent (including private) for
// learning and reference. Interactive surfaces (chat creation, @-mention
// dispatch) are gated separately by canTriggerPrivateAgent.
//
// Public agents are unrestricted — the predicate returns true unconditionally.
//
// Agent-to-agent traffic is always allowed (actorType == "agent"); this is
// what preserves A2A collaboration even with private agents.
//
// For members, any workspace member can view a private agent's details.
func (h *Handler) canAccessPrivateAgent(ctx context.Context, agent db.Agent, actorType, actorID, workspaceID string) bool {
	if agent.Visibility != "private" {
		return true
	}
	if actorType == "agent" {
		return true
	}
	// OPE-817: any workspace member can read private agent details.
	// Verify actorID is a workspace member (not an outsider).
	_, err := h.getWorkspaceMember(ctx, actorID, workspaceID)
	return err == nil
}

// canTriggerPrivateAgent enforces the stricter mention/assign policy:
// private agents may be triggered by their owner, explicitly allowed members,
// or same-owner agents. Workspace admins do not bypass this gate unless they
// are also explicitly allowlisted.
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
		return h.isAgentAllowedPrincipal(ctx, agent.ID, actorID)
	case "agent":
		triggerAgent, err := h.Queries.GetAgent(ctx, parseUUID(actorID))
		return err == nil && uuidToString(triggerAgent.OwnerID) == ownerID
	default:
		return false
	}
}

// memberAllowedForPrivateAgent is the pure predicate used by the ListAgents
// filter loop. Caller must have already confirmed agent.Visibility == "private".
//
// OPE-817: all workspace members may VIEW any agent for learning/reference.
// Returns true unconditionally — the caller has already validated the user
// is a workspace member.
func memberAllowedForPrivateAgent(agent db.Agent, userID, role string) bool {
	return true
}

func memberAllowedForPrivateAgentWithAllowlist(agent db.Agent, userID, role string, allowedUserIDs []string) bool {
	if memberAllowedForPrivateAgent(agent, userID, role) {
		return true
	}
	for _, allowedUserID := range allowedUserIDs {
		if allowedUserID == userID {
			return true
		}
	}
	return false
}

// accessibleAgentIDs returns the set of agent IDs in the workspace the actor
// is allowed to see, for use by workspace-wide aggregation endpoints
// (run counts, activity histograms, task snapshots) that need to filter out
// private agents the member can't access. Returns nil and false on error.
func (h *Handler) accessibleAgentIDs(ctx context.Context, workspaceID, actorType, actorID, role string) (map[string]struct{}, bool) {
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return nil, false
	}
	agents, err := h.Queries.ListAllAgents(ctx, wsUUID)
	if err != nil {
		return nil, false
	}
	allowed := make(map[string]struct{}, len(agents))
	for _, a := range agents {
		agentID := uuidToString(a.ID)
		if a.Visibility == "private" && actorType == "member" {
			if !memberAllowedForPrivateAgent(a, actorID, role) {
				continue
			}
		}
		allowed[agentID] = struct{}{}
	}
	return allowed, true
}

func (h *Handler) isAgentAllowedPrincipal(ctx context.Context, agentID pgtype.UUID, principalID string) bool {
	principalUUID, err := util.ParseUUID(principalID)
	if err != nil {
		return false
	}
	allowed, err := h.Queries.IsAgentAllowedPrincipal(ctx, db.IsAgentAllowedPrincipalParams{
		AgentID:     agentID,
		PrincipalID: principalUUID,
	})
	return err == nil && allowed
}
