package handler

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Check the entire selection before cancelling runs or deleting any records.
// Agent requests never inherit the role or ownership rights of their token's user.
func (h *Handler) requireIssueDeletion(w http.ResponseWriter, r *http.Request, workspaceID pgtype.UUID, issues []db.Issue) bool {
	workspace, err := h.Queries.GetWorkspace(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load issue deletion policy")
		return false
	}
	if !workspace.RestrictIssueDeletion {
		return true
	}
	// Cloud PATs have a user but no bound agent. They cannot claim that user's
	// deletion rights; only task tokens identify a machine creator reliably.
	if isMachineCredentialActor(r) && r.Header.Get("X-Actor-Source") != "task_token" {
		writeError(w, http.StatusForbidden, "restricted issue deletion requires a human or a bound agent identity")
		return false
	}
	wsID := uuidToString(workspaceID)
	actorType, actorID := h.resolveActor(r, requestUserID(r), wsID)
	if actorType == "member" {
		member, ok := h.workspaceMember(w, r, wsID)
		if !ok {
			return false
		}
		if roleAllowed(member.Role, "owner", "admin") {
			return true
		}
	}
	ownedAgents := make(map[pgtype.UUID]bool)
	for _, issue := range issues {
		if issue.CreatorType == actorType && uuidToString(issue.CreatorID) == actorID {
			continue
		}
		if actorType == "member" && issue.CreatorType == "agent" {
			owned, checked := ownedAgents[issue.CreatorID]
			if !checked {
				agent, err := h.Queries.GetAgent(r.Context(), issue.CreatorID)
				owned = err == nil && agent.WorkspaceID == workspaceID && uuidToString(agent.OwnerID) == actorID
				ownedAgents[issue.CreatorID] = owned
			}
			if owned {
				continue
			}
		}
		writeError(w, http.StatusForbidden, "issue deletion is restricted to its creator, the creating agent's owner, and workspace owners or admins")
		return false
	}
	return true
}
