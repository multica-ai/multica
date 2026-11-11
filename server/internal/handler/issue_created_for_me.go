package handler

import (
	"net/http"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ListIssueIDsCreatedForMe returns the ids of the issues an agent quick-created
// for the caller — the agent-filed half of the "created" relation — so clients
// can evaluate that relation exactly on rows they filter themselves (#4673).
// Issues the caller created directly are not listed: clients decide those
// from the row's own creator.
func (h *Handler) ListIssueIDsCreatedForMe(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, ctxWorkspaceID(r.Context()), "workspace id")
	if !ok {
		return
	}
	ids, err := h.Queries.ListIssueIDsCreatedForMember(r.Context(), db.ListIssueIDsCreatedForMemberParams{
		WorkspaceID: wsUUID,
		UserID:      parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list issues created for you")
		return
	}
	issueIDs := make([]string, len(ids))
	for i, id := range ids {
		issueIDs[i] = uuidToString(id)
	}
	writeJSON(w, http.StatusOK, map[string][]string{"issue_ids": issueIDs})
}
