// Package projectgrant hosts the REST surface for per-project access grants —
// the hierarchical project access layer of the Collections feature (FIR-2125).
// A grant gives a group / member / workspace / agent / runtime a role
// (viewer | editor | full_access) on a project; grants cascade down to
// sub-projects via WITH RECURSIVE over the project_nesting table, so a
// child project shows its own direct grants plus the ones inherited from
// its parent (and grandparent).
//
// Wired under /api/cerebro/project-grants by the cerebro-project-grants-routes
// CEREBRO-PATCH in server/cmd/server/router.go. Workspace-scoped via
// X-Workspace-ID and authenticated via X-User-ID. The whole surface is gated
// client-side by the cerebro_collections feature flag (OFF); these endpoints
// stay dormant until the flag and UI ship.
package projectgrant

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
)

// Handler is the project-grant REST handler.
type Handler struct {
	pool *pgxpool.Pool
}

func NewHandler(pool *pgxpool.Pool) *Handler {
	return &Handler{pool: pool}
}

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
	ProjectID   string  `json:"project_id"`
	GranteeType string  `json:"grantee_type"`
	GranteeID   *string `json:"grantee_id"` // null only for grantee_type 'workspace'
	Role        string  `json:"role"`
}

type grantResponse struct {
	ProjectID       string  `json:"project_id"`
	GranteeType     string  `json:"grantee_type"`
	GranteeID       *string `json:"grantee_id"`
	Role            string  `json:"role"`
	SourceProjectID string  `json:"source_project_id"`
	IsDirect        bool    `json:"is_direct"`
	Depth           int     `json:"depth"`
}

func uuidToStringPtr(u pgtype.UUID) *string {
	if !u.Valid {
		return nil
	}
	s := util.UUIDToString(u)
	return &s
}

// ---------------------------------------------------------------------------
// Endpoints
// ---------------------------------------------------------------------------

