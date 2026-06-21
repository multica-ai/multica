package handler

// CEREBRO-PATCH(artifact-handler): cerebro modification of upstream file

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util" // CEREBRO-PATCH(folder-access-import): FIR-1590
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
	Kind        string  `json:"kind"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
	// CEREBRO-PATCH(folder-access-response): FIR-1590 — folder-level access.
	OwnerID    *string `json:"owner_id"`
	Visibility string  `json:"visibility"`
}

func artifactFolderToResponse(f db.ArtifactFolder) ArtifactFolderResponse {
	resp := ArtifactFolderResponse{
		ID:          uuidToString(f.ID),
		WorkspaceID: uuidToString(f.WorkspaceID),
		Name:        f.Name,
		Kind:        f.Kind,
		CreatedAt:   timestampToString(f.CreatedAt),
		UpdatedAt:   timestampToString(f.UpdatedAt),
		Visibility:  f.Visibility,
	}
	if f.ParentID.Valid {
		s := uuidToString(f.ParentID)
		resp.ParentID = &s
	}
	if f.OwnerID.Valid {
		s := uuidToString(f.OwnerID)
		resp.OwnerID = &s
	}
	return resp
}

// ---------------------------------------------------------------------------
// CreateArtifactFolder — POST /api/artifact-folders
// ---------------------------------------------------------------------------

type CreateArtifactFolderRequest struct {
	Name     string  `json:"name"`
	ParentID *string `json:"parent_id"`
	// CEREBRO-PATCH(artifact-folder-kind): TECH-3637 — scope folders to a surface
	// ("document" or "note"); empty defaults to "document" for existing callers.
	Kind string `json:"kind"`
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

	kind := req.Kind
	if kind == "" {
		kind = "document"
	}

	id, _ := uuid.NewV7()
	folder, err := h.Queries.CreateArtifactFolder(r.Context(), db.CreateArtifactFolderParams{
		ID:          pgtype.UUID{Bytes: id, Valid: true},
		WorkspaceID: parseUUID(workspaceID),
		ParentID:    parentID,
		Name:        req.Name,
		Kind:        kind,
		// CEREBRO-PATCH(folder-access-owner): FIR-1590 — creator owns the folder.
		OwnerID: parseUUID(userID),
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
	// CEREBRO-PATCH(folder-access-list): FIR-1590 — need the caller to filter
	// the folder list to folders they may see.
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}

	// Optional ?kind= scopes the list to one surface so notes and documents
	// don't share a folder tree (TECH-3637). Absent = every folder.
	var kindFilter pgtype.Text
	if k := r.URL.Query().Get("kind"); k != "" {
		kindFilter = pgtype.Text{String: k, Valid: true}
	}
	folders, err := h.Queries.ListArtifactFoldersByWorkspace(r.Context(), db.ListArtifactFoldersByWorkspaceParams{
		WorkspaceID: parseUUID(workspaceID),
		Kind:        kindFilter,
		UserID:      parseUUID(userID),
	})
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
	member, ok := h.workspaceMember(w, r, workspaceID) // CEREBRO-PATCH(folder-action-guard): FIR-1590 — capture role for the action guard
	if !ok {
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

	if !h.requireFolderActionAllowed(w, r, existing, userID, member) { // CEREBRO-PATCH(folder-action-guard): FIR-1590 — guard rename/move
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
	member, ok := h.workspaceMember(w, r, workspaceID) // CEREBRO-PATCH(folder-action-guard): FIR-1590 — capture role for the action guard
	if !ok {
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

	if !h.requireFolderActionAllowed(w, r, existing, userID, member) { // CEREBRO-PATCH(folder-action-guard): FIR-1590 — guard delete
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

// ---------------------------------------------------------------------------
// CEREBRO-PATCH(folder-access-handlers): FIR-1590 — folder-level access control.
// Mirrors the per-note visibility model (cerebro_note) onto folders: a folder
// owner picks "Only you" (private) / "Selected colleagues" (shared) / "Whole
// team" (workspace); the choice gates the folder and everything inside it.
// ---------------------------------------------------------------------------

var validFolderVisibility = map[string]bool{
	"private":   true,
	"shared":    true,
	"workspace": true,
}

type SetArtifactFolderVisibilityRequest struct {
	Visibility    string   `json:"visibility"`
	SharedUserIDs []string `json:"shared_user_ids"`
}

// requireFolderOwner returns the folder when the caller may change its access,
// else writes an error and returns ok=false. A legacy folder with no owner
// (owner_id NULL) may be claimed by any workspace member; once owned, only the
// owner may change it.
func (h *Handler) requireFolderOwner(w http.ResponseWriter, r *http.Request, id, workspaceID, userID string) (db.ArtifactFolder, bool) {
	folder, err := h.Queries.GetArtifactFolder(r.Context(), db.GetArtifactFolderParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "folder not found")
		return db.ArtifactFolder{}, false
	}
	if folder.OwnerID.Valid && uuidToString(folder.OwnerID) != userID {
		writeError(w, http.StatusForbidden, "only the folder owner can change its access")
		return db.ArtifactFolder{}, false
	}
	return folder, true
}

// SetArtifactFolderVisibility — PUT /api/artifact-folders/{id}/visibility
func (h *Handler) SetArtifactFolderVisibility(w http.ResponseWriter, r *http.Request) {
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
	folder, ok := h.requireFolderOwner(w, r, id, workspaceID, userID)
	if !ok {
		return
	}

	var req SetArtifactFolderVisibilityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !validFolderVisibility[req.Visibility] {
		writeError(w, http.StatusBadRequest, "invalid visibility; expected private|shared|workspace")
		return
	}

	if err := h.Queries.SetArtifactFolderVisibility(r.Context(), db.SetArtifactFolderVisibilityParams{
		ID:          folder.ID,
		Visibility:  req.Visibility,
		WorkspaceID: folder.WorkspaceID,
		OwnerID:     parseUUID(userID),
	}); err != nil {
		slog.Error("set artifact folder visibility failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update visibility")
		return
	}

	// Replace the share list: clear, then re-add only for 'shared'.
	if err := h.Queries.ReplaceArtifactFolderShares(r.Context(), folder.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update shares")
		return
	}
	if req.Visibility == "shared" {
		for _, uid := range req.SharedUserIDs {
			parsed, parseErr := util.ParseUUID(uid)
			if parseErr != nil {
				continue
			}
			_ = h.Queries.AddArtifactFolderShare(r.Context(), db.AddArtifactFolderShareParams{
				FolderID: folder.ID,
				UserID:   parsed,
			})
		}
	}

	updated, err := h.Queries.GetArtifactFolder(r.Context(), db.GetArtifactFolderParams{
		ID:          folder.ID,
		WorkspaceID: folder.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reload folder")
		return
	}
	resp := artifactFolderToResponse(updated)
	authorType, authorID := h.resolveActor(r, userID, workspaceID)
	h.publish(EventArtifactFolderUpdated, workspaceID, authorType, authorID, map[string]any{
		"folder": resp,
	})
	writeJSON(w, http.StatusOK, resp)
}

// ListArtifactFolderShares — GET /api/artifact-folders/{id}/shares
func (h *Handler) ListArtifactFolderShares(w http.ResponseWriter, r *http.Request) {
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
	folder, ok := h.requireFolderOwner(w, r, id, workspaceID, userID)
	if !ok {
		return
	}
	shares, err := h.Queries.ListArtifactFolderShares(r.Context(), folder.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list shares")
		return
	}
	ids := make([]string, 0, len(shares))
	for _, s := range shares {
		ids = append(ids, uuidToString(s.UserID))
	}
	writeJSON(w, http.StatusOK, map[string][]string{"shared_user_ids": ids})
}

// folderVisibleToCaller reports whether the requesting member may see the given
// folder (and therefore its contents). A NULL/invalid folder (item at the
// workspace root) is always visible; a caller with no user id (server/agent
// context) is not gated. FIR-1590.
func (h *Handler) folderVisibleToCaller(r *http.Request, folderID pgtype.UUID) bool {
	if !folderID.Valid {
		return true
	}
	uid := requestUserID(r)
	if uid == "" {
		return true
	}
	viewer, err := util.ParseUUID(uid)
	if err != nil {
		return true
	}
	allowed, err := h.Queries.CanUserSeeArtifactFolder(r.Context(), db.CanUserSeeArtifactFolderParams{
		PFolder: folderID,
		Column2: viewer,
	})
	if err != nil {
		return true // fail open on a query error rather than hard-blocking reads
	}
	return allowed
}
