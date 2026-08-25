package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/pkg/projectauth"
)

type projectMemberRequest struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}

func (h *Handler) projectSubject(w http.ResponseWriter, r *http.Request, projectID string) (projectauth.Subject, bool) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return projectauth.Subject{}, false
	}
	workspaceID := h.resolveWorkspaceID(r)
	member, err := h.getWorkspaceMember(r.Context(), userID, workspaceID)
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return projectauth.Subject{}, false
	}
	return projectauth.Subject{UserID: userID, WorkspaceID: workspaceID, WorkspaceRole: projectauth.WorkspaceRole(member.Role)}, true
}

func (h *Handler) ListProjectMembers(w http.ResponseWriter, r *http.Request) {
	if h.ProjectAuth == nil || !h.ProjectAuth.Enabled() {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	projectID := chi.URLParam(r, "id")
	subject, ok := h.projectSubject(w, r, projectID)
	if !ok {
		return
	}
	members, err := h.ProjectAuth.ListMembers(r.Context(), subject, projectID)
	if err != nil {
		writeProjectAuthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": members, "total": len(members)})
}

func (h *Handler) AddProjectMember(w http.ResponseWriter, r *http.Request) {
	if h.ProjectAuth == nil || !h.ProjectAuth.Enabled() {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	projectID := chi.URLParam(r, "id")
	subject, ok := h.projectSubject(w, r, projectID)
	if !ok {
		return
	}
	var req projectMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.UserID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	if err := h.ProjectAuth.AddMember(r.Context(), subject, projectID, req.UserID, projectauth.ProjectRole(req.Role)); err != nil {
		writeProjectAuthError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) RemoveProjectMember(w http.ResponseWriter, r *http.Request) {
	if h.ProjectAuth == nil || !h.ProjectAuth.Enabled() {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	projectID := chi.URLParam(r, "id")
	subject, ok := h.projectSubject(w, r, projectID)
	if !ok {
		return
	}
	if err := h.ProjectAuth.RemoveMember(r.Context(), subject, projectID, chi.URLParam(r, "userId")); err != nil {
		writeProjectAuthError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeProjectAuthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, projectauth.ErrInvalidRole):
		writeError(w, http.StatusBadRequest, "invalid project role")
	case errors.Is(err, projectauth.ErrInvalidIssuePermission):
		writeError(w, http.StatusBadRequest, "invalid issue permission")
	case errors.Is(err, projectauth.ErrForbidden):
		writeError(w, http.StatusForbidden, "insufficient project permissions")
	case errors.Is(err, projectauth.ErrNotWorkspaceMember), errors.Is(err, projectauth.ErrNoProjectAccess), errors.Is(err, projectauth.ErrCrossWorkspace):
		writeError(w, http.StatusNotFound, "project not found")
	default:
		writeError(w, http.StatusInternalServerError, "failed to update project members")
	}
}
