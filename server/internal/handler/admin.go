package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// AdminUserResponse is the JSON shape returned for admin user-management endpoints.
type AdminUserResponse struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name"`
	Email      string                 `json:"email"`
	AvatarURL  *string                `json:"avatar_url"`
	CreatedAt  string                 `json:"created_at"`
	UpdatedAt  string                 `json:"updated_at"`
	Workspaces []AdminUserWorkspace   `json:"workspaces"`
}

// AdminUserWorkspace represents a single workspace membership for an admin user response.
type AdminUserWorkspace struct {
	WorkspaceID   string `json:"workspace_id"`
	WorkspaceName string `json:"workspace_name"`
	WorkspaceSlug string `json:"workspace_slug"`
	Role          string `json:"role"`
	MemberID      string `json:"member_id"`
}

func adminUserRowToResponse(u db.ListUsersRow, workspaces []db.ListUserWorkspacesRow) AdminUserResponse {
	ws := make([]AdminUserWorkspace, len(workspaces))
	for i, w := range workspaces {
		ws[i] = AdminUserWorkspace{
			WorkspaceID:   uuidToString(w.WorkspaceID),
			WorkspaceName: w.WorkspaceName,
			WorkspaceSlug: w.WorkspaceSlug,
			Role:          w.Role,
			MemberID:      uuidToString(w.MemberID),
		}
	}
	return AdminUserResponse{
		ID:         uuidToString(u.ID),
		Name:       u.Name,
		Email:      u.Email,
		AvatarURL:  textToPtr(u.AvatarUrl),
		CreatedAt:  timestampToString(u.CreatedAt),
		UpdatedAt:  timestampToString(u.UpdatedAt),
		Workspaces: ws,
	}
}

// AdminListUsers returns all users with optional search filtering.
// Requires super-admin authentication.
// GET /api/admin/users?search=<term>&limit=<n>&offset=<n>
func (h *Handler) AdminListUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireSuperAdmin(w, r); !ok {
		return
	}

	search := strings.TrimSpace(r.URL.Query().Get("search"))
	limit := int32(parseQueryInt(r.URL.Query().Get("limit"), 100))
	offset := int32(parseQueryInt(r.URL.Query().Get("offset"), 0))

	rows, err := h.Queries.ListUsers(r.Context(), db.ListUsersParams{
		Column1: search,
		Limit:   limit,
		Offset:  offset,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list users")
		return
	}

	resp := make([]AdminUserResponse, len(rows))
	for i, row := range rows {
		workspaces, _ := h.Queries.ListUserWorkspaces(r.Context(), row.ID)
		resp[i] = adminUserRowToResponse(row, workspaces)
	}
	writeJSON(w, http.StatusOK, resp)
}

// AdminListWorkspaces returns ALL workspaces (super-admin only).
// GET /api/admin/workspaces
func (h *Handler) AdminListWorkspaces(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireSuperAdmin(w, r); !ok {
		return
	}

	workspaces, err := h.Queries.ListAllWorkspaces(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list workspaces")
		return
	}

	type workspaceSummary struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	resp := make([]workspaceSummary, len(workspaces))
	for i, w := range workspaces {
		resp[i] = workspaceSummary{
			ID:   uuidToString(w.ID),
			Name: w.Name,
			Slug: w.Slug,
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// AdminUpdateUserRequest is the request body for PATCH /api/admin/users/{id}.
type AdminUpdateUserRequest struct {
	Name string `json:"name"`
}

// AdminUpdateUser updates the display name of any user.
// Requires super-admin authentication.
// PATCH /api/admin/users/{id}
func (h *Handler) AdminUpdateUser(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.requireSuperAdmin(w, r)
	if !ok {
		return
	}

	targetID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return
	}

	var req AdminUpdateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	// Fetch the target user first so we can log the old name.
	target, err := h.Queries.GetUser(r.Context(), targetID)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}

	updated, err := h.Queries.AdminUpdateUserName(r.Context(), db.AdminUpdateUserNameParams{
		ID:   targetID,
		Name: name,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update user")
		return
	}

	// Minimal audit trail until a full audit-log table is added.
	slog.Info("admin renamed user",
		"actor_id", uuidToString(actor.ID),
		"actor_email", actor.Email,
		"target_id", uuidToString(target.ID),
		"target_email", target.Email,
		"old_name", target.Name,
		"new_name", updated.Name,
	)

	writeJSON(w, http.StatusOK, userToResponse(updated))
}

// envPositiveIntDefault parses a query string as a positive integer, returning
// the default value when the string is empty or invalid.
func parseQueryInt(s string, def int) int {
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil || v <= 0 {
		return def
	}
	return v
}

// ---- Workspace membership management ----

// AdminAddUserToWorkspaceRequest is the request body for POST /api/admin/users/{id}/workspaces.
type AdminAddUserToWorkspaceRequest struct {
	WorkspaceIDs []string `json:"workspace_ids"`
	Role         string   `json:"role"` // "owner" | "admin" | "member"
}

// AdminAddUserToWorkspace adds an existing user to one or more workspaces.
// POST /api/admin/users/{id}/workspaces
func (h *Handler) AdminAddUserToWorkspace(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.requireSuperAdmin(w, r)
	if !ok {
		return
	}

	userID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return
	}

	var req AdminAddUserToWorkspaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(req.WorkspaceIDs) == 0 {
		writeError(w, http.StatusBadRequest, "workspace_ids is required")
		return
	}

	role := req.Role
	if role == "" {
		role = "member"
	}
	if role != "owner" && role != "admin" && role != "member" {
		writeError(w, http.StatusBadRequest, "role must be owner, admin, or member")
		return
	}

	// Verify target user exists.
	_, err := h.Queries.GetUser(r.Context(), userID)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}

	added := 0
	for _, wsIDStr := range req.WorkspaceIDs {
		wsID := parseUUID(wsIDStr)

		// Check if already a member.
		_, err = h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
			UserID:      userID,
			WorkspaceID: wsID,
		})
		if err == nil {
			// Already a member, skip.
			continue
		}

		_, err = h.Queries.CreateMember(r.Context(), db.CreateMemberParams{
			WorkspaceID: wsID,
			UserID:      userID,
			Role:        role,
		})
		if err != nil {
			slog.Warn("admin add user to workspace failed",
				"user_id", uuidToString(userID),
				"workspace_id", wsIDStr,
				"error", err,
			)
			continue
		}
		added++
	}

	slog.Info("admin added user to workspaces",
		"actor_id", uuidToString(actor.ID),
		"actor_email", actor.Email,
		"user_id", uuidToString(userID),
		"added_count", added,
	)

	writeJSON(w, http.StatusOK, map[string]any{"added": added})
}

