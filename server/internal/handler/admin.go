package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// AdminUserResponse is the JSON shape returned for admin user-management endpoints.
type AdminUserResponse struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Email     string  `json:"email"`
	AvatarURL *string `json:"avatar_url"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

func adminUserRowToResponse(u db.ListUsersRow) AdminUserResponse {
	return AdminUserResponse{
		ID:        uuidToString(u.ID),
		Name:      u.Name,
		Email:     u.Email,
		AvatarURL: textToPtr(u.AvatarUrl),
		CreatedAt: timestampToString(u.CreatedAt),
		UpdatedAt: timestampToString(u.UpdatedAt),
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
		Search: search,
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list users")
		return
	}

	resp := make([]AdminUserResponse, len(rows))
	for i, row := range rows {
		resp[i] = adminUserRowToResponse(row)
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
