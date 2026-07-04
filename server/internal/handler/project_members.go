package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type ProjectMemberResponse struct {
	ProjectID string  `json:"project_id"`
	MemberID  string  `json:"member_id"`
	Role      string  `json:"role"`
	InvitedAt string  `json:"invited_at"`
	InvitedBy string  `json:"invited_by"`
	Email     string  `json:"email"`
	Name      string  `json:"name"`
	AvatarUrl *string `json:"avatar_url"`
}

func listProjectMemberToResponse(m db.ListProjectMembersRow) ProjectMemberResponse {
	return ProjectMemberResponse{
		ProjectID: uuidToString(m.ProjectID),
		MemberID:  uuidToString(m.MemberID),
		Role:      m.Role,
		InvitedAt: timestampToString(m.InvitedAt),
		InvitedBy: uuidToString(m.InvitedBy),
		Email:     m.Email,
		Name:      m.Name,
		AvatarUrl: textToPtr(m.AvatarUrl),
	}
}

func (h *Handler) ListProjectMembers(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id"); !ok {
		return
	}
	id := chi.URLParam(r, "id")
	projectID, ok := parseUUIDOrBadRequest(w, id, "project id")
	if !ok {
		return
	}

	members, err := h.Queries.ListProjectMembers(r.Context(), projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list project members")
		return
	}

	resp := make([]ProjectMemberResponse, len(members))
	for i, m := range members {
		resp[i] = listProjectMemberToResponse(m)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) AddProjectMember(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id"); !ok {
		return
	}
	id := chi.URLParam(r, "id")
	projectID, ok := parseUUIDOrBadRequest(w, id, "project id")
	if !ok {
		return
	}
	
	requester, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin", "member")
	if !ok {
		return
	}

	var req struct {
		MemberID string `json:"member_id"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.MemberID == "" {
		writeError(w, http.StatusBadRequest, "member_id is required")
		return
	}
	if req.Role == "" {
		req.Role = "member"
	}

	memberUUID, ok := parseUUIDOrBadRequest(w, req.MemberID, "member_id")
	if !ok {
		return
	}

	sm, err := h.Queries.AddProjectMember(r.Context(), db.AddProjectMemberParams{
		ProjectID: projectID,
		MemberID:  memberUUID,
		Role:      req.Role,
		InvitedBy: requester.ID,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "member already in project")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to add project member")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"project_id": uuidToString(sm.ProjectID),
		"member_id":  uuidToString(sm.MemberID),
		"role":       sm.Role,
		"invited_at": timestampToString(sm.InvitedAt),
		"invited_by": uuidToString(sm.InvitedBy),
	})
	
	h.publish(protocol.EventProjectMemberAdded, workspaceID, "member", uuidToString(requester.UserID), map[string]any{
		"project_id": uuidToString(projectID),
		"member_id":  uuidToString(memberUUID),
	})
}

func (h *Handler) UpdateProjectMember(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id"); !ok {
		return
	}
	id := chi.URLParam(r, "id")
	projectID, ok := parseUUIDOrBadRequest(w, id, "project id")
	if !ok {
		return
	}
	memberIdStr := chi.URLParam(r, "memberId")
	memberUUID, ok := parseUUIDOrBadRequest(w, memberIdStr, "member id")
	if !ok {
		return
	}

	requester, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin", "member")
	if !ok {
		return
	}

	var req struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Role == "" {
		writeError(w, http.StatusBadRequest, "role is required")
		return
	}

	sm, err := h.Queries.UpdateProjectMemberRole(r.Context(), db.UpdateProjectMemberRoleParams{
		ProjectID: projectID,
		MemberID:  memberUUID,
		Role:      req.Role,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update project member")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"project_id": uuidToString(sm.ProjectID),
		"member_id":  uuidToString(sm.MemberID),
		"role":       sm.Role,
		"invited_at": timestampToString(sm.InvitedAt),
		"invited_by": uuidToString(sm.InvitedBy),
	})
	
	h.publish(protocol.EventProjectMemberUpdated, workspaceID, "member", uuidToString(requester.UserID), map[string]any{
		"project_id": uuidToString(projectID),
		"member_id":  uuidToString(memberUUID),
	})
}

func (h *Handler) RemoveProjectMember(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id"); !ok {
		return
	}
	id := chi.URLParam(r, "id")
	projectID, ok := parseUUIDOrBadRequest(w, id, "project id")
	if !ok {
		return
	}
	memberIdStr := chi.URLParam(r, "memberId")
	memberUUID, ok := parseUUIDOrBadRequest(w, memberIdStr, "member id")
	if !ok {
		return
	}

	requester, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin", "member")
	if !ok {
		return
	}

	err := h.Queries.RemoveProjectMember(r.Context(), db.RemoveProjectMemberParams{
		ProjectID: projectID,
		MemberID:  memberUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove project member")
		return
	}

	w.WriteHeader(http.StatusNoContent)
	
	h.publish(protocol.EventProjectMemberRemoved, workspaceID, "member", uuidToString(requester.UserID), map[string]any{
		"project_id": uuidToString(projectID),
		"member_id":  uuidToString(memberUUID),
	})
}