// AdminRemoveUserFromWorkspace removes a user from a workspace.
// DELETE /api/admin/users/{id}/workspaces/{workspaceId}
func (h *Handler) AdminRemoveUserFromWorkspace(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.requireSuperAdmin(w, r)
	if !ok {
		return
	}

	userID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return
	}

	wsID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "workspaceId"), "workspaceId")
	if !ok {
		return
	}

	member, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      userID,
		WorkspaceID: wsID,
	})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "user is not a member of this workspace")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load membership")
		return
	}

	if err := h.Queries.DeleteMember(r.Context(), member.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove user from workspace")
		return
	}

	slog.Info("admin removed user from workspace",
		"actor_id", uuidToString(actor.ID),
		"user_id", uuidToString(userID),
		"workspace_id", uuidToString(wsID),
	)

	w.WriteHeader(http.StatusNoContent)
}

// AdminUpdateUserRoleRequest is the request body for PATCH /api/admin/users/{id}/workspaces/{workspaceId}.
type AdminUpdateUserRoleRequest struct {
	Role string `json:"role"` // "owner" | "admin" | "member"
}

// AdminUpdateUserRole changes a user's role in a workspace.
// PATCH /api/admin/users/{id}/workspaces/{workspaceId}
func (h *Handler) AdminUpdateUserRole(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.requireSuperAdmin(w, r)
	if !ok {
		return
	}

	userID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return
	}

	wsID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "workspaceId"), "workspaceId")
	if !ok {
		return
	}

	var req AdminUpdateUserRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Role != "owner" && req.Role != "admin" && req.Role != "member" {
		writeError(w, http.StatusBadRequest, "role must be owner, admin, or member")
		return
	}

	member, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      userID,
		WorkspaceID: wsID,
	})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "user is not a member of this workspace")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load membership")
		return
	}

	updated, err := h.Queries.UpdateMemberRole(r.Context(), db.UpdateMemberRoleParams{
		ID:   member.ID,
		Role: req.Role,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update role")
		return
	}

	slog.Info("admin updated user role",
		"actor_id", uuidToString(actor.ID),
		"user_id", uuidToString(userID),
		"workspace_id", uuidToString(wsID),
		"role", req.Role,
	)

	writeJSON(w, http.StatusOK, map[string]any{
		"member_id": uuidToString(updated.ID),
		"role":      updated.Role,
	})
}

// ---- Invitation management ----

// AdminCreateInvitationsRequest is the request body for POST /api/admin/invitations.
type AdminCreateInvitationsRequest struct {
	Email      string   `json:"email"`
	Name       string   `json:"name"`
	Role       string   `json:"role"`
	Workspaces []string `json:"workspaces"` // workspace IDs
}

