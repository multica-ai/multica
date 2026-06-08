package connections

// handler.go exposes the workspace connection CRUD API (TECH-3108).
//
// Routes (all mounted under /api/workspaces/{id}/connections in router.go):
//
//	GET    /                  — list all connections (any workspace member)
//	POST   /                  — create a connection (admin/owner only)
//	GET    /{connId}          — get one connection (any workspace member)
//	PUT    /{connId}          — update a connection (admin/owner only)
//	DELETE /{connId}          — delete a connection (admin/owner only)
//
// Auth read is member-level; writes are gated to admin/owner by the router group.
// Auth tokens in auth_config are returned masked (non-empty → "***") to members.

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
)

var validNameRe = regexp.MustCompile(`^[a-z0-9_-]{1,64}$`)

// Handler exposes the connection store over HTTP.
type Handler struct {
	Store *Store
	pool  *pgxpool.Pool
}

// NewHandler constructs a Handler over the given Store and pool.
func NewHandler(pool *pgxpool.Pool) *Handler {
	return &Handler{Store: New(pool), pool: pool}
}

// --- request/response types -------------------------------------------------

type createRequest struct {
	Name                string               `json:"name"`
	DisplayName         string               `json:"display_name"`
	Type                string               `json:"type"`
	URL                 string               `json:"url"`
	Internal            bool                 `json:"internal"`
	AuthConfig          AuthConfig           `json:"auth_config"`
	EndpointPermissions []EndpointPermission `json:"endpoint_permissions"`
}

type updateRequest struct {
	DisplayName         string               `json:"display_name"`
	URL                 string               `json:"url"`
	Internal            bool                 `json:"internal"`
	AuthConfig          AuthConfig           `json:"auth_config"`
	EndpointPermissions []EndpointPermission `json:"endpoint_permissions"`
	Enabled             bool                 `json:"enabled"`
}

// --- handlers ---------------------------------------------------------------

// List — GET /api/workspaces/{id}/connections
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	wsID, ok := h.workspaceID(w, r)
	if !ok {
		return
	}
	conns, err := h.Store.List(r.Context(), wsID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list connections failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connections": maskAll(conns)})
}

// Get — GET /api/workspaces/{id}/connections/{connId}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	wsID, ok := h.workspaceID(w, r)
	if !ok {
		return
	}
	connID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "connId"), "connId")
	if !ok {
		return
	}
	c, err := h.Store.Get(r.Context(), connID, wsID)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "connection not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "get connection failed")
		return
	}
	writeJSON(w, http.StatusOK, mask(c))
}

// Create — POST /api/workspaces/{id}/connections
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	wsID, ok := h.workspaceID(w, r)
	if !ok {
		return
	}
	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !validNameRe.MatchString(req.Name) {
		writeError(w, http.StatusBadRequest, "name must be 1-64 lowercase alphanumeric, hyphen, or underscore characters")
		return
	}
	if req.DisplayName == "" {
		writeError(w, http.StatusBadRequest, "display_name required")
		return
	}
	if req.URL == "" {
		writeError(w, http.StatusBadRequest, "url required")
		return
	}
	c, err := h.Store.Create(r.Context(), CreateParams{
		WorkspaceID:         wsID,
		Name:                req.Name,
		DisplayName:         req.DisplayName,
		Type:                req.Type,
		URL:                 req.URL,
		Internal:            req.Internal,
		AuthConfig:          req.AuthConfig,
		EndpointPermissions: req.EndpointPermissions,
	})
	if errors.Is(err, ErrDuplicateName) {
		writeError(w, http.StatusConflict, "a connection with that name already exists")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = SyncCapability(r.Context(), h.pool, wsID, c)
	writeJSON(w, http.StatusCreated, mask(c))
}

