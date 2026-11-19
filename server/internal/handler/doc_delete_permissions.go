package handler

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// requireDocDeletion decides whether the caller may delete a doc or folder.
// Deleting a doc also deletes its version history, so the rule follows
// requireIssueDeletion: workspace owners and admins may delete anything;
// members may delete what they created or what an agent they currently own
// created. Unlike issues, only a signed-in human session deletes docs: agents,
// machine credentials and personal access tokens are refused whoever created
// the doc, so a script or CLI holding a PAT cannot wipe docs and their history.
func (h *Handler) requireDocDeletion(w http.ResponseWriter, r *http.Request, workspaceID pgtype.UUID, creatorType string, creatorID pgtype.UUID, noun string) bool {
	wsID := uuidToString(workspaceID)
	actorType, actorID := h.resolveActor(r, requestUserID(r), wsID)
	if isMachineCredentialActor(r) || middleware.IsPersonalAccessToken(r.Context()) || actorType != "member" || r.Header.Get("X-Agent-ID") != "" {
		writeError(w, http.StatusForbidden, "only humans can delete "+noun+"s")
		return false
	}
	member, ok := h.workspaceMember(w, r, wsID)
	if !ok {
		return false
	}
	if roleAllowed(member.Role, "owner", "admin") {
		return true
	}
	switch creatorType {
	case "member":
		if uuidToString(creatorID) == actorID {
			return true
		}
	case "agent":
		agent, err := h.Queries.GetAgent(r.Context(), creatorID)
		if err == nil && agent.WorkspaceID == workspaceID && uuidToString(agent.OwnerID) == actorID {
			return true
		}
	}
	writeError(w, http.StatusForbidden, noun+" deletion is restricted to its creator, the creating agent's owner, and workspace owners or admins")
	return false
}

func (h *Handler) requireDocDeletionFor(w http.ResponseWriter, r *http.Request, doc db.Doc) bool {
	return h.requireDocDeletion(w, r, doc.WorkspaceID, doc.CreatedByType, doc.CreatedByID, "doc")
}

func (h *Handler) requireDocFolderDeletionFor(w http.ResponseWriter, r *http.Request, folder db.DocFolder) bool {
	return h.requireDocDeletion(w, r, folder.WorkspaceID, folder.CreatedByType, folder.CreatedByID, "folder")
}
