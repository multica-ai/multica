package handler

import (
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"net/http"
)

func (h *Handler) publishControllerReadback(r *http.Request, issue db.Issue) {
	fresh, err := h.Queries.GetIssue(r.Context(), issue.ID)
	if err != nil {
		return
	} // Durable outbox is acknowledged only after native readback.
	prefix := ""
	if ws, err := h.Queries.GetWorkspace(r.Context(), issue.WorkspaceID); err == nil {
		prefix = ws.IssuePrefix
	}
	response := issueToResponse(fresh, prefix)
	h.fillStatusCategory(r.Context(), issue.WorkspaceID, &response)
	h.publish(protocol.EventIssueUpdated, uuidToString(issue.WorkspaceID), "system", "", map[string]any{"issue": response, "controller_projection": true})
}
