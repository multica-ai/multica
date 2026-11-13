package handler

import (
	"net/http"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// requireProjectDeletion mirrors requireIssueDeletion before any cleanup starts.
// Unknown creators (including pre-migration projects) grant no creator rights.
// Agent requests never inherit their token user's role or agent ownership.
func (h *Handler) requireProjectDeletion(w http.ResponseWriter, r *http.Request, project db.Project) bool {
	if isMachineCredentialActor(r) && r.Header.Get("X-Actor-Source") != "task_token" {
		writeError(w, http.StatusForbidden, "project deletion requires a human or a bound agent identity")
		return false
	}
	wsID := uuidToString(project.WorkspaceID)
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
	if project.CreatorType.Valid && project.CreatorID.Valid {
		if project.CreatorType.String == actorType && uuidToString(project.CreatorID) == actorID {
			return true
		}
		if actorType == "member" && project.CreatorType.String == "agent" {
			agent, err := h.Queries.GetAgent(r.Context(), project.CreatorID)
			if err == nil && agent.WorkspaceID == project.WorkspaceID && uuidToString(agent.OwnerID) == actorID {
				return true
			}
		}
	}
	writeError(w, http.StatusForbidden, "project deletion is restricted to its creator, the creating agent's owner, and workspace owners or admins")
	return false
}
