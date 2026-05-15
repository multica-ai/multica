package handler

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// canAccessPrivateAgent gates private-agent read/chat/history surfaces.
// Public agents are unrestricted. Agent-to-agent traffic is allowed so A2A
// collaboration keeps working; member access is owner, workspace owner/admin,
// or explicit allowlist.
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
	if roleAllowed(member.Role, "owner", "admin") {
		return true
	}
	return h.isAgentAllowedPrincipal(ctx, agent.ID, actorID)
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

// memberAllowedForPrivateAgent is the pure predicate used by tests and by
// access paths that do not have the explicit allowlist preloaded.
func memberAllowedForPrivateAgent(agent db.Agent, userID, role string) bool {
	if roleAllowed(role, "owner", "admin") {
		return true
	}
	return uuidToString(agent.OwnerID) == userID
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
	allowedRows, err := h.Queries.ListAgentAllowedPrincipalIDsByWorkspace(ctx, wsUUID)
	if err != nil {
		return nil, false
	}
	allowedUserMap := map[string][]string{}
	for _, row := range allowedRows {
		agentID := uuidToString(row.AgentID)
		allowedUserMap[agentID] = append(allowedUserMap[agentID], uuidToString(row.PrincipalID))
	}

	allowed := make(map[string]struct{}, len(agents))
	for _, a := range agents {
		agentID := uuidToString(a.ID)
		if a.Visibility == "private" && actorType == "member" {
			if !memberAllowedForPrivateAgentWithAllowlist(a, actorID, role, allowedUserMap[agentID]) {
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
