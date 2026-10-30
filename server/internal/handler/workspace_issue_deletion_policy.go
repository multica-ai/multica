package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// The policy has its own write boundary; generic settings snapshots cannot
// enable or disable it, regardless of the client version that sends them.
func (h *Handler) UpdateWorkspaceIssueDeletionPolicy(w http.ResponseWriter, r *http.Request) {
	if isMachineCredentialActor(r) {
		writeError(w, http.StatusForbidden, "only human workspace owners or admins can change the issue deletion policy")
		return
	}
	id := workspaceIDFromURL(r, "id")
	workspaceID, ok := parseUUIDOrBadRequest(w, id, "workspace id")
	if !ok {
		return
	}
	member, ok := h.workspaceMember(w, r, id)
	if !ok {
		return
	}
	if !roleAllowed(member.Role, "owner", "admin") {
		writeError(w, http.StatusForbidden, "only workspace owners or admins can change the issue deletion policy")
		return
	}
	var req struct {
		Restricted *bool `json:"restrict_issue_deletion"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil || req.Restricted == nil {
		writeError(w, http.StatusBadRequest, "restrict_issue_deletion must be a boolean")
		return
	}
	workspace, err := h.Queries.UpdateWorkspaceIssueDeletionPolicy(r.Context(), db.UpdateWorkspaceIssueDeletionPolicyParams{
		ID:                    workspaceID,
		RestrictIssueDeletion: *req.Restricted,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update issue deletion policy")
		return
	}
	response := h.workspaceToResponse(workspace)
	h.publish(protocol.EventWorkspaceUpdated, id, "member", requestUserID(r), map[string]any{"workspace": response})
	writeJSON(w, http.StatusOK, response)
}
