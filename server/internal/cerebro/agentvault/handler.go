package agentvault

// handler.go exposes the per-agent access-table CRUD API (TECH-3196).
//
// Routes (mounted under /api/workspaces/{id}/agentvault in router.go):
//
//	GET    /access?agent_id=  — list an agent's granted boxes (any member)
//	PUT    /access            — upsert one {agent, vault, role} (admin/owner)
//	DELETE /access            — remove one {agent, vault} (admin/owner)
//
// This is the TECH-2962 admin-controlled table: only owners/admins write it, so
// an agent can never grant itself access. Writes are gated to admin/owner by the
// router group.

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
)

// Handler exposes the access store over HTTP.
type Handler struct {
	store *Store
}

// NewHandler constructs a Handler over a Store built from the given pool.
func NewHandler(pool *pgxpool.Pool) *Handler { return &Handler{store: NewStore(pool)} }

type accessWriteRequest struct {
	AgentID string `json:"agent_id"`
	Vault   string `json:"vault"`
	Role    string `json:"role"`
}

// List — GET /api/workspaces/{id}/agentvault/access?agent_id=
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	wsID, ok := h.workspaceID(w, r)
	if !ok {
		return
	}
	agentID, ok := parseUUIDOrBadRequest(w, r.URL.Query().Get("agent_id"), "agent_id")
	if !ok {
		return
	}
	rows, err := h.store.ListForAgent(r.Context(), util.UUIDToString(wsID), util.UUIDToString(agentID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if rows == nil {
		rows = []Access{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"access": rows})
}

// Set — PUT /api/workspaces/{id}/agentvault/access
func (h *Handler) Set(w http.ResponseWriter, r *http.Request) {
	wsID, ok := h.workspaceID(w, r)
	if !ok {
		return
	}
	var req accessWriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	agentID, ok := parseUUIDOrBadRequest(w, req.AgentID, "agent_id")
	if !ok {
		return
	}
	if strings.TrimSpace(req.Vault) == "" {
		writeError(w, http.StatusBadRequest, "vault required")
		return
	}
	role := strings.TrimSpace(req.Role)
	if role == "" {
		role = "read-only"
	}
	switch role {
	case "read-only", "member", "admin":
	default:
		writeError(w, http.StatusBadRequest, "role must be read-only, member, or admin")
		return
	}
	if err := h.store.SetAccess(r.Context(), util.UUIDToString(wsID), util.UUIDToString(agentID), req.Vault, role); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// Delete — DELETE /api/workspaces/{id}/agentvault/access
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	wsID, ok := h.workspaceID(w, r)
	if !ok {
		return
	}
	var req accessWriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	agentID, ok := parseUUIDOrBadRequest(w, req.AgentID, "agent_id")
	if !ok {
		return
	}
	if strings.TrimSpace(req.Vault) == "" {
		writeError(w, http.StatusBadRequest, "vault required")
		return
	}
	if err := h.store.DeleteAccess(r.Context(), util.UUIDToString(wsID), util.UUIDToString(agentID), req.Vault); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) workspaceID(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	if _, ok := middleware.MemberFromContext(r.Context()); !ok {
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
