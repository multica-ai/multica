package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/multica-ai/multica/server/pkg/projectauth"
)

// 2026-08-24 coder(lq): Expose the permission report as a read-only endpoint
// in the workspace-scoped route group. Filter parsing stays at the HTTP edge;
// authorization and effective-permission rules remain in projectauth.Service.
func (h *Handler) ListPermissionReport(w http.ResponseWriter, r *http.Request) {
	if h.ProjectAuth == nil || !h.ProjectAuth.Enabled() {
		writeError(w, http.StatusNotFound, "permission report not found")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	member, err := h.getWorkspaceMember(r.Context(), userID, workspaceID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}

	q := r.URL.Query()
	filter := projectauth.PermissionReportFilter{
		WorkspaceID: workspaceID,
		ProjectID:   q.Get("project_id"),
		IssueID:     q.Get("issue_id"),
		UserID:      q.Get("user_id"),
		Role:        q.Get("role"),
		Permission:  projectauth.Permission(q.Get("permission")),
		Scope:       q.Get("scope"),
	}
	if raw := q.Get("limit"); raw != "" {
		filter.Limit, err = strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "limit must be an integer")
			return
		}
	}
	if raw := q.Get("offset"); raw != "" {
		filter.Offset, err = strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "offset must be an integer")
			return
		}
	}
	if filter.Limit <= 0 || filter.Limit > 1000 {
		filter.Limit = 1000
	}

	subject := projectauth.Subject{
		UserID:        userID,
		WorkspaceID:   workspaceID,
		WorkspaceRole: projectauth.WorkspaceRole(member.Role),
	}
	result, err := h.ProjectAuth.ListPermissionReport(r.Context(), subject, filter)
	if err != nil {
		switch {
		case errors.Is(err, projectauth.ErrInvalidReportFilter):
			writeError(w, http.StatusBadRequest, "invalid permission report filter")
		case errors.Is(err, projectauth.ErrForbidden):
			writeError(w, http.StatusForbidden, "insufficient project permissions")
		case errors.Is(err, projectauth.ErrNotWorkspaceMember), errors.Is(err, projectauth.ErrNoProjectAccess), errors.Is(err, projectauth.ErrCrossWorkspace):
			writeError(w, http.StatusNotFound, "project not found")
		default:
			writeError(w, http.StatusInternalServerError, "failed to load permission report")
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"rows":   result.Rows,
		"total":  result.Total,
		"limit":  filter.Limit,
		"offset": filter.Offset,
	})
}
