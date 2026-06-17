package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/logger"
)

// UpsertWorkspaceSecret creates or updates a named secret. Only workspace
// owners and admins can manage secrets. Agent actors are rejected.
func (h *Handler) UpsertWorkspaceSecret(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	secretName := chi.URLParam(r, "name")
	if secretName == "" {
		writeError(w, http.StatusBadRequest, "secret name is required")
		return
	}

	userID := requestUserID(r)
	actorType, _ := h.resolveActor(r, userID, workspaceID)
	if actorType == "agent" {
		writeError(w, http.StatusForbidden, "agents may not access secret management endpoints")
		return
	}

	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin")
	if !ok {
		return
	}

	var req struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Value == "" {
		writeError(w, http.StatusBadRequest, "secret value is required")
		return
	}

	if h.WorkspaceSecretService == nil {
		writeError(w, http.StatusServiceUnavailable, "workspace secret service is not configured")
		return
	}

	if err := h.WorkspaceSecretService.UpsertSecret(r.Context(), member.WorkspaceID, secretName, req.Value, member.ID); err != nil {
		slog.Error("upsert workspace secret failed",
			append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID, "secret_name", secretName)...)
		writeError(w, http.StatusInternalServerError, "failed to store secret")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"workspace_id": uuidToString(member.WorkspaceID),
		"name":         secretName,
	})
}

// DeleteWorkspaceSecret removes a named secret. Only workspace owners and
// admins can delete secrets.
func (h *Handler) DeleteWorkspaceSecret(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	secretName := chi.URLParam(r, "name")
	if secretName == "" {
		writeError(w, http.StatusBadRequest, "secret name is required")
		return
	}

	userID := requestUserID(r)
	actorType, _ := h.resolveActor(r, userID, workspaceID)
	if actorType == "agent" {
		writeError(w, http.StatusForbidden, "agents may not access secret management endpoints")
		return
	}

	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin")
	if !ok {
		return
	}

	if h.WorkspaceSecretService == nil {
		writeError(w, http.StatusServiceUnavailable, "workspace secret service is not configured")
		return
	}

	if err := h.WorkspaceSecretService.DeleteSecret(r.Context(), member.WorkspaceID, secretName); err != nil {
		slog.Error("delete workspace secret failed",
			append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID, "secret_name", secretName)...)
		writeError(w, http.StatusInternalServerError, "failed to delete secret")
		return
	}

	writeJSON(w, http.StatusNoContent, nil)
}

// ListWorkspaceSecretNames returns all secret names for a workspace.
// Members see names only; owners and admins can request values with ?include_values=true.
// Agent actors are rejected.
func (h *Handler) ListWorkspaceSecretNames(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")

	userID := requestUserID(r)
	actorType, _ := h.resolveActor(r, userID, workspaceID)
	if actorType == "agent" {
		writeError(w, http.StatusForbidden, "agents may not access secret management endpoints")
		return
	}

	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}

	if h.WorkspaceSecretService == nil {
		writeError(w, http.StatusServiceUnavailable, "workspace secret service is not configured")
		return
	}

	names, err := h.WorkspaceSecretService.ListSecretNames(r.Context(), member.WorkspaceID)
	if err != nil {
		slog.Error("list workspace secrets failed",
			append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to list secrets")
		return
	}

	type secretEntry struct {
		Name      string `json:"name"`
		CreatedBy string `json:"created_by,omitempty"`
	}

	entries := make([]secretEntry, 0, len(names))
	for _, n := range names {
		entries = append(entries, secretEntry{Name: n.Name, CreatedBy: uuidToString(n.CreatedBy)})
	}

	includeValues := r.URL.Query().Get("include_values") == "true" && (member.Role == "owner" || member.Role == "admin")
	result := map[string]any{"secrets": entries}
	if includeValues {
		values := make(map[string]string, len(names))
		for _, n := range names {
			val, err := h.WorkspaceSecretService.GetSecret(r.Context(), member.WorkspaceID, n.Name)
			if err != nil {
				values[n.Name] = ""
				continue
			}
			values[n.Name] = val
		}
		result["values"] = values
	}

	writeJSON(w, http.StatusOK, result)
}
