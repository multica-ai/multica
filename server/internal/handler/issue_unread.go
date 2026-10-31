package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// IssueUnreadResponse is an issue with an agent result the caller has not seen
// yet. outcome is completed | failed, or manual when the caller marked it
// unread themselves; agent_id is null for manual.
type IssueUnreadResponse struct {
	IssueID   string  `json:"issue_id"`
	Outcome   string  `json:"outcome"`
	AgentID   *string `json:"agent_id"`
	CreatedAt string  `json:"created_at"`
}

// ListIssueUnread returns the caller's unread issues in the current workspace.
func (h *Handler) ListIssueUnread(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, ctxWorkspaceID(r.Context()), "workspace id")
	if !ok {
		return
	}

	rows, err := h.Queries.ListIssueUnread(r.Context(), db.ListIssueUnreadParams{
		WorkspaceID: wsUUID,
		UserID:      parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list unread issues")
		return
	}

	items := make([]IssueUnreadResponse, len(rows))
	for i, row := range rows {
		items[i] = IssueUnreadResponse{
			IssueID:   uuidToString(row.IssueID),
			Outcome:   row.Outcome,
			AgentID:   uuidToPtr(row.AgentID),
			CreatedAt: timestampToString(row.CreatedAt),
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// MarkIssueRead clears the caller's unread mark on an issue.
func (h *Handler) MarkIssueRead(w http.ResponseWriter, r *http.Request) {
	h.setIssueUnread(w, r, false)
}

// MarkIssueUnread marks an issue unread for the caller.
func (h *Handler) MarkIssueUnread(w http.ResponseWriter, r *http.Request) {
	h.setIssueUnread(w, r, true)
}

func (h *Handler) setIssueUnread(w http.ResponseWriter, r *http.Request, unread bool) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}

	params := db.MarkIssueReadParams{
		WorkspaceID: issue.WorkspaceID,
		UserID:      parseUUID(userID),
		IssueID:     issue.ID,
	}
	var changed int64
	var err error
	if unread {
		changed, err = h.Queries.MarkIssueUnreadManually(r.Context(), db.MarkIssueUnreadManuallyParams(params))
	} else {
		changed, err = h.Queries.MarkIssueRead(r.Context(), params)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update unread state")
		return
	}

	if changed > 0 {
		workspaceID := uuidToString(issue.WorkspaceID)
		h.publish(protocol.EventIssueUnreadChanged, workspaceID, "member", userID, map[string]any{
			"workspace_id": workspaceID,
			"recipient_id": userID,
			"issue_id":     uuidToString(issue.ID),
			"unread":       unread,
		})
	}
	writeJSON(w, http.StatusOK, map[string]bool{"unread": unread})
}
