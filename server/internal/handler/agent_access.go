package handler

import (
	"context"

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

// canTriggerPrivateAgent enforces OPE-531 strict policy for mention/assign:
// only the agent owner (member) or agents sharing the same owner may trigger
// a private agent. Workspace admins do NOT get bypass.
func (h *Handler) canTriggerPrivateAgent(ctx context.Context, agent db.Agent, actorType, actorID string) bool {
	if agent.Visibility != "private" {
		return true
	}
	targetOwner := uuidToString(agent.OwnerID)
	switch actorType {
	case "member":
		return targetOwner == actorID
	case "agent":
		triggerAgent, err := h.Queries.GetAgent(ctx, parseUUID(actorID))
		if err != nil {
			return false
		}
		return uuidToString(triggerAgent.OwnerID) == targetOwner
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
		if a.Visibility == "private" && actorType == "member" {
			if !memberAllowedForPrivateAgent(a, actorID, role) {
				continue
			}
		}
		allowed[uuidToString(a.ID)] = struct{}{}
	}
	return allowed, true
}

