// Package foldergrant hosts the REST surface for per-folder access grants —
// the "Valgt her / Arvet" editor of the Collections feature (FIR-1590 →
// Collections). A grant gives a group / member / workspace / agent / runtime a
// role (viewer | editor | full_access) on a folder; grants cascade down to the
// folder's descendants, so a child folder shows its own direct grants plus the
// ones inherited from its ancestors.
//
// One handler serves both folder backends, selected by `surface`:
//
//	'artifact' = Documents/Notes folders (artifact_folder)
//	'entity'   = Autopilots/Skills folders (cerebro_entity_folder)
//
// Wired under /api/cerebro/folder-grants by the cerebro-folder-grants-routes
// CEREBRO-PATCH in server/cmd/server/router.go. Workspace-scoped via
// X-Workspace-ID and authenticated via X-User-ID. The whole surface is gated
// client-side by the cerebro_collections feature flag (OFF); these endpoints
// stay dormant until the flag and UI ship.
package foldergrant

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
)

// Handler is the folder-grant REST handler.
type Handler struct {
	Cerebro *cerebrodb.Queries
}

func NewHandler(cerebro *cerebrodb.Queries) *Handler {
	return &Handler{Cerebro: cerebro}
}

func validSurface(s string) bool { return s == "artifact" || s == "entity" }

func validGranteeType(s string) bool {
	switch s {
	case "group", "member", "workspace", "agent", "runtime":
		return true
	}
	return false
}