// Update — PUT /api/workspaces/{id}/connections/{connId}
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	wsID, ok := h.workspaceID(w, r)
	if !ok {
		return
	}
	connID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "connId"), "connId")
	if !ok {
		return
	}
	var req updateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.DisplayName == "" {
		writeError(w, http.StatusBadRequest, "display_name required")
		return
	}
	if req.URL == "" {
		writeError(w, http.StatusBadRequest, "url required")
		return
	}
	c, err := h.Store.Update(r.Context(), UpdateParams{
		ID:                  connID,
		WorkspaceID:         wsID,
		DisplayName:         req.DisplayName,
		URL:                 req.URL,
		Internal:            req.Internal,
		AuthConfig:          req.AuthConfig,
		EndpointPermissions: req.EndpointPermissions,
		Enabled:             req.Enabled,
	})
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "connection not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update connection failed")
		return
	}
	_ = SyncCapability(r.Context(), h.pool, wsID, c)
	writeJSON(w, http.StatusOK, mask(c))
}

// Test — POST /api/workspaces/{id}/connections/test
func (h *Handler) Test(w http.ResponseWriter, r *http.Request) {
	wsID, ok := h.workspaceID(w, r)
	if !ok {
		return
	}
	var req testConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.URL == "" {
		writeError(w, http.StatusBadRequest, "url required")
		return
	}
	// When connection_id is provided, merge stored (real) credentials over masked form fields.
	if req.ConnectionID != "" {
		if connID, err := util.ParseUUID(req.ConnectionID); err == nil {
			if stored, err := h.Store.Get(r.Context(), connID, wsID); err == nil {
				if req.AuthConfig.BearerToken == "" {
					req.AuthConfig.BearerToken = stored.AuthConfig.BearerToken
				}
				if req.AuthConfig.APIKey == "" {
					req.AuthConfig.APIKey = stored.AuthConfig.APIKey
				}
				if req.AuthConfig.CFAccessID == "" {
					req.AuthConfig.CFAccessID = stored.AuthConfig.CFAccessID
				}
				if req.AuthConfig.CFAccessSecret == "" {
					req.AuthConfig.CFAccessSecret = stored.AuthConfig.CFAccessSecret
				}
			}
		}
	}
	result := doTestConnection(r.Context(), req)
	writeJSON(w, http.StatusOK, result)
}

// Delete — DELETE /api/workspaces/{id}/connections/{connId}
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	wsID, ok := h.workspaceID(w, r)
	if !ok {
		return
	}
	connID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "connId"), "connId")
	if !ok {
		return
	}
	// Load connection name before deleting for capability cleanup.
	conn, getErr := h.Store.Get(r.Context(), connID, wsID)
	if err := h.Store.Delete(r.Context(), connID, wsID); errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "connection not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "delete connection failed")
		return
	}
	if getErr == nil {
		_ = DeleteCapability(r.Context(), h.pool, wsID, conn.Name)
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- helpers ----------------------------------------------------------------

func (h *Handler) workspaceID(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	_, ok := middleware.MemberFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return pgtype.UUID{}, false
	}
	id, err := util.ParseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return pgtype.UUID{}, false
	}
	return id, true
}

func parseUUIDOrBadRequest(w http.ResponseWriter, s, field string) (pgtype.UUID, bool) {
	id, err := util.ParseUUID(s)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid "+field)
		return pgtype.UUID{}, false
	}
	return id, true
}

// mask returns a copy of the connection with auth secrets replaced by "***"
// so callers never receive raw credentials.
func mask(c Connection) Connection {
	m := c
	m.AuthConfig = maskAuth(c.AuthConfig)
	return m
}

func maskAll(conns []Connection) []Connection {
	out := make([]Connection, len(conns))
	for i, c := range conns {
		out[i] = mask(c)
	}
	return out
}

func maskAuth(a AuthConfig) AuthConfig {
	m := a
	if m.BearerToken != "" {
		m.BearerToken = "***"
	}
	if m.APIKey != "" {
		m.APIKey = "***"
	}
	if m.CFAccessSecret != "" {
		m.CFAccessSecret = "***"
	}
	return m
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
