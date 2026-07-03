package handler

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// canAccessPrivateAgent gates the four protected surfaces for private
// agents: chat / @-mention dispatch, viewing the agent's history, editing
// configuration, and deletion.
//
// Public agents are unrestricted — the predicate returns true unconditionally.
//
// Agent-to-agent traffic is checked through the calling agent's owner. This
// preserves A2A collaboration for the same allowed principals while keeping
// another user's agent from dispatching work into a private agent.
//
// Platform-owned system triggers are allowed. They are not acting on behalf of
// another workspace member or agent.
//
// For members, the implicit allowed_principals set is computed inline as:
// {agent.owner_id} ∪ workspace owner/admin members. Manual configuration of
// allowed_principals is not exposed in v1; future work can extend this set
// without changing call sites.
func (h *Handler) canAccessPrivateAgent(ctx context.Context, agent db.Agent, actorType, actorID, workspaceID string) bool {
	if agent.Visibility != "private" {
		return true
	}
	if actorType == "system" {
		return true
	}
	if actorType == "agent" {
		return h.canAgentActorAccessPrivateAgent(ctx, agent, actorID, workspaceID)
	}
	return h.canMemberAccessPrivateAgent(ctx, agent, actorID, workspaceID)
}

func (h *Handler) canMemberAccessPrivateAgent(ctx context.Context, agent db.Agent, userID, workspaceID string) bool {
	if uuidToString(agent.OwnerID) == userID {
		return true
	}
	member, err := h.getWorkspaceMember(ctx, userID, workspaceID)
	if err != nil {
		return false
	}
	return roleAllowed(member.Role, "owner", "admin")
}

func (h *Handler) canAgentActorAccessPrivateAgent(ctx context.Context, target db.Agent, actorID, workspaceID string) bool {
	if h == nil || h.Queries == nil {
		return false
	}
	actorUUID, err := util.ParseUUID(actorID)
	if err != nil {
		return false
	}
	actor, err := h.Queries.GetAgent(ctx, actorUUID)
	if err != nil || uuidToString(actor.WorkspaceID) != workspaceID {
		return false
	}
	ownerID := uuidToString(actor.OwnerID)
	if uuidToString(target.OwnerID) == ownerID {
		return true
	}
	member, err := h.getWorkspaceMember(ctx, ownerID, workspaceID)
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

// canEnqueueSquadLeader returns true when the given actor is allowed to
// trigger the squad's private leader. It loads the leader agent and delegates
// to canAccessPrivateAgent. Non-private leaders always pass. System-initiated
// triggers (e.g. github webhooks) pass through the platform-owned "system"
// actor type.
func (h *Handler) canEnqueueSquadLeader(ctx context.Context, leaderID pgtype.UUID, actorType, actorID, workspaceID string) bool {
	agent, err := h.Queries.GetAgent(ctx, leaderID)
	if err != nil {
		return false
	}
	return h.canAccessPrivateAgent(ctx, agent, actorType, actorID, workspaceID)
}
