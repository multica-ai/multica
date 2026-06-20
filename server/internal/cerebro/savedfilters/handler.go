// Package savedfilters hosts the REST surface for the cerebro "personal saved
// filters" feature (FIR-1659 Fase 2). A saved filter is a named snapshot of the
// issue view-store filter state, owned by the member who created it and scoped
// to a single workspace (table cerebro_saved_filters, migration 9091). The
// snapshot is opaque JSON so new filter types need no backend change.
//
// Wired into the router under /api/cerebro/saved-filters by the
// cerebro-saved-filters-routes CEREBRO-PATCH in server/cmd/server/router.go.
// Every endpoint is workspace-scoped via X-Workspace-ID and authenticated via
// X-User-ID; a member only ever sees and mutates their own filters.
//
// Sharing (Fase 3) and the "may create shared filters" group capability
// (Fase 4) extend this package later; nothing here assumes a single owner is
// the only access model.
package savedfilters

import (
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

const maxNameLen = 120

// Handler serves the saved-filter endpoints. Cerebro holds the cerebro storage
// queries; there is no upstream dependency because saved filters live entirely
// in the cerebro schema.
type Handler struct {
	Cerebro *cerebrodb.Queries
}

func NewHandler(cerebro *cerebrodb.Queries) *Handler {
	return &Handler{Cerebro: cerebro}
}

// response is the wire shape. filter_state is passed through verbatim as the
// raw JSON snapshot the frontend stored.
type response struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Surface     string          `json:"surface"`
	FilterState json.RawMessage `json:"filter_state"`
	Position    int32           `json:"position"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

type createRequest struct {
	Name        string          `json:"name"`
	Surface     string          `json:"surface"`
	FilterState json.RawMessage `json:"filter_state"`
	Position    *int32          `json:"position"`
}

type updateRequest struct {
	Name        *string         `json:"name"`
	FilterState json.RawMessage `json:"filter_state"`
	Position    *int32          `json:"position"`
}

// List returns the calling member's saved filters for this workspace, optionally
// narrowed to a single surface via the ?surface= query param.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	owner, ok := requireUserUUID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := workspaceFromContext(w, r)
	if !ok {
		return
	}
	rows, err := h.Cerebro.ListCerebroSavedFiltersByOwner(r.Context(), cerebrodb.ListCerebroSavedFiltersByOwnerParams{
		WorkspaceID: wsUUID,
		OwnerID:     owner,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load saved filters failed")
		return
	}
	surface := strings.TrimSpace(r.URL.Query().Get("surface"))
	out := make([]response, 0, len(rows))
	for _, row := range rows {
		if surface != "" && row.Surface != surface {
			continue
		}
		out = append(out, toResponse(row))
	}
	writeJSON(w, http.StatusOK, out)
}

// Create stores a new saved filter owned by the calling member.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	owner, ok := requireUserUUID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := workspaceFromContext(w, r)
	if !ok {
		return
	}
	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if len([]rune(name)) > maxNameLen {
		writeError(w, http.StatusBadRequest, "name too long")
		return
	}
	surface := strings.TrimSpace(req.Surface)
	if surface == "" {
		surface = "issues"
	}
	state, ok := normalizeFilterState(w, req.FilterState)
	if !ok {
		return
	}
	var position int32
	if req.Position != nil {
		position = *req.Position
	}
	row, err := h.Cerebro.CreateCerebroSavedFilter(r.Context(), cerebrodb.CreateCerebroSavedFilterParams{
		WorkspaceID: wsUUID,
		OwnerID:     owner,
		Name:        name,
		Surface:     surface,
		FilterState: state,
		Position:    position,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create saved filter failed")
		return
	}
	writeJSON(w, http.StatusCreated, toResponse(row))
}

// Update renames a saved filter and/or replaces its snapshot. Only the owner in
// the current workspace may update it.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	existing, ok := h.loadOwned(w, r)
	if !ok {
		return
	}
	var req updateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	name := existing.Name
	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if trimmed == "" {
			writeError(w, http.StatusBadRequest, "name cannot be empty")
			return
		}
		if len([]rune(trimmed)) > maxNameLen {
			writeError(w, http.StatusBadRequest, "name too long")
			return
		}
		name = trimmed
	}

	state := existing.FilterState
	if req.FilterState != nil {
		normalized, valid := normalizeFilterState(w, req.FilterState)
		if !valid {
			return
		}
		state = normalized
	}

	position := existing.Position
	if req.Position != nil {
		position = *req.Position
	}

	row, err := h.Cerebro.UpdateCerebroSavedFilter(r.Context(), cerebrodb.UpdateCerebroSavedFilterParams{
		ID:          existing.ID,
		Name:        name,
		FilterState: state,
		Position:    position,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update saved filter failed")
		return
	}
	writeJSON(w, http.StatusOK, toResponse(row))
}

// Delete removes a saved filter. Only the owner in the current workspace may
// delete it.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	existing, ok := h.loadOwned(w, r)
	if !ok {
		return
	}
	if err := h.Cerebro.DeleteCerebroSavedFilter(r.Context(), existing.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "delete saved filter failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// loadOwned resolves the {id} path param, then enforces both workspace scope and
// owner identity so a member can never touch another member's filter.
func (h *Handler) loadOwned(w http.ResponseWriter, r *http.Request) (cerebrodb.CerebroSavedFilter, bool) {
	owner, ok := requireUserUUID(w, r)
	if !ok {
		return cerebrodb.CerebroSavedFilter{}, false
	}
	wsUUID, ok := workspaceFromContext(w, r)
	if !ok {
		return cerebrodb.CerebroSavedFilter{}, false
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return cerebrodb.CerebroSavedFilter{}, false
	}
	row, err := h.Cerebro.GetCerebroSavedFilter(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "saved filter not found")
			return cerebrodb.CerebroSavedFilter{}, false
		}
		writeError(w, http.StatusInternalServerError, "load saved filter failed")
		return cerebrodb.CerebroSavedFilter{}, false
	}
	// Hide existence of other workspaces'/members' rows behind a 404.
	if !uuidEqual(row.WorkspaceID, wsUUID) || !uuidEqual(row.OwnerID, owner) {
		writeError(w, http.StatusNotFound, "saved filter not found")
		return cerebrodb.CerebroSavedFilter{}, false
	}
	return row, true
}

// normalizeFilterState validates that the snapshot is a JSON object (or empty,
// defaulting to {}). It returns the bytes to persist.
func normalizeFilterState(w http.ResponseWriter, raw json.RawMessage) ([]byte, bool) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return []byte("{}"), true
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		writeError(w, http.StatusBadRequest, "filter_state must be a JSON object")
		return nil, false
	}
	return []byte(trimmed), true
}

func toResponse(row cerebrodb.CerebroSavedFilter) response {
	state := json.RawMessage(row.FilterState)
	if len(state) == 0 {
		state = json.RawMessage("{}")
	}
	return response{
		ID:          util.UUIDToString(row.ID),
		Name:        row.Name,
		Surface:     row.Surface,
		FilterState: state,
		Position:    row.Position,
		CreatedAt:   row.CreatedAt.Time,
		UpdatedAt:   row.UpdatedAt.Time,
	}
}

// ---------------------------------------------------------------------------
// Request helpers (mirror the cerebro/issuedatetime handler conventions)
// ---------------------------------------------------------------------------

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

func requireUserUUID(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return pgtype.UUID{}, false
	}
	uid, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user")
		return pgtype.UUID{}, false
	}
	return uid, true
}

func uuidEqual(a, b pgtype.UUID) bool {
	return a.Valid && b.Valid && a.Bytes == b.Bytes
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
