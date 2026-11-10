package handler

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ListIssueDeliverables returns every live comment's attachments independently
// of the bounded timeline, without transferring historical comment bodies.
func (h *Handler) ListIssueDeliverables(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	attachments, err := h.Queries.ListAttachmentsByIssue(r.Context(), db.ListAttachmentsByIssueParams{IssueID: issue.ID, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		slog.Error("failed to list deliverables", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list deliverables")
		return
	}
	ids := make([]pgtype.UUID, 0)
	seen := make(map[pgtype.UUID]bool)
	byComment := make(map[string][]AttachmentResponse)
	mode := attachmentURLModeFromRequest(r)
	for _, a := range attachments {
		if !a.CommentID.Valid {
			continue
		}
		if !seen[a.CommentID] {
			ids = append(ids, a.CommentID)
			seen[a.CommentID] = true
		}
		key := uuidToString(a.CommentID)
		byComment[key] = append(byComment[key], h.attachmentToResponse(a, mode))
	}
	if len(ids) == 0 {
		writeJSON(w, http.StatusOK, []TimelineEntry{})
		return
	}
	comments, err := h.Queries.ListCommentsByIDsForIssue(r.Context(), db.ListCommentsByIDsForIssueParams{Ids: ids, IssueID: issue.ID, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		slog.Error("failed to load deliverable origins", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list deliverables")
		return
	}
	writeJSON(w, http.StatusOK, issueDeliverableEntries(comments, byComment))
}

func issueDeliverableEntries(comments []db.Comment, attachments map[string][]AttachmentResponse) []TimelineEntry {
	entries := make([]TimelineEntry, 0, len(comments))
	for _, c := range comments {
		id := uuidToString(c.ID)
		if c.DeletedAt.Valid || len(attachments[id]) == 0 {
			continue
		}
		entry := TimelineEntry{Type: "comment", ID: id, ActorType: c.AuthorType, ActorID: uuidToString(c.AuthorID), CreatedAt: c.CreatedAt.Time.Format("2006-01-02T15:04:05Z07:00"), Attachments: attachments[id]}
		if c.ParentID.Valid {
			parent := uuidToString(c.ParentID)
			entry.ParentID = &parent
		}
		entries = append(entries, entry)
	}
	return entries
}
