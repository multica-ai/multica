package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/pkg/projectauth"
)

type issuePermissionRequest struct {
	UserID     string `json:"user_id"`
	Permission string `json:"permission"`
}

func (h *Handler) issueSubject(w http.ResponseWriter, r *http.Request, issueWorkspaceID string) (projectauth.Subject, bool) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return projectauth.Subject{}, false
	}
	member, err := h.getWorkspaceMember(r.Context(), userID, issueWorkspaceID)
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return projectauth.Subject{}, false
	}
	return projectauth.Subject{
		UserID:        userID,
		WorkspaceID:   issueWorkspaceID,
		WorkspaceRole: projectauth.WorkspaceRole(member.Role),
	}, true
}

func (h *Handler) ListIssuePermissions(w http.ResponseWriter, r *http.Request) {
	if h.ProjectAuth == nil || !h.ProjectAuth.Enabled() {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if !issue.ProjectID.Valid {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}
	subject, ok := h.issueSubject(w, r, uuidToString(issue.WorkspaceID))
	if !ok {
		return
	}
	permissions, err := h.ProjectAuth.ListIssuePermissions(r.Context(), subject, uuidToString(issue.ID), uuidToString(issue.ProjectID))
	if err != nil {
		writeProjectAuthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"permissions": permissions, "total": len(permissions)})
}

func (h *Handler) GrantIssuePermission(w http.ResponseWriter, r *http.Request) {
	if h.ProjectAuth == nil || !h.ProjectAuth.Enabled() {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if !issue.ProjectID.Valid {
		writeError(w, http.StatusBadRequest, "task must be attached to a project")
		return
	}
	subject, ok := h.issueSubject(w, r, uuidToString(issue.WorkspaceID))
	if !ok {
		return
	}
	var req issuePermissionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.UserID == "" || req.Permission == "" {
		writeError(w, http.StatusBadRequest, "user_id and permission are required")
		return
	}
	if err := h.ProjectAuth.GrantIssuePermission(r.Context(), subject, uuidToString(issue.ID), uuidToString(issue.ProjectID), req.UserID, projectauth.Permission(req.Permission)); err != nil {
		writeProjectAuthError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) RevokeIssuePermission(w http.ResponseWriter, r *http.Request) {
	if h.ProjectAuth == nil || !h.ProjectAuth.Enabled() {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if !issue.ProjectID.Valid {
		writeError(w, http.StatusBadRequest, "task must be attached to a project")
		return
	}
	subject, ok := h.issueSubject(w, r, uuidToString(issue.WorkspaceID))
	if !ok {
		return
	}
	permission := projectauth.Permission(chi.URLParam(r, "permission"))
	if err := h.ProjectAuth.RevokeIssuePermission(r.Context(), subject, uuidToString(issue.ID), uuidToString(issue.ProjectID), chi.URLParam(r, "userId"), permission); err != nil {
		writeProjectAuthError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
