package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ProjectFileResponse is an attachment surfaced in the project files panel. It
// carries the viewer-specific hidden flag on top of the standard attachment
// shape, so the list endpoint can return every project file exactly once with
// a per-viewer hidden bit instead of two lists.
type ProjectFileResponse struct {
	AttachmentResponse
	Hidden bool `json:"hidden"`
}

// ListProjectFiles — GET /api/projects/{id}/files
//
// Returns every attachment that belongs to the project's work — files on its
// issues, on comments of those issues, and on chat sessions linked to the
// project — ordered newest first. `hidden` is computed against the calling
// member, because hiding is a personal view preference (see migration 342).
func (h *Handler) ListProjectFiles(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	rows, err := h.Queries.ListProjectFiles(r.Context(), db.ListProjectFilesParams{
		ProjectID:   project.ID,
		UserID:      parseUUID(userID),
		WorkspaceID: project.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list project files")
		return
	}

	// Collapse duplicate artifacts: an agent can attach one produced file to
	// both an issue and a comment (two rows, one artifact). Rows are ordered
	// created_at DESC, so the first occurrence is the newest and keeps its
	// hidden flag.
	seen := make(map[string]struct{}, len(rows))
	deduped := make([]db.ListProjectFilesRow, 0, len(rows))
	for _, row := range rows {
		key := attachmentDedupeKey(row.Attachment)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, row)
	}

	mode := attachmentURLModeFromRequest(r)
	resp := make([]ProjectFileResponse, len(deduped))
	for i, row := range deduped {
		resp[i] = ProjectFileResponse{
			AttachmentResponse: h.attachmentToResponse(row.Attachment, mode),
			Hidden:             row.Hidden,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": resp, "total": len(resp)})
}

// loadProjectFileAttachment resolves the project and validates that the target
// attachment belongs to that project (and therefore to the project's
// workspace). It is the hide/unhide write gate: an attachment id outside the
// project's work returns 404, so a member cannot hide or unhide a file that
// belongs to another project or workspace.
func (h *Handler) loadProjectFileAttachment(w http.ResponseWriter, r *http.Request, attachmentIDParam string) (db.Project, db.Attachment, bool) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return db.Project{}, db.Attachment{}, false
	}
	attUUID, ok := parseUUIDOrBadRequest(w, attachmentIDParam, "attachment id")
	if !ok {
		return db.Project{}, db.Attachment{}, false
	}
	row, err := h.Queries.GetProjectAttachment(r.Context(), db.GetProjectAttachmentParams{
		AttachmentID: attUUID,
		WorkspaceID:  project.WorkspaceID,
		ProjectID:    project.ID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "file not found")
		return db.Project{}, db.Attachment{}, false
	}
	return project, row.Attachment, true
}

// HideProjectFile — POST /api/projects/{id}/files/{attachmentId}/hide
//
// Idempotent: hiding an already-hidden file is a no-op.
func (h *Handler) HideProjectFile(w http.ResponseWriter, r *http.Request) {
	project, att, ok := h.loadProjectFileAttachment(w, r, chi.URLParam(r, "attachmentId"))
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	if err := h.Queries.HideProjectFile(r.Context(), db.HideProjectFileParams{
		WorkspaceID:  project.WorkspaceID,
		ProjectID:    project.ID,
		AttachmentID: att.ID,
		UserID:       parseUUID(userID),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to hide file")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// UnhideProjectFile — DELETE /api/projects/{id}/files/{attachmentId}/hide
//
// Idempotent: unhiding a file that was never hidden is a no-op.
func (h *Handler) UnhideProjectFile(w http.ResponseWriter, r *http.Request) {
	project, att, ok := h.loadProjectFileAttachment(w, r, chi.URLParam(r, "attachmentId"))
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	if err := h.Queries.UnhideProjectFile(r.Context(), db.UnhideProjectFileParams{
		ProjectID:    project.ID,
		AttachmentID: att.ID,
		UserID:       parseUUID(userID),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unhide file")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
