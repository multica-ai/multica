package handler

// CEREBRO-PATCH(project-access-handler): cerebro modification of upstream file

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/pkg/protocol"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ProjectMemberResponse is the wire shape for a project member row.
type ProjectMemberResponse struct {
	UserID    string  `json:"user_id"`
	Name      string  `json:"name"`
	Email     string  `json:"email"`
	AvatarURL *string `json:"avatar_url"`
	AddedAt   string  `json:"added_at"`
}

// UpdateProjectAccess flips a project between 'workspace' and 'restricted'.
// Admin-only — restricting access is a governance decision, not a member task.
func (h *Handler) UpdateProjectAccess(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)

	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "project not found", "owner", "admin")
	if !ok {
		return
	}
	_ = member

	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	var req struct {
		Access string `json:"access"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Access != accessWorkspace && req.Access != accessRestricted {
		writeError(w, http.StatusBadRequest, "access must be 'workspace' or 'restricted'")
		return
	}

	updated, err := h.Queries.UpdateProjectAccess(r.Context(), db.UpdateProjectAccessParams{
		ID:     project.ID,
		Access: req.Access,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update project access")
		return
	}

	slog.Info("project access updated",
		append(logger.RequestAttrs(r),
			"project_id", id,
			"old_access", project.Access,
			"new_access", req.Access,
		)...)

	// Compute audience: when going restricted, only members + admins should
	// see this event so others lose visibility immediately. When going open,
	// the event needs to reach everyone so they can rediscover the project.
	var audience []string
	if req.Access == accessRestricted {
		audience = h.audienceForRestrictedProject(r.Context(), updated)
	}
	h.publishToAudience("project:access_changed", workspaceID, "member", uuidToString(member.UserID), map[string]any{
		"project_id": id,
		"access":     req.Access,
	}, audience)

	writeJSON(w, http.StatusOK, projectToResponse(updated))
}

// ListProjectMembers returns the explicit member list for a restricted project.
// Anyone with read access to the project can see who's in it. (Hiding the
// member list adds complexity without security value — if you can see the
// project you already know the people who can see it from comments etc.)
func (h *Handler) ListProjectMembers(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)

	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	if !h.canAccessProject(r.Context(), member, project) {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	rows, err := h.Queries.ListProjectMembers(r.Context(), project.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list project members")
		return
	}
	resp := make([]ProjectMemberResponse, len(rows))
	for i, row := range rows {
		resp[i] = ProjectMemberResponse{
			UserID:    uuidToString(row.UserID),
			Name:      row.Name,
			Email:     row.Email,
			AvatarURL: textToPtr(row.AvatarUrl),
			AddedAt:   timestampToString(row.AddedAt),
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": resp})
}

// AddProjectMember adds a workspace member to a restricted project.
// Admin/owner only. The added user must already be a workspace member —
// this endpoint never invites; it only grants project access.
func (h *Handler) AddProjectMember(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)

	requester, ok := h.requireWorkspaceRole(w, r, workspaceID, "project not found", "owner", "admin")
	if !ok {
		return
	}

	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	var req struct {
		UserID string `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.UserID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}

	// Confirm the target is a member of this workspace.
	if _, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      parseUUID(req.UserID),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		writeError(w, http.StatusBadRequest, "user is not a member of this workspace")
		return
	}

	row, err := h.Queries.AddProjectMember(r.Context(), db.AddProjectMemberParams{
		WorkspaceID: project.WorkspaceID,
		ProjectID:   project.ID,
		UserID:      parseUUID(req.UserID),
		AddedBy:     requester.UserID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add project member")
		return
	}

	slog.Info("project member added", append(logger.RequestAttrs(r), "project_id", id, "user_id", req.UserID)...)

	// Audience update: the newly added user is now in the project's audience,
	// so they should receive the membership-added event. Send to the whole
	// new audience so admins also see the list change.
	audience := h.audienceForRestrictedProject(r.Context(), project)
	h.publishToAudience("project:member_added", workspaceID, "member", uuidToString(requester.UserID), map[string]any{
		"project_id": id,
		"user_id":    req.UserID,
	}, audience)

	writeJSON(w, http.StatusOK, map[string]any{"member": row})
}

// ListMemberProjects returns every restricted project a workspace member
// belongs to. Used by the workspace member detail page (Projects tab) to
// answer "what does this person actually have access to?". Admin-only —
// surfacing membership context is a governance concern.
func (h *Handler) ListMemberProjects(w http.ResponseWriter, r *http.Request) {
	memberID := chi.URLParam(r, "memberId")
	workspaceID := chi.URLParam(r, "id")

	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}

	target, err := h.Queries.GetMember(r.Context(), parseUUID(memberID))
	if err != nil || uuidToString(target.WorkspaceID) != workspaceID {
		writeError(w, http.StatusNotFound, "member not found")
		return
	}

	rows, err := h.Queries.ListProjectMembershipsForUser(r.Context(), db.ListProjectMembershipsForUserParams{
		WorkspaceID: parseUUID(workspaceID),
		UserID:      target.UserID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list member projects")
		return
	}

	type row struct {
		ID      string  `json:"id"`
		Title   string  `json:"title"`
		Icon    *string `json:"icon"`
		Color   *string `json:"color"`
		AddedAt string  `json:"added_at"`
	}
	out := make([]row, len(rows))
	for i, r := range rows {
		out[i] = row{
			ID:      uuidToString(r.ID),
			Title:   r.Title,
			Icon:    textToPtr(r.Icon),
			Color:   textToPtr(r.Color),
			AddedAt: timestampToString(r.AddedAt),
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": out})
}

// RemoveProjectMember revokes a member's explicit project access. Admin-only.
// Workspace admins/owners can never be removed via this endpoint — their
// access comes from the role, not from project_member.
func (h *Handler) RemoveProjectMember(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	memberUserID := chi.URLParam(r, "userId")
	workspaceID := h.resolveWorkspaceID(r)

	requester, ok := h.requireWorkspaceRole(w, r, workspaceID, "project not found", "owner", "admin")
	if !ok {
		return
	}

	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	// Audience BEFORE removal — contains the user about to lose access. We
	// need them to receive the event so their client can drop tabs / refresh.
	audience := h.audienceForRestrictedProject(r.Context(), project)

	if err := h.Queries.RemoveProjectMember(r.Context(), db.RemoveProjectMemberParams{
		ProjectID: project.ID,
		UserID:    parseUUID(memberUserID),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove project member")
		return
	}

	slog.Info("project member removed",
		append(logger.RequestAttrs(r), "project_id", id, "user_id", memberUserID)...)

	h.publishToAudience("project:member_removed", workspaceID, "member", uuidToString(requester.UserID), map[string]any{
		"project_id": id,
		"user_id":    memberUserID,
	}, audience)

	w.WriteHeader(http.StatusNoContent)
	_ = protocol.EventIssueUpdated // keep import
}