// AdminCreateInvitations creates invitations for a user (existing or new) to one or more workspaces.
// If a user with the email already exists, they are directly added to the workspace(s).
// Otherwise, pending invitations are created.
// POST /api/admin/invitations
func (h *Handler) AdminCreateInvitations(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.requireSuperAdmin(w, r)
	if !ok {
		return
	}

	var req AdminCreateInvitationsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	email := strings.TrimSpace(req.Email)
	if email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}

	role := req.Role
	if role == "" {
		role = "member"
	}
	if role != "owner" && role != "admin" && role != "member" {
		writeError(w, http.StatusBadRequest, "role must be owner, admin, or member")
		return
	}

	name := strings.TrimSpace(req.Name)

	// Check if user already exists.
	existingUser, userErr := h.Queries.GetUserByEmail(r.Context(), email)

	type invitationResult struct {
		WorkspaceID string `json:"workspace_id"`
		Status      string `json:"status"` // "added" or "invited"
	}
	results := make([]invitationResult, 0, len(req.Workspaces))

	for _, wsIDStr := range req.Workspaces {
		wsID := parseUUID(wsIDStr)

		if userErr == nil {
			// User exists — add directly as member.
			_, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
				UserID:      existingUser.ID,
				WorkspaceID: wsID,
			})
			if err == nil {
				// Already a member.
				continue
			}

			_, err = h.Queries.CreateMember(r.Context(), db.CreateMemberParams{
				WorkspaceID: wsID,
				UserID:      existingUser.ID,
				Role:        role,
			})
			if err != nil {
				slog.Warn("admin add existing user to workspace failed",
					"email", email,
					"workspace_id", wsIDStr,
					"error", err,
				)
				continue
			}
			results = append(results, invitationResult{WorkspaceID: wsIDStr, Status: "added"})
		} else {
			// User doesn't exist — create invitation.
			_, err := h.Queries.AdminCreateInvitation(r.Context(), db.AdminCreateInvitationParams{
				WorkspaceID:  wsID,
				InviterID:    actor.ID,
				InviteeEmail: email,
				InviteeUserID: pgtype.UUID{}, // NULL — user doesn't exist yet
				Role:         role,
				Column6:      name,
			})
			if err != nil {
				slog.Warn("admin create invitation failed",
					"email", email,
					"workspace_id", wsIDStr,
					"error", err,
				)
				continue
			}
			results = append(results, invitationResult{WorkspaceID: wsIDStr, Status: "invited"})
		}
	}

	slog.Info("admin created invitations / added user",
		"actor_id", uuidToString(actor.ID),
		"email", email,
		"user_exists", userErr == nil,
		"results_count", len(results),
	)

	writeJSON(w, http.StatusOK, map[string]any{
		"email":       email,
		"user_exists": userErr == nil,
		"results":     results,
	})
}

// AdminListPendingInvitations returns all pending invitations across all workspaces.
// Requires super-admin authentication.
// GET /api/admin/invitations
func (h *Handler) AdminListPendingInvitations(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireSuperAdmin(w, r); !ok {
		return
	}

	rows, err := h.Queries.ListAllPendingInvitations(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list pending invitations")
		return
	}

	type invitationResponse struct {
		ID           string `json:"id"`
		WorkspaceID  string `json:"workspace_id"`
		WorkspaceName string `json:"workspace_name"`
		WorkspaceSlug string `json:"workspace_slug"`
		InviteeEmail string `json:"invitee_email"`
		InviteeName  string `json:"invitee_name"`
		Role         string `json:"role"`
		InviterName  string `json:"inviter_name"`
		InviterEmail string `json:"inviter_email"`
		CreatedAt    string `json:"created_at"`
		ExpiresAt    string `json:"expires_at"`
	}

	resp := make([]invitationResponse, len(rows))
	for i, row := range rows {
		resp[i] = invitationResponse{
			ID:            uuidToString(row.ID),
			WorkspaceID:   uuidToString(row.WorkspaceID),
			WorkspaceName: row.WorkspaceName,
			WorkspaceSlug: row.WorkspaceSlug,
			InviteeEmail:  row.InviteeEmail,
			InviteeName:   textToPtrString(row.InviteeName),
			Role:          row.Role,
			InviterName:   row.InviterName,
			InviterEmail:  row.InviterEmail,
			CreatedAt:     timestampToString(row.CreatedAt),
			ExpiresAt:     timestampToString(row.ExpiresAt),
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// textToPtrString converts a pgtype.Text to a string, returning empty string for NULL.
func textToPtrString(t pgtype.Text) string {
	if t.Valid {
		return t.String
	}
	return ""
}

// AdminRevokeInvitation revokes a pending invitation.
// DELETE /api/admin/invitations/{id}
func (h *Handler) AdminRevokeInvitation(w http.ResponseWriter, r *http.Request) {
	_, ok := h.requireSuperAdmin(w, r)
	if !ok {
		return
	}

	invID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return
	}

	if err := h.Queries.RevokeInvitation(r.Context(), invID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke invitation")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
