package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// RepoBindingResponse is the JSON shape for a workspace ↔ GitHub repo binding.
type RepoBindingResponse struct {
	ID             string  `json:"id"`
	WorkspaceID    string  `json:"workspace_id"`
	RepoFullName   string  `json:"repo_full_name"`
	InstallationID int64   `json:"installation_id"`
	CrBotUsername  string  `json:"cr_bot_username"`
	Active         bool    `json:"active"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
}

func repoBindingToResponse(b db.WorkspaceRepoBinding) RepoBindingResponse {
	return RepoBindingResponse{
		ID:             uuidToString(b.ID),
		WorkspaceID:    uuidToString(b.WorkspaceID),
		RepoFullName:   b.RepoFullName,
		InstallationID: b.InstallationID,
		CrBotUsername:  b.CrBotUsername,
		Active:         b.Active,
		CreatedAt:      timestampToString(b.CreatedAt),
		UpdatedAt:      timestampToString(b.UpdatedAt),
	}
}

type CreateRepoBindingRequest struct {
	RepoFullName   string  `json:"repo_full_name"`
	InstallationID int64   `json:"installation_id"`
	CrBotUsername  *string `json:"cr_bot_username"`
}

type UpdateRepoBindingRequest struct {
	InstallationID *int64 `json:"installation_id"`
	Active         *bool  `json:"active"`
}

// ListRepoBindings returns all repo bindings for a workspace.
// Route: GET /api/workspaces/{id}/repo-bindings
// Required role: workspace member (lookup is harmless).
func (h *Handler) ListRepoBindings(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	bindings, err := h.Queries.ListRepoBindingsForWorkspace(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list repo bindings")
		return
	}
	resp := make([]RepoBindingResponse, len(bindings))
	for i, b := range bindings {
		resp[i] = repoBindingToResponse(b)
	}
	writeJSON(w, http.StatusOK, resp)
}

// CreateRepoBinding creates a new workspace ↔ GitHub repo binding.
// Route: POST /api/workspaces/{id}/repo-bindings
// Required role: owner / admin.
func (h *Handler) CreateRepoBinding(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	var req CreateRepoBindingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.RepoFullName = strings.TrimSpace(req.RepoFullName)
	if req.RepoFullName == "" || !strings.Contains(req.RepoFullName, "/") {
		writeError(w, http.StatusBadRequest, "repo_full_name must be in 'owner/repo' format")
		return
	}
	if req.InstallationID <= 0 {
		writeError(w, http.StatusBadRequest, "installation_id is required and must be positive")
		return
	}

	var botUser pgtype.Text
	if req.CrBotUsername != nil {
		trimmed := strings.TrimSpace(*req.CrBotUsername)
		if trimmed != "" {
			botUser = pgtype.Text{String: trimmed, Valid: true}
		}
	}

	binding, err := h.Queries.CreateRepoBinding(r.Context(), db.CreateRepoBindingParams{
		WorkspaceID:    parseUUID(workspaceID),
		RepoFullName:   req.RepoFullName,
		InstallationID: req.InstallationID,
		CrBotUsername:  botUser,
	})
	if err != nil {
		// repo_full_name has a UNIQUE constraint
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "23505") {
			writeError(w, http.StatusConflict, "this repository is already bound to a workspace")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create repo binding")
		return
	}
	writeJSON(w, http.StatusCreated, repoBindingToResponse(binding))
}

// UpdateRepoBinding updates installation_id or active flag.
// Route: PATCH /api/workspaces/{id}/repo-bindings/{bindingId}
// Required role: owner / admin.
func (h *Handler) UpdateRepoBinding(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	bindingID := chi.URLParam(r, "bindingId")
	if bindingID == "" {
		writeError(w, http.StatusBadRequest, "binding id is required")
		return
	}

	// Verify the binding belongs to this workspace.
	existing, err := h.Queries.GetRepoBinding(r.Context(), parseUUID(bindingID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "repo binding not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load repo binding")
		return
	}
	if uuidToString(existing.WorkspaceID) != workspaceID {
		writeError(w, http.StatusNotFound, "repo binding not found")
		return
	}

	var req UpdateRepoBindingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	updated := existing
	if req.InstallationID != nil {
		if *req.InstallationID <= 0 {
			writeError(w, http.StatusBadRequest, "installation_id must be positive")
			return
		}
		updated, err = h.Queries.UpdateRepoBindingInstallation(r.Context(), db.UpdateRepoBindingInstallationParams{
			ID:             parseUUID(bindingID),
			InstallationID: *req.InstallationID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update repo binding")
			return
		}
	}
	if req.Active != nil {
		updated, err = h.Queries.SetRepoBindingActive(r.Context(), db.SetRepoBindingActiveParams{
			ID:     parseUUID(bindingID),
			Active: *req.Active,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update repo binding")
			return
		}
	}

	writeJSON(w, http.StatusOK, repoBindingToResponse(updated))
}

// DeleteRepoBinding removes a workspace ↔ repo binding.
// Route: DELETE /api/workspaces/{id}/repo-bindings/{bindingId}
// Required role: owner / admin.
func (h *Handler) DeleteRepoBinding(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	bindingID := chi.URLParam(r, "bindingId")
	if bindingID == "" {
		writeError(w, http.StatusBadRequest, "binding id is required")
		return
	}

	if err := h.Queries.DeleteRepoBinding(r.Context(), db.DeleteRepoBindingParams{
		ID:          parseUUID(bindingID),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete repo binding")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