// ListGrants returns the grants on a project.
// view=direct returns only grants set directly on the project ("This project" tab);
// view=effective (default) also includes grants inherited from ancestor projects ("Inherited" tab).
//
// GET /api/cerebro/project-grants?project_id=&view=direct|effective
func (h *Handler) ListGrants(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	wsUUID, ok := workspaceFromContext(w, r)
	if !ok {
		return
	}
	projectUUID, ok := parseUUIDQuery(w, r, "project_id")
	if !ok {
		return
	}
	if !h.projectInWorkspace(r.Context(), w, projectUUID, wsUUID) {
		return
	}

	if strings.TrimSpace(r.URL.Query().Get("view")) == "direct" {
		rows, err := h.listDirectGrants(r.Context(), wsUUID, projectUUID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list grants failed")
			return
		}
		writeJSON(w, http.StatusOK, rows)
		return
	}

	// Effective view: direct + inherited via project_nesting recursion.
	rows, err := h.listEffectiveGrants(r.Context(), wsUUID, projectUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list grants failed")
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

// UpsertGrant adds or changes a grant on a project.
// Requires the caller to be a workspace admin or owner.
//
// PUT /api/cerebro/project-grants
func (h *Handler) UpsertGrant(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := workspaceFromContext(w, r)
	if !ok {
		return
	}
	if !h.requireAdminOrOwner(r.Context(), w, userID, wsUUID) {
		return
	}
	var req grantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	projectUUID, granteeUUID, ok := h.validateGrantTarget(w, r, req, wsUUID)
	if !ok {
		return
	}
	if !validRole(req.Role) {
		writeError(w, http.StatusBadRequest, "role must be viewer, editor or full_access")
		return
	}

	createdBy, _ := util.ParseUUID(userID)

	const q = `
INSERT INTO cerebro_project_grant (workspace_id, project_id, grantee_type, grantee_id, role, created_by)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (project_id, grantee_type,
             COALESCE(grantee_id, '00000000-0000-0000-0000-000000000000'::uuid))
DO UPDATE SET role = EXCLUDED.role
RETURNING project_id, grantee_type, grantee_id, role`

	row := h.pool.QueryRow(r.Context(), q,
		wsUUID, projectUUID, req.GranteeType, granteeUUID, req.Role, createdBy)

	var resp grantResponse
	var projectID pgtype.UUID
	var granteeID pgtype.UUID
	if err := row.Scan(&projectID, &resp.GranteeType, &granteeID, &resp.Role); err != nil {
		writeError(w, http.StatusInternalServerError, "save grant failed")
		return
	}
	resp.ProjectID = util.UUIDToString(projectID)
	resp.SourceProjectID = resp.ProjectID
	resp.GranteeID = uuidToStringPtr(granteeID)
	resp.IsDirect = true
	resp.Depth = 0

	writeJSON(w, http.StatusOK, resp)
}

// RemoveGrant removes a single direct grant from a project.
// Requires the caller to be a workspace admin or owner.
//
// DELETE /api/cerebro/project-grants
func (h *Handler) RemoveGrant(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := workspaceFromContext(w, r)
	if !ok {
		return
	}
	if !h.requireAdminOrOwner(r.Context(), w, userID, wsUUID) {
		return
	}
	var req grantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	projectUUID, granteeUUID, ok := h.validateGrantTarget(w, r, req, wsUUID)
	if !ok {
		return
	}

	const q = `
DELETE FROM cerebro_project_grant
WHERE workspace_id = $1 AND project_id = $2 AND grantee_type = $3
  AND COALESCE(grantee_id, '00000000-0000-0000-0000-000000000000'::uuid)
    = COALESCE($4, '00000000-0000-0000-0000-000000000000'::uuid)`

	if _, err := h.pool.Exec(r.Context(), q, wsUUID, projectUUID, req.GranteeType, granteeUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "remove grant failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// DB helpers
// ---------------------------------------------------------------------------

func (h *Handler) listDirectGrants(ctx context.Context, wsUUID, projectUUID pgtype.UUID) ([]grantResponse, error) {
	const q = `
SELECT project_id, grantee_type, grantee_id, role
FROM cerebro_project_grant
WHERE workspace_id = $1 AND project_id = $2
ORDER BY grantee_type, created_at`

	rows, err := h.pool.Query(ctx, q, wsUUID, projectUUID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []grantResponse
	for rows.Next() {
		var projectID, granteeID pgtype.UUID
		var r grantResponse
		if err := rows.Scan(&projectID, &r.GranteeType, &granteeID, &r.Role); err != nil {
			return nil, err
		}
		r.ProjectID = util.UUIDToString(projectID)
		r.SourceProjectID = r.ProjectID
		r.GranteeID = uuidToStringPtr(granteeID)
		r.IsDirect = true
		r.Depth = 0
		out = append(out, r)
	}
	return out, rows.Err()
}

func (h *Handler) listEffectiveGrants(ctx context.Context, wsUUID, projectUUID pgtype.UUID) ([]grantResponse, error) {
	// Walk ancestor projects then join grants. We anchor the recursion on the
	// project_nesting table. Projects that have no nesting row (root-only) are
	// handled by the UNION with the direct-grants query below so we always return
	// at least the project's own grants even when no project_nesting row exists.
	const q = `
WITH RECURSIVE ancestors AS (
    SELECT $1::uuid AS id, 0 AS depth
    UNION ALL
    SELECT pn.parent_project_id, a.depth + 1
    FROM project_nesting pn
    JOIN ancestors a ON pn.project_id = a.id
    WHERE pn.parent_project_id IS NOT NULL AND a.depth < 3
)
SELECT g.project_id, g.grantee_type, g.grantee_id, g.role,
       a.depth, (a.depth = 0) AS is_direct
FROM ancestors a
JOIN cerebro_project_grant g ON g.project_id = a.id
  AND g.workspace_id = $2
ORDER BY a.depth ASC, g.grantee_type`

	rows, err := h.pool.Query(ctx, q, projectUUID, wsUUID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []grantResponse
	for rows.Next() {
		var projectID, granteeID pgtype.UUID
		var r grantResponse
		if err := rows.Scan(&projectID, &r.GranteeType, &granteeID, &r.Role, &r.Depth, &r.IsDirect); err != nil {
			return nil, err
		}
		r.ProjectID = util.UUIDToString(projectUUID)
		r.SourceProjectID = util.UUIDToString(projectID)
		r.GranteeID = uuidToStringPtr(granteeID)
		out = append(out, r)
	}
	return out, rows.Err()
}

// projectInWorkspace confirms the project exists and belongs to the caller's
// workspace. Writes 404 and returns false otherwise.
func (h *Handler) projectInWorkspace(ctx context.Context, w http.ResponseWriter, projectUUID, wsUUID pgtype.UUID) bool {
	var ownerUUID pgtype.UUID
	err := h.pool.QueryRow(ctx,
		`SELECT workspace_id FROM project WHERE id = $1`, projectUUID,
	).Scan(&ownerUUID)
	if err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "project not found")
			return false
		}
		writeError(w, http.StatusInternalServerError, "project lookup failed")
		return false
	}
	if !ownerUUID.Valid || !wsUUID.Valid || ownerUUID.Bytes != wsUUID.Bytes {
		writeError(w, http.StatusNotFound, "project not found")
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// Validation helpers
// ---------------------------------------------------------------------------

func (h *Handler) validateGrantTarget(w http.ResponseWriter, r *http.Request, req grantRequest, wsUUID pgtype.UUID) (pgtype.UUID, pgtype.UUID, bool) {
	if !validGranteeType(req.GranteeType) {
		writeError(w, http.StatusBadRequest, "invalid grantee_type")
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	projectUUID, err := util.ParseUUID(strings.TrimSpace(req.ProjectID))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid project_id")
		return pgtype.UUID{}, pgtype.UUID{}, false
	}

	var granteeUUID pgtype.UUID
	if req.GranteeType == "workspace" {
		if req.GranteeID != nil && strings.TrimSpace(*req.GranteeID) != "" {
			writeError(w, http.StatusBadRequest, "workspace grant must not carry a grantee_id")
			return pgtype.UUID{}, pgtype.UUID{}, false
		}
	} else {
		if req.GranteeID == nil || strings.TrimSpace(*req.GranteeID) == "" {
			writeError(w, http.StatusBadRequest, "grantee_id required for this grantee_type")
			return pgtype.UUID{}, pgtype.UUID{}, false
		}
		granteeUUID, err = util.ParseUUID(strings.TrimSpace(*req.GranteeID))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid grantee_id")
			return pgtype.UUID{}, pgtype.UUID{}, false
		}
	}

	if !h.projectInWorkspace(r.Context(), w, projectUUID, wsUUID) {
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	return projectUUID, granteeUUID, true
}

// requireAdminOrOwner confirms the caller holds an admin or owner role in the
// workspace. Writes 403 and returns false when the check fails.
func (h *Handler) requireAdminOrOwner(ctx context.Context, w http.ResponseWriter, userID string, wsUUID pgtype.UUID) bool {
	uid, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid user id")
		return false
	}
	var role string
	err = h.pool.QueryRow(ctx,
		`SELECT role FROM member WHERE user_id = $1 AND workspace_id = $2`,
		uid, wsUUID,
	).Scan(&role)
	if err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusForbidden, "not a workspace member")
			return false
		}
		writeError(w, http.StatusInternalServerError, "role check failed")
		return false
	}
	if role != "owner" && role != "admin" {
		writeError(w, http.StatusForbidden, "admin or owner role required to manage project grants")
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// Small request helpers
// ---------------------------------------------------------------------------

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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
