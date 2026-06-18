// Package entityfolder hosts the REST surface for folders on the Skills and
// Autopilots interfaces (FIR-1412). A single cerebro-namespaced table backs
// both surfaces, scoped by `kind` ('skill' | 'autopilot'); folders nest via a
// self-referencing parent_id, and a membership table files an individual
// skill/autopilot into a folder.
//
// Wired into the router under /api/cerebro/entity-folders and
// /api/cerebro/entity-folder-items by the cerebro-entity-folders-routes
// CEREBRO-PATCH in server/cmd/server/router.go.
//
// Every endpoint is workspace-scoped via the X-Workspace-ID header (read
// through middleware.WorkspaceIDFromContext) and authenticated via the
// X-User-ID header (requireUserID).
package entityfolder

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
)

// Handler is the entity-folder REST handler.
type Handler struct {
	Cerebro *cerebrodb.Queries
}

func NewHandler(cerebro *cerebrodb.Queries) *Handler {
	return &Handler{Cerebro: cerebro}
}

func validKind(k string) bool {
	return k == "skill" || k == "autopilot"
}

// ---------------------------------------------------------------------------
// Request / response shapes
// ---------------------------------------------------------------------------

type folderRequest struct {
	Name     string  `json:"name"`
	Kind     string  `json:"kind"`
	ParentID *string `json:"parent_id"`
}

type folderResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	ParentID    *string `json:"parent_id"`
	Kind        string  `json:"kind"`
	Name        string  `json:"name"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

func toFolderResponse(f cerebrodb.CerebroEntityFolder) folderResponse {
	resp := folderResponse{
		ID:          util.UUIDToString(f.ID),
		WorkspaceID: util.UUIDToString(f.WorkspaceID),
		Kind:        f.Kind,
		Name:        f.Name,
		CreatedAt:   tsString(f.CreatedAt),
		UpdatedAt:   tsString(f.UpdatedAt),
	}
	if f.ParentID.Valid {
		s := util.UUIDToString(f.ParentID)
		resp.ParentID = &s
	}
	return resp
}

type itemRequest struct {
	Kind     string  `json:"kind"`
	ItemID   string  `json:"item_id"`
	FolderID *string `json:"folder_id"` // null = unfile (move to root)
}

type itemResponse struct {
	Kind     string `json:"kind"`
	ItemID   string `json:"item_id"`
	FolderID string `json:"folder_id"`
}

func toItemResponse(it cerebrodb.CerebroEntityFolderItem) itemResponse {
	return itemResponse{
		Kind:     it.Kind,
		ItemID:   util.UUIDToString(it.ItemID),
		FolderID: util.UUIDToString(it.FolderID),
	}
}

// ---------------------------------------------------------------------------
// Folder CRUD
// ---------------------------------------------------------------------------

// ListFolders returns every folder of the given kind in the current workspace.
// The flat list is turned into a tree client-side via parent_id.
func (h *Handler) ListFolders(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	wsUUID, ok := workspaceFromContext(w, r)
	if !ok {
		return
	}
	kind, ok := requireKindQuery(w, r)
	if !ok {
		return
	}
	rows, err := h.Cerebro.ListCerebroEntityFoldersByWorkspace(r.Context(), cerebrodb.ListCerebroEntityFoldersByWorkspaceParams{
		WorkspaceID: wsUUID,
		Kind:        kind,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list folders failed")
		return
	}
	out := make([]folderResponse, 0, len(rows))
	for _, f := range rows {
		out = append(out, toFolderResponse(f))
	}
	writeJSON(w, http.StatusOK, out)
}

// CreateFolder creates a folder (optionally nested under parent_id).
func (h *Handler) CreateFolder(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	wsUUID, ok := workspaceFromContext(w, r)
	if !ok {
		return
	}
	var req folderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if !validKind(req.Kind) {
		writeError(w, http.StatusBadRequest, "kind must be 'skill' or 'autopilot'")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	parent, ok := h.resolveParent(w, r, wsUUID, req.Kind, req.ParentID)
	if !ok {
		return
	}
	f, err := h.Cerebro.CreateCerebroEntityFolder(r.Context(), cerebrodb.CreateCerebroEntityFolderParams{
		WorkspaceID: wsUUID,
		Kind:        req.Kind,
		Name:        name,
		ParentID:    parent,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create folder failed")
		return
	}
	writeJSON(w, http.StatusCreated, toFolderResponse(f))
}

// UpdateFolder renames a folder and/or moves it to a different parent.
func (h *Handler) UpdateFolder(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	wsUUID, ok := workspaceFromContext(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	existing, err := h.Cerebro.GetCerebroEntityFolder(r.Context(), cerebrodb.GetCerebroEntityFolderParams{
		ID:          id,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "folder not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "load folder failed")
		return
	}

	var req folderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	name := existing.Name
	if strings.TrimSpace(req.Name) != "" {
		name = strings.TrimSpace(req.Name)
	}
	// parent_id defaults to its current value when the field is omitted.
	parent := existing.ParentID
	if req.ParentID != nil {
		p, ok := h.resolveParent(w, r, wsUUID, existing.Kind, req.ParentID)
		if !ok {
			return
		}
		parent = p
	}
	// Guard against a folder becoming its own ancestor (cycle).
	if parent.Valid && uuidEqual(parent, id) {
		writeError(w, http.StatusBadRequest, "a folder cannot be its own parent")
		return
	}
	if parent.Valid {
		cyclic, err := h.wouldCycle(r.Context(), wsUUID, id, parent)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "validate parent failed")
			return
		}
		if cyclic {
			writeError(w, http.StatusBadRequest, "cannot move a folder into one of its own sub-folders")
			return
		}
	}

	f, err := h.Cerebro.UpdateCerebroEntityFolder(r.Context(), cerebrodb.UpdateCerebroEntityFolderParams{
		ID:          id,
		WorkspaceID: wsUUID,
		Name:        name,
		ParentID:    parent,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update folder failed")
		return
	}
	writeJSON(w, http.StatusOK, toFolderResponse(f))
}

// DeleteFolder removes a folder. Sub-folders cascade away and items in the
// deleted folder(s) fall back to root (membership rows cascade-delete).
func (h *Handler) DeleteFolder(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	wsUUID, ok := workspaceFromContext(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	if _, err := h.Cerebro.GetCerebroEntityFolder(r.Context(), cerebrodb.GetCerebroEntityFolderParams{
		ID:          id,
		WorkspaceID: wsUUID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "folder not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "load folder failed")
		return
	}
	if err := h.Cerebro.DeleteCerebroEntityFolder(r.Context(), cerebrodb.DeleteCerebroEntityFolderParams{
		ID:          id,
		WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "delete folder failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Membership
// ---------------------------------------------------------------------------

// ListItems returns the folder membership for every filed item of the given
// kind in the workspace. Items without a row live at the root.
func (h *Handler) ListItems(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	wsUUID, ok := workspaceFromContext(w, r)
	if !ok {
		return
	}
	kind, ok := requireKindQuery(w, r)
	if !ok {
		return
	}
	rows, err := h.Cerebro.ListCerebroEntityFolderItemsByWorkspace(r.Context(), cerebrodb.ListCerebroEntityFolderItemsByWorkspaceParams{
		WorkspaceID: wsUUID,
		Kind:        kind,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list folder items failed")
		return
	}
	out := make([]itemResponse, 0, len(rows))
	for _, it := range rows {
		out = append(out, toItemResponse(it))
	}
	writeJSON(w, http.StatusOK, out)
}

// SetItem files an item into a folder, or unfiles it (folder_id null → root).
func (h *Handler) SetItem(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	wsUUID, ok := workspaceFromContext(w, r)
	if !ok {
		return
	}
	var req itemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if !validKind(req.Kind) {
		writeError(w, http.StatusBadRequest, "kind must be 'skill' or 'autopilot'")
		return
	}
	itemID, err := util.ParseUUID(strings.TrimSpace(req.ItemID))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid item_id")
		return
	}

	// folder_id null/empty → unfile (move to root).
	if req.FolderID == nil || strings.TrimSpace(*req.FolderID) == "" {
		if err := h.Cerebro.DeleteCerebroEntityFolderItem(r.Context(), cerebrodb.DeleteCerebroEntityFolderItemParams{
			WorkspaceID: wsUUID,
			Kind:        req.Kind,
			ItemID:      itemID,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "unfile item failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"item_id": util.UUIDToString(itemID), "folder_id": nil})
		return
	}

	folderID, err := util.ParseUUID(strings.TrimSpace(*req.FolderID))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid folder_id")
		return
	}
	// The destination folder must exist in this workspace and match the kind.
	folder, err := h.Cerebro.GetCerebroEntityFolder(r.Context(), cerebrodb.GetCerebroEntityFolderParams{
		ID:          folderID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "folder not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "load folder failed")
		return
	}
	if folder.Kind != req.Kind {
		writeError(w, http.StatusBadRequest, "folder kind mismatch")
		return
	}
	if err := h.Cerebro.SetCerebroEntityFolderItem(r.Context(), cerebrodb.SetCerebroEntityFolderItemParams{
		WorkspaceID: wsUUID,
		Kind:        req.Kind,
		ItemID:      itemID,
		FolderID:    folderID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "file item failed")
		return
	}
	writeJSON(w, http.StatusOK, itemResponse{
		Kind:     req.Kind,
		ItemID:   util.UUIDToString(itemID),
		FolderID: util.UUIDToString(folderID),
	})
}

// ---------------------------------------------------------------------------
// Parent resolution + cycle detection
// ---------------------------------------------------------------------------

// resolveParent validates an optional parent_id: it must exist in the same
// workspace and share the kind. Returns an invalid (null) UUID when no parent
// is given. ok=false means a 4xx was already written.
func (h *Handler) resolveParent(w http.ResponseWriter, r *http.Request, wsUUID pgtype.UUID, kind string, parentID *string) (pgtype.UUID, bool) {
	if parentID == nil || strings.TrimSpace(*parentID) == "" {
		return pgtype.UUID{}, true
	}
	pid, err := util.ParseUUID(strings.TrimSpace(*parentID))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid parent_id")
		return pgtype.UUID{}, false
	}
	parent, err := h.Cerebro.GetCerebroEntityFolder(r.Context(), cerebrodb.GetCerebroEntityFolderParams{
		ID:          pid,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusBadRequest, "parent folder not found")
			return pgtype.UUID{}, false
		}
		writeError(w, http.StatusInternalServerError, "load parent failed")
		return pgtype.UUID{}, false
	}
	if parent.Kind != kind {
		writeError(w, http.StatusBadRequest, "parent folder kind mismatch")
		return pgtype.UUID{}, false
	}
	return pid, true
}

// wouldCycle reports whether making `newParent` the parent of `folderID` would
// create a cycle — i.e. newParent is folderID itself or one of its descendants.
// It walks up from newParent to the root; if it ever reaches folderID, it's a
// cycle. The depth bound is a safety net against pre-existing corrupt data.
func (h *Handler) wouldCycle(ctx context.Context, wsUUID pgtype.UUID, folderID, newParent pgtype.UUID) (bool, error) {
	cur := newParent
	for i := 0; i < 256 && cur.Valid; i++ {
		if uuidEqual(cur, folderID) {
			return true, nil
		}
		node, err := h.Cerebro.GetCerebroEntityFolder(ctx, cerebrodb.GetCerebroEntityFolderParams{
			ID:          cur,
			WorkspaceID: wsUUID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return false, nil
			}
			return false, err
		}
		cur = node.ParentID
	}
	return false, nil
}

// ---------------------------------------------------------------------------
// Helpers (mirrors of the note_types package; kept local to avoid cross-package
// coupling on request-plumbing internals).
// ---------------------------------------------------------------------------

func requireKindQuery(w http.ResponseWriter, r *http.Request) (string, bool) {
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	if !validKind(kind) {
		writeError(w, http.StatusBadRequest, "kind query param must be 'skill' or 'autopilot'")
		return "", false
	}
	return kind, true
}

func parseUUIDParam(w http.ResponseWriter, r *http.Request, name string) (pgtype.UUID, bool) {
	raw := strings.TrimSpace(chi.URLParam(r, name))
	if raw == "" {
		writeError(w, http.StatusBadRequest, "missing "+name)
		return pgtype.UUID{}, false
	}
	uid, err := util.ParseUUID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid "+name)
		return pgtype.UUID{}, false
	}
	return uid, true
}

func workspaceFromContext(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	raw := middleware.WorkspaceIDFromContext(r.Context())
	if raw == "" {
		writeError(w, http.StatusBadRequest, "missing workspace")
		return pgtype.UUID{}, false
	}
	uid, err := util.ParseUUID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace")
		return pgtype.UUID{}, false
	}
	return uid, true
}

func uuidEqual(a, b pgtype.UUID) bool {
	if !a.Valid || !b.Valid {
		return false
	}
	return a.Bytes == b.Bytes
}

func requireUserID(w http.ResponseWriter, r *http.Request) (string, bool) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return "", false
	}
	return userID, true
}

func tsString(t pgtype.Timestamptz) string {
	if !t.Valid {
		return ""
	}
	return t.Time.UTC().Format(time.RFC3339)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
