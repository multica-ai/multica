package handler

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func (h *Handler) GetIssue(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return
	}
	prefix := h.getIssuePrefix(r.Context(), issue.WorkspaceID)
	resp := issueToResponse(issue, prefix)
	detailLabels := h.labelsByIssue(r.Context(), issue.WorkspaceID, []pgtype.UUID{issue.ID})[uuidToString(issue.ID)]
	if detailLabels == nil {
		detailLabels = []LabelResponse{}
	}
	resp.Labels = &detailLabels

	// Fetch issue reactions.
	reactions, err := h.Queries.ListIssueReactions(r.Context(), issue.ID)
	if err == nil && len(reactions) > 0 {
		resp.Reactions = make([]IssueReactionResponse, len(reactions))
		for i, rx := range reactions {
			resp.Reactions[i] = issueReactionToResponse(rx)
		}
	}

	// Fetch issue-level attachments.
	attachments, err := h.Queries.ListAttachmentsByIssue(r.Context(), db.ListAttachmentsByIssueParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err == nil && len(attachments) > 0 {
		resp.Attachments = make([]AttachmentResponse, len(attachments))
		for i, a := range attachments {
			resp.Attachments[i] = h.attachmentToResponse(a)
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ListChildIssues(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return
	}
	children, err := h.Queries.ListChildIssues(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list child issues")
		return
	}
	prefix := h.getIssuePrefix(r.Context(), issue.WorkspaceID)
	resp := make([]IssueResponse, len(children))
	for i, child := range children {
		resp[i] = issueToResponse(child, prefix)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"issues": resp,
	})
}

// Cap on the number of parents we'll fan-out children for in one request.
// Swimlane's visible-lane count is naturally bounded by what fits on screen
// (typically <= 50), but cap explicitly so a malicious caller can't ANY()
// across the whole workspace's issue set in a single round trip.
const listChildrenByParentsLimit = 200

// ListChildrenByParents returns the union of children for the
// provided parent ids. Replaces the N-call fan-out Swimlane would otherwise
// have to make on mount (one /issues/:id/children per visible parent lane).
//
// Workspace scope is enforced at the query level — any parent_id that doesn't
// belong to the caller's workspace simply yields zero children, so callers
// can't probe parents across workspace boundaries.
func (h *Handler) ListChildrenByParents(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	raw := r.URL.Query().Get("parent_ids")
	if raw == "" {
		// Empty input is a no-op response (not an error) — simplifies the
		// client which calls this unconditionally on Swimlane mount even
		// when there are zero visible parent lanes.
		writeJSON(w, http.StatusOK, map[string]any{"issues": []IssueResponse{}})
		return
	}

	parts := strings.Split(raw, ",")
	if len(parts) > listChildrenByParentsLimit {
		writeError(w, http.StatusBadRequest, "too many parent_ids")
		return
	}
	parentIDs := make([]pgtype.UUID, 0, len(parts))
	for _, s := range parts {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		id, ok := parseUUIDOrBadRequest(w, s, "parent_ids")
		if !ok {
			return
		}
		parentIDs = append(parentIDs, id)
	}
	if len(parentIDs) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"issues": []IssueResponse{}})
		return
	}

	children, err := h.Queries.ListChildrenByParents(r.Context(), db.ListChildrenByParentsParams{
		WorkspaceID: wsUUID,
		ParentIds:   parentIDs,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list child issues")
		return
	}
	prefix := h.getIssuePrefix(r.Context(), wsUUID)
	resp := make([]IssueResponse, len(children))
	for i, child := range children {
		resp[i] = issueToResponse(child, prefix)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"issues": resp,
	})
}

func (h *Handler) ChildIssueProgress(w http.ResponseWriter, r *http.Request) {
	wsID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace_id")
	if !ok {
		return
	}

	rows, err := h.Queries.ChildIssueProgress(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get child issue progress")
		return
	}

	type progressEntry struct {
		ParentIssueID string `json:"parent_issue_id"`
		Total         int64  `json:"total"`
		Done          int64  `json:"done"`
	}
	resp := make([]progressEntry, len(rows))
	for i, row := range rows {
		resp[i] = progressEntry{
			ParentIssueID: uuidToString(row.ParentIssueID),
			Total:         row.Total,
			Done:          row.Done,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"progress": resp,
	})
}
