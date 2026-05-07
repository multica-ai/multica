package handler

// CEREBRO-PATCH(artifact-handler): cerebro modification of upstream file

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Event types for folder lifecycle. Defined here (not in pkg/protocol/events.go)
// to keep this feature additive and avoid merge conflicts with upstream.
const (
	EventArtifactFolderCreated = "artifact_folder:created"
	EventArtifactFolderUpdated = "artifact_folder:updated"
	EventArtifactFolderDeleted = "artifact_folder:deleted"
)

type ArtifactFolderResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	ParentID    *string `json:"parent_id"`
	Name        string  `json:"name"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

func artifactFolderToResponse(f db.ArtifactFolder) ArtifactFolderResponse {
	resp := ArtifactFolderResponse{
		ID:          uuidToString(f.ID),
		WorkspaceID: uuidToString(f.WorkspaceID),
		Name:        f.Name,
		CreatedAt:   timestampToString(f.CreatedAt),
		UpdatedAt:   timestampToString(f.UpdatedAt),
	}
	if f.ParentID.Valid {
		s := uuidToString(f.ParentID)
		resp.ParentID = &s
	}
	return resp
}

// ---------------------------------------------------------------------------
// CreateArtifactFolder — POST /api/artifact-folders
// ---------------------------------------------------------------------------

type CreateArtifactFolderRequest struct {
	Name     string  `json:"name"`
	ParentID *string `json:"parent_id"`
}

func (h *Handler) CreateArtifactFolder(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}

	var req CreateArtifactFolderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	var parentID pgtype.UUID
	if req.ParentID != nil && *req.ParentID != "" {
		parent, err := h.Queries.GetArtifactFolder(r.Context(), db.GetArtifactFolderParams{
			ID:          parseUUID(*req.ParentID),
			WorkspaceID: parseUUID(workspaceID),
		})
		if err != nil {
			writeError(w, http.StatusNotFound, "parent folder not found")
			return
		}
		parentID = parent.ID
	}

	id, _ := uuid.NewV7()
	folder, err := h.Queries.CreateArtifactFolder(r.Context(), db.CreateArtifactFolderParams{
		ID:          pgtype.UUID{Bytes: id, Valid: true},
		WorkspaceID: parseUUID(workspaceID),
		ParentID:    parentID,
		Name:        req.Name,
	})
	if err != nil {
		slog.Error("create artifact folder failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create folder (name may already exist in this parent)")
		return
	}

	resp := artifactFolderToResponse(folder)
	authorType, authorID := h.resolveActor(r, userID, workspaceID)
	h.publish(EventArtifactFolderCreated, workspaceID, authorType, authorID, map[string]any{
		"folder": resp,
	})
	writeJSON(w, http.StatusCreated, resp)
}

// ---------------------------------------------------------------------------
// ListArtifactFolders — GET /api/artifact-folders
// ---------------------------------------------------------------------------

func (h *Handler) ListArtifactFolders(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}

	folders, err := h.Queries.ListArtifactFoldersByWorkspace(r.Context(), parseUUID(workspaceID))
	if err != nil {
		slog.Error("list artifact folders failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list folders")
		return
	}

	resp := make([]ArtifactFolderResponse, len(folders))
	for i, f := range folders {
		resp[i] = artifactFolderToResponse(f)
	}
	writeJSON(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------------------
// UpdateArtifactFolder — PUT /api/artifact-folders/{id}
// ---------------------------------------------------------------------------

type UpdateArtifactFolderRequest struct {
	Name     *string `json:"name"`
	ParentID *string `json:"parent_id"`
}

func (h *Handler) UpdateArtifactFolder(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}

	existing, err := h.Queries.GetArtifactFolder(r.Context(), db.GetArtifactFolderParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "folder not found")
		return
	}

	var req UpdateArtifactFolderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	name := existing.Name
	if req.Name != nil {
		if *req.Name == "" {
			writeError(w, http.StatusBadRequest, "name cannot be empty")
			return
		}
		name = *req.Name
	}

	parentID := existing.ParentID
	if req.ParentID != nil {
		if *req.ParentID == "" {
			parentID = pgtype.UUID{}
		} else {
			if *req.ParentID == id {
				writeError(w, http.StatusBadRequest, "folder cannot be its own parent")
				return
			}
			parent, err := h.Queries.GetArtifactFolder(r.Context(), db.GetArtifactFolderParams{
				ID:          parseUUID(*req.ParentID),
				WorkspaceID: parseUUID(workspaceID),
			})
			if err != nil {
				writeError(w, http.StatusNotFound, "parent folder not found")
				return
			}
			parentID = parent.ID
		}
	}

	updated, err := h.Queries.UpdateArtifactFolder(r.Context(), db.UpdateArtifactFolderParams{
		ID:          existing.ID,
		WorkspaceID: existing.WorkspaceID,
		Name:        name,
		ParentID:    parentID,
	})
	if err != nil {
		slog.Error("update artifact folder failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update folder")
		return
	}

	resp := artifactFolderToResponse(updated)
	authorType, authorID := h.resolveActor(r, userID, workspaceID)
	h.publish(EventArtifactFolderUpdated, workspaceID, authorType, authorID, map[string]any{
		"folder": resp,
	})
	writeJSON(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------------------
// DeleteArtifactFolder — DELETE /api/artifact-folders/{id}
// ---------------------------------------------------------------------------

func (h *Handler) DeleteArtifactFolder(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}

	existing, err := h.Queries.GetArtifactFolder(r.Context(), db.GetArtifactFolderParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "folder not found")
		return
	}

	if err := h.Queries.DeleteArtifactFolder(r.Context(), db.DeleteArtifactFolderParams{
		ID:          existing.ID,
		WorkspaceID: existing.WorkspaceID,
	}); err != nil {
		slog.Error("delete artifact folder failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete folder")
		return
	}

	resp := artifactFolderToResponse(existing)
	authorType, authorID := h.resolveActor(r, userID, workspaceID)
	h.publish(EventArtifactFolderDeleted, workspaceID, authorType, authorID, map[string]any{
		"folder": resp,
	})
	w.WriteHeader(http.StatusNoContent)
}
