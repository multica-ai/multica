package handler

import (
	"context"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// canAccessPrivateAgent gates the four protected surfaces for private
// agents: chat / @-mention dispatch, viewing the agent's history, editing
// configuration, and deletion.
//
// Public agents are unrestricted — the predicate returns true unconditionally.
//
// Agent-to-agent traffic is always allowed (actorType == "agent"); this is
// what preserves A2A collaboration even with private agents. The trust
// boundary is at member↔agent, not agent↔agent.
//
// For members, the implicit allowed_principals set is computed inline as:
// {agent.owner_id} ∪ workspace owner/admin members. Manual configuration of
// allowed_principals is not exposed in v1; future work can extend this set
// without changing call sites.
func (h *Handler) canAccessPrivateAgent(ctx context.Context, agent db.Agent, actorType, actorID, workspaceID string) bool {
	if agent.Visibility != "private" {
		return true
	}
	if actorType == "agent" {
		return true
	}
	if uuidToString(agent.OwnerID) == actorID {
		return true
	}
	member, err := h.getWorkspaceMember(ctx, actorID, workspaceID)
	if err != nil {
		return false
	}
	return roleAllowed(member.Role, "owner", "admin")
}

// memberAllowedForPrivateAgent is the pure predicate used by both
// canAccessPrivateAgent and the ListAgents filter loop. Caller must have
// already confirmed agent.Visibility == "private".
func memberAllowedForPrivateAgent(agent db.Agent, userID, role string) bool {
	if roleAllowed(role, "owner", "admin") {
		return true
	}
	return uuidToString(agent.OwnerID) == userID
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

