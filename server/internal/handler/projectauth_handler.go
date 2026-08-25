package handler

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/projectauth"
)

func issueProjectVisibilityPredicate(issueAlias, workspaceRef, userRef string) string {
	return fmt.Sprintf(`(%s.project_id IS NOT NULL AND (
		EXISTS (SELECT 1 FROM member m WHERE m.workspace_id = %s AND m.user_id = %s::uuid AND m.role IN ('owner', 'admin'))
		OR EXISTS (SELECT 1 FROM project_members pm WHERE pm.project_id = %s.project_id AND pm.user_id = %s::uuid)
	))`, issueAlias, workspaceRef, userRef, issueAlias, userRef)
}

// 2026-08-24 coder(lq): Keep HTTP/error mapping in this thin adapter so the
// independent projectauth policy stays free of chi and generated DB models.
func (h *Handler) requireProjectPermission(w http.ResponseWriter, r *http.Request, projectID, workspaceID string, permission projectauth.Permission) bool {
	if h.ProjectAuth == nil || !h.ProjectAuth.Enabled() {
		return true
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return false
	}
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return false
	}
	member, err := h.getWorkspaceMember(r.Context(), userID, workspaceID)
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return false
	}
	subject := projectauth.Subject{
		UserID:        userID,
		WorkspaceID:   workspaceID,
		WorkspaceRole: projectauth.WorkspaceRole(member.Role),
	}
	if err := h.ProjectAuth.Require(r.Context(), subject, projectID, permission); err != nil {
		// Project membership is intentionally indistinguishable from a missing
		// project to avoid leaking project IDs across the workspace boundary.
		if errors.Is(err, projectauth.ErrNotWorkspaceMember) || errors.Is(err, projectauth.ErrNoProjectAccess) || errors.Is(err, projectauth.ErrForbidden) {
			writeError(w, http.StatusNotFound, "project not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to check project permissions")
		}
		return false
	}
	return true
}

// 2026-08-24 coder(lq): Make task authorization inherit the issue's project;
// projectless issues are rejected while the project-permission overlay is on.
func (h *Handler) requireIssueProjectPermission(w http.ResponseWriter, r *http.Request, issue db.Issue, permission projectauth.Permission) bool {
	ok, reason := h.issueProjectAllowed(r, issue, permission)
	if ok {
		return true
	}
	if reason == "projectless" {
		writeError(w, http.StatusNotFound, "task is not attached to a project")
	} else if reason == "internal" {
		writeError(w, http.StatusInternalServerError, "failed to check project permissions")
	} else {
		writeError(w, http.StatusNotFound, "project not found")
	}
	return false
}

// 2026-08-24 coder(lq): Allow list handlers to filter unauthorized tasks
// without writing an HTTP response halfway through a successful page.
func (h *Handler) issueProjectAllowed(r *http.Request, issue db.Issue, permission projectauth.Permission) (bool, string) {
	if h.ProjectAuth == nil || !h.ProjectAuth.Enabled() {
		return true, ""
	}
	if !issue.ProjectID.Valid {
		return false, "projectless"
	}
	userID := requestUserID(r)
	if userID == "" {
		return false, "denied"
	}
	member, err := h.getWorkspaceMember(r.Context(), userID, uuidToString(issue.WorkspaceID))
	if err != nil {
		return false, "denied"
	}
	subject := projectauth.Subject{UserID: userID, WorkspaceID: uuidToString(issue.WorkspaceID), WorkspaceRole: projectauth.WorkspaceRole(member.Role)}
	err = h.ProjectAuth.CheckIssue(r.Context(), subject, uuidToString(issue.ID), uuidToString(issue.ProjectID), permission)
	if err != nil {
		if errors.Is(err, projectauth.ErrDisabled) {
			return false, "internal"
		}
		return false, "denied"
	}
	return true, ""
}

// 2026-08-24 coder(lq): Agent runs are task side effects, so they inherit the
// same project boundary as the issue and additionally require AgentUse.
func (h *Handler) issueAgentAllowed(r *http.Request, issue db.Issue) bool {
	if h.ProjectAuth == nil || !h.ProjectAuth.Enabled() {
		return true
	}
	allowed, _ := h.issueProjectAllowed(r, issue, projectauth.AgentUse)
	return allowed
}

func (h *Handler) requireNewIssueProjectPermission(w http.ResponseWriter, r *http.Request, workspaceID string, projectID pgtype.UUID, permission projectauth.Permission) bool {
	if h.ProjectAuth == nil || !h.ProjectAuth.Enabled() {
		return true
	}
	if !projectID.Valid {
		writeError(w, http.StatusBadRequest, "project_id is required")
		return false
	}
	return h.requireProjectPermission(w, r, uuidToString(projectID), workspaceID, permission)
}

// 2026-08-24 coder(lq): Parent-child links are task access edges too. When the
// overlay is enabled, a parent must be visible to the caller and both tasks
// must stay in the same project; otherwise a user could use a visible child
// to discover or mutate an unrelated project's task tree.
func (h *Handler) requireParentIssueProjectPermission(w http.ResponseWriter, r *http.Request, parent db.Issue, projectID pgtype.UUID) bool {
	if h.ProjectAuth == nil || !h.ProjectAuth.Enabled() {
		return true
	}
	if !projectID.Valid || !parent.ProjectID.Valid || parent.ProjectID != projectID {
		writeError(w, http.StatusBadRequest, "parent issue must belong to the same project")
		return false
	}
	return h.requireIssueProjectPermission(w, r, parent, projectauth.View)
}