func validRole(s string) bool {
	switch s {
	case "viewer", "editor", "full_access":
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Request / response shapes
// ---------------------------------------------------------------------------

type grantRequest struct {
	Surface     string  `json:"surface"`
	FolderID    string  `json:"folder_id"`
	GranteeType string  `json:"grantee_type"`
	GranteeID   *string `json:"grantee_id"` // null only for grantee_type 'workspace'
	Role        string  `json:"role"`
}

type grantResponse struct {
	Surface        string  `json:"surface"`
	FolderID       string  `json:"folder_id"`
	GranteeType    string  `json:"grantee_type"`
	GranteeID      *string `json:"grantee_id"`
	Role           string  `json:"role"`
	SourceFolderID string  `json:"source_folder_id"`
	IsDirect       bool    `json:"is_direct"`
	Depth          int32   `json:"depth"`
}

func granteeIDPtr(u pgtype.UUID) *string {
	if !u.Valid {
		return nil
	}
	s := util.UUIDToString(u)
	return &s
}

// ---------------------------------------------------------------------------
// Endpoints
// ---------------------------------------------------------------------------

// ListGrants returns the grants on a folder. view=direct returns only grants
// set directly on the folder (the "Valgt her" tab); view=effective (default)
// also includes grants inherited from ancestor folders (the "Arvet" tab).
//
// GET /api/cerebro/folder-grants?surface=&folder_id=&view=direct|effective
func (h *Handler) ListGrants(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	wsUUID, ok := workspaceFromContext(w, r)
	if !ok {
		return
	}
	surface, ok := requireSurfaceQuery(w, r)
	if !ok {
		return
	}
	folderID, ok := parseUUIDQuery(w, r, "folder_id")
	if !ok {
		return
	}
	if !h.folderInWorkspace(w, r, surface, folderID, wsUUID) {
		return
	}

	if strings.TrimSpace(r.URL.Query().Get("view")) == "direct" {
		rows, err := h.Cerebro.ListCerebroFolderDirectGrants(r.Context(), cerebrodb.ListCerebroFolderDirectGrantsParams{
			Surface:  surface,
			FolderID: folderID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list grants failed")
			return
		}
		out := make([]grantResponse, 0, len(rows))
		for _, g := range rows {
			out = append(out, grantResponse{
				Surface:        g.Surface,
				FolderID:       util.UUIDToString(g.FolderID),
				GranteeType:    g.GranteeType,
				GranteeID:      granteeIDPtr(g.GranteeID),
				Role:           g.Role,
				SourceFolderID: util.UUIDToString(g.FolderID),
				IsDirect:       true,
				Depth:          0,
			})
		}
		writeJSON(w, http.StatusOK, out)
		return
	}

	// Effective view (direct + inherited), resolved per surface.
	out := make([]grantResponse, 0)
	switch surface {
	case "artifact":
		rows, err := h.Cerebro.ListCerebroArtifactFolderEffectiveGrants(r.Context(), folderID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list grants failed")
			return
		}
		for _, g := range rows {
			out = append(out, grantResponse{
				Surface:        surface,
				FolderID:       util.UUIDToString(folderID),
				GranteeType:    g.GranteeType,
				GranteeID:      granteeIDPtr(g.GranteeID),
				Role:           g.Role,
				SourceFolderID: util.UUIDToString(g.SourceFolderID),
				IsDirect:       g.IsDirect,
				Depth:          g.Depth,
			})
		}
	case "entity":
		rows, err := h.Cerebro.ListCerebroEntityFolderEffectiveGrants(r.Context(), folderID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list grants failed")
			return
		}
		for _, g := range rows {
			out = append(out, grantResponse{
				Surface:        surface,
				FolderID:       util.UUIDToString(folderID),
				GranteeType:    g.GranteeType,
				GranteeID:      granteeIDPtr(g.GranteeID),
				Role:           g.Role,
				SourceFolderID: util.UUIDToString(g.SourceFolderID),
				IsDirect:       g.IsDirect,
				Depth:          g.Depth,
			})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// UpsertGrant adds or changes a grant on a folder (the "Tilføj / ændre" action
// in the Valgt her tab).
//
// PUT /api/cerebro/folder-grants
func (h *Handler) UpsertGrant(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := workspaceFromContext(w, r)
	if !ok {
		return
	}
	var req grantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	surface, folderID, granteeID, ok := h.validateGrantTarget(w, r, req, wsUUID)
	if !ok {
		return
	}
	if !validRole(req.Role) {
		writeError(w, http.StatusBadRequest, "role must be viewer, editor or full_access")
		return
	}

	createdBy, err := util.ParseUUID(userID)
	if err != nil {
		createdBy = pgtype.UUID{}
	}
	g, err := h.Cerebro.UpsertCerebroFolderGrant(r.Context(), cerebrodb.UpsertCerebroFolderGrantParams{
		Surface:     surface,
		FolderID:    folderID,
		GranteeType: req.GranteeType,
		GranteeID:   granteeID,
		Role:        req.Role,
		CreatedBy:   createdBy,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "save grant failed")
		return
	}
	writeJSON(w, http.StatusOK, grantResponse{
		Surface:        g.Surface,
		FolderID:       util.UUIDToString(g.FolderID),
		GranteeType:    g.GranteeType,
		GranteeID:      granteeIDPtr(g.GranteeID),
		Role:           g.Role,
		SourceFolderID: util.UUIDToString(g.FolderID),
		IsDirect:       true,
		Depth:          0,
	})
}

// RemoveGrant removes a single direct grant from a folder.
//
// DELETE /api/cerebro/folder-grants
func (h *Handler) RemoveGrant(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	wsUUID, ok := workspaceFromContext(w, r)
	if !ok {
		return
	}
	var req grantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	surface, folderID, granteeID, ok := h.validateGrantTarget(w, r, req, wsUUID)
	if !ok {
		return
	}
	if err := h.Cerebro.RemoveCerebroFolderGrant(r.Context(), cerebrodb.RemoveCerebroFolderGrantParams{
		Surface:     surface,
		FolderID:    folderID,
		GranteeType: req.GranteeType,
		GranteeID:   granteeID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "remove grant failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Validation helpers
// ---------------------------------------------------------------------------

// validateGrantTarget validates surface, folder, grantee_type and grantee_id of
// a write request, and confirms the folder belongs to the caller's workspace.
func (h *Handler) validateGrantTarget(w http.ResponseWriter, r *http.Request, req grantRequest, wsUUID pgtype.UUID) (string, pgtype.UUID, pgtype.UUID, bool) {
	if !validSurface(req.Surface) {
		writeError(w, http.StatusBadRequest, "surface must be 'artifact' or 'entity'")
		return "", pgtype.UUID{}, pgtype.UUID{}, false
	}
	if !validGranteeType(req.GranteeType) {
		writeError(w, http.StatusBadRequest, "invalid grantee_type")
		return "", pgtype.UUID{}, pgtype.UUID{}, false
	}
	folderID, err := util.ParseUUID(strings.TrimSpace(req.FolderID))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid folder_id")
		return "", pgtype.UUID{}, pgtype.UUID{}, false
	}

	// 'workspace' is the only grantee with no id; everyone else must name one.
	var granteeID pgtype.UUID
	if req.GranteeType == "workspace" {
		if req.GranteeID != nil && strings.TrimSpace(*req.GranteeID) != "" {
			writeError(w, http.StatusBadRequest, "workspace grant must not carry a grantee_id")
			return "", pgtype.UUID{}, pgtype.UUID{}, false
		}
	} else {
		if req.GranteeID == nil || strings.TrimSpace(*req.GranteeID) == "" {
			writeError(w, http.StatusBadRequest, "grantee_id required for this grantee_type")
			return "", pgtype.UUID{}, pgtype.UUID{}, false
		}
		granteeID, err = util.ParseUUID(strings.TrimSpace(*req.GranteeID))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid grantee_id")
			return "", pgtype.UUID{}, pgtype.UUID{}, false
		}
	}

	if !h.folderInWorkspace(w, r, req.Surface, folderID, wsUUID) {
		return "", pgtype.UUID{}, pgtype.UUID{}, false
	}
	return req.Surface, folderID, granteeID, true
}

// folderInWorkspace confirms the folder exists and belongs to the caller's
// workspace. Writes 404 and returns false otherwise (a folder in another
// workspace is reported as not-found so its existence is not leaked).
func (h *Handler) folderInWorkspace(w http.ResponseWriter, r *http.Request, surface string, folderID, wsUUID pgtype.UUID) bool {
	var owner pgtype.UUID
	var err error
	switch surface {
	case "artifact":
		owner, err = h.Cerebro.GetCerebroArtifactFolderWorkspace(r.Context(), folderID)
	case "entity":
		owner, err = h.Cerebro.GetCerebroEntityFolderWorkspace(r.Context(), folderID)
	default:
		writeError(w, http.StatusBadRequest, "surface must be 'artifact' or 'entity'")
		return false
	}
	if err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "folder not found")
			return false
		}
		writeError(w, http.StatusInternalServerError, "folder lookup failed")
		return false
	}
	if !uuidEqual(owner, wsUUID) {
		writeError(w, http.StatusNotFound, "folder not found")
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// Small request helpers (mirrors the entityfolder package)
// ---------------------------------------------------------------------------

func requireSurfaceQuery(w http.ResponseWriter, r *http.Request) (string, bool) {
	s := strings.TrimSpace(r.URL.Query().Get("surface"))
	if !validSurface(s) {
		writeError(w, http.StatusBadRequest, "surface query param must be 'artifact' or 'entity'")
		return "", false
	}
	return s, true
}

func parseUUIDQuery(w http.ResponseWriter, r *http.Request, name string) (pgtype.UUID, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
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

func requireUserID(w http.ResponseWriter, r *http.Request) (string, bool) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return "", false
	}
	return userID, true
}

func uuidEqual(a, b pgtype.UUID) bool {
	if !a.Valid || !b.Valid {
		return false
	}
	return a.Bytes == b.Bytes
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
