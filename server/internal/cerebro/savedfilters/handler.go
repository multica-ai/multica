// Package savedfilters hosts the REST surface for the cerebro saved-filters
// feature (FIR-1659). A saved filter is a named snapshot of the issue view-store
// filter state, owned by the member who created it and scoped to a single
// workspace (table cerebro_saved_filters, migration 9091). The snapshot is
// opaque JSON so new filter types need no backend change.
//
// Fase 3 adds sharing: a filter's `visibility` is private | shared | workspace,
// and `shared` filters name explicit group/member targets in
// cerebro_saved_filter_shares (migration 9093). Recipients get read/apply
// access only — just the owner can rename, re-share or delete.
//
// Fase 4 gates who may create a non-private (shared/workspace) filter behind the
// `create_shared_filters` group capability, OR-ed with an admin/owner override.
// Everyone may always create private filters.
//
// Wired into the router under /api/cerebro/saved-filters by the
// cerebro-saved-filters-routes CEREBRO-PATCH in server/cmd/server/router.go.
// Every endpoint is workspace-scoped via X-Workspace-ID and authenticated via
// X-User-ID.
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

// ShareCapability is the cerebro_group_capability value that lets a member
// create shared/workspace filters. Mirrors the frontend CerebroGroupCapability
// union ("create_shared_filters").
const ShareCapability = "create_shared_filters"

// visibility values for a saved filter.
const (
	visPrivate   = "private"
	visShared    = "shared"
	visWorkspace = "workspace"
)

// Handler serves the saved-filter endpoints. Cerebro holds the cerebro storage
// queries; there is no upstream dependency because saved filters live entirely
// in the cerebro schema (the membership-role read uses the upstream `member`
// table but is generated into cerebrodb).
type Handler struct {
	Cerebro *cerebrodb.Queries
}

func NewHandler(cerebro *cerebrodb.Queries) *Handler {
	return &Handler{Cerebro: cerebro}
}

// shareTarget is one group/member a shared filter is shared with.
type shareTarget struct {
	TargetType string `json:"target_type"` // "group" | "member"
	TargetID   string `json:"target_id"`
}

// response is the wire shape. filter_state is passed through verbatim as the raw
// JSON snapshot the frontend stored. shares is populated only on the single-GET
// (owner-only) used by the share dialog; the list omits it.
type response struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Surface     string          `json:"surface"`
	FilterState json.RawMessage `json:"filter_state"`
	Position    int32           `json:"position"`
	Visibility  string          `json:"visibility"`
	OwnerID     string          `json:"owner_id"`
	OwnerName   string          `json:"owner_name,omitempty"`
	IsOwner     bool            `json:"is_owner"`
	Shares      []shareTarget   `json:"shares,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

type createRequest struct {
	Name        string          `json:"name"`
	Surface     string          `json:"surface"`
	FilterState json.RawMessage `json:"filter_state"`
	Position    *int32          `json:"position"`
	Visibility  string          `json:"visibility"`
	Shares      []shareTarget   `json:"shares"`
}

type updateRequest struct {
	Name        *string         `json:"name"`
	FilterState json.RawMessage `json:"filter_state"`
	Position    *int32          `json:"position"`
	Visibility  *string         `json:"visibility"`
	Shares      *[]shareTarget  `json:"shares"`
}

// List returns every saved filter the calling member may use in this workspace
// (own + workspace-wide + shared to them), optionally narrowed to a surface.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	owner, ok := requireUserUUID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := workspaceFromContext(w, r)
	if !ok {
		return
	}
	rows, err := h.Cerebro.ListCerebroSavedFiltersVisibleToUser(r.Context(), cerebrodb.ListCerebroSavedFiltersVisibleToUserParams{
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
		out = append(out, response{
			ID:          util.UUIDToString(row.ID),
			Name:        row.Name,
			Surface:     row.Surface,
			FilterState: rawState(row.FilterState),
			Position:    row.Position,
			Visibility:  row.Visibility,
			OwnerID:     util.UUIDToString(row.OwnerID),
			OwnerName:   row.OwnerName,
			IsOwner:     uuidEqual(row.OwnerID, owner),
			CreatedAt:   row.CreatedAt.Time,
			UpdatedAt:   row.UpdatedAt.Time,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// CanShare reports whether the caller may create shared/workspace filters
// (Fase 4 gate). The save dialog calls it to decide whether to offer the
// "Selected colleagues" / "Whole team" choices. It never writes an error for a
// plain denial — a denial is a normal {can_share:false} result.
func (h *Handler) CanShare(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUserUUID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := workspaceFromContext(w, r)
	if !ok {
		return
	}
	role, err := h.Cerebro.GetCerebroMemberRole(r.Context(), cerebrodb.GetCerebroMemberRoleParams{
		WorkspaceID: wsUUID,
		UserID:      user,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}
	canShare := role == "owner" || role == "admin"
	if !canShare {
		hasCap, err := h.Cerebro.HasCerebroCapability(r.Context(), cerebrodb.HasCerebroCapabilityParams{
			WorkspaceID: wsUUID,
			UserID:      user,
			Capability:  ShareCapability,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "permission check failed")
			return
		}
		canShare = hasCap
	}
	writeJSON(w, http.StatusOK, map[string]bool{"can_share": canShare})
}

// Get returns a single owned filter together with its share targets. Owner-only:
// only the owner opens the share dialog that needs the current targets.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	existing, owner, ok := h.loadOwned(w, r)
	if !ok {
		return
	}
	resp := toResponse(existing, owner)
	shares, err := h.Cerebro.ListCerebroSavedFilterShares(r.Context(), existing.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load shares failed")
		return
	}
	resp.Shares = make([]shareTarget, 0, len(shares))
	for _, s := range shares {
		resp.Shares = append(resp.Shares, shareTarget{
			TargetType: s.TargetType,
			TargetID:   util.UUIDToString(s.TargetID),
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

// Create stores a new saved filter owned by the calling member. A non-private
// filter requires the create_shared_filters capability (or admin/owner).
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
	visibility, ok := normalizeVisibility(w, req.Visibility)
	if !ok {
		return
	}
	state, ok := normalizeFilterState(w, req.FilterState)
	if !ok {
		return
	}
	if visibility != visPrivate {
		if allowed, okCap := h.requireMayShare(w, r, wsUUID, owner); !okCap || !allowed {
			return
		}
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
		Visibility:  visibility,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create saved filter failed")
		return
	}
	if visibility == visShared {
		if !h.replaceShares(w, r, row.ID, owner, req.Shares) {
			return
		}
	}
	writeJSON(w, http.StatusCreated, toResponse(row, owner))
}

// Update renames a saved filter, replaces its snapshot, and/or changes its
// sharing. Only the owner may update it. Widening beyond private requires the
// create_shared_filters capability (or admin/owner).
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	existing, owner, ok := h.loadOwned(w, r)
	if !ok {
		return
	}
	wsUUID, ok := workspaceFromContext(w, r)
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

	visibility := existing.Visibility
	if req.Visibility != nil {
		v, valid := normalizeVisibility(w, *req.Visibility)
		if !valid {
			return
		}
		visibility = v
	}

	// Widening (or re-sharing) a filter beyond private requires the capability.
	if visibility != visPrivate {
		if allowed, okCap := h.requireMayShare(w, r, wsUUID, owner); !okCap || !allowed {
			return
		}
	}

	row, err := h.Cerebro.UpdateCerebroSavedFilter(r.Context(), cerebrodb.UpdateCerebroSavedFilterParams{
		ID:          existing.ID,
		Name:        name,
		FilterState: state,
		Position:    position,
		Visibility:  visibility,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update saved filter failed")
		return
	}

	// Reconcile share rows. A filter that is no longer 'shared' keeps no rows;
	// a 'shared' filter with an explicit Shares list is replaced wholesale.
	switch {
	case visibility != visShared:
		if err := h.Cerebro.DeleteCerebroSavedFilterShares(r.Context(), existing.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "update shares failed")
			return
		}
	case req.Shares != nil:
		if !h.replaceShares(w, r, existing.ID, owner, *req.Shares) {
			return
		}
	}

	writeJSON(w, http.StatusOK, toResponse(row, owner))
}

// Delete removes a saved filter (its shares cascade). Owner-only.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	existing, _, ok := h.loadOwned(w, r)
	if !ok {
		return
	}
	if err := h.Cerebro.DeleteCerebroSavedFilter(r.Context(), existing.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "delete saved filter failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// replaceShares clears and re-inserts the share target set for a filter. It
// validates each target before writing so a bad target_type/id is a 400 rather
// than a silent miss. Returns false (and writes the error) on failure.
func (h *Handler) replaceShares(w http.ResponseWriter, r *http.Request, filterID, owner pgtype.UUID, targets []shareTarget) bool {
	clean := make([]cerebrodb.CreateCerebroSavedFilterShareParams, 0, len(targets))
	for _, t := range targets {
		tt := strings.TrimSpace(t.TargetType)
		if tt != "group" && tt != "member" {
			writeError(w, http.StatusBadRequest, "invalid share target_type")
			return false
		}
		tid, err := util.ParseUUID(strings.TrimSpace(t.TargetID))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid share target_id")
			return false
		}
		clean = append(clean, cerebrodb.CreateCerebroSavedFilterShareParams{
			SavedFilterID: filterID,
			TargetType:    tt,
			TargetID:      tid,
			CreatedBy:     owner,
		})
	}
	if err := h.Cerebro.DeleteCerebroSavedFilterShares(r.Context(), filterID); err != nil {
		writeError(w, http.StatusInternalServerError, "update shares failed")
		return false
	}
	for _, p := range clean {
		if err := h.Cerebro.CreateCerebroSavedFilterShare(r.Context(), p); err != nil {
			writeError(w, http.StatusInternalServerError, "update shares failed")
			return false
		}
	}
	return true
}

// requireMayShare resolves the Fase 4 gate: admin/owner override OR the
// create_shared_filters group capability. It writes a 403 and returns
// (false,true) when the caller lacks it, or (false,false) on a lookup error
// (error already written). On success it returns (true,true).
func (h *Handler) requireMayShare(w http.ResponseWriter, r *http.Request, ws, user pgtype.UUID) (allowed bool, ok bool) {
	role, err := h.Cerebro.GetCerebroMemberRole(r.Context(), cerebrodb.GetCerebroMemberRoleParams{
		WorkspaceID: ws,
		UserID:      user,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return false, false
	}
	if role == "owner" || role == "admin" {
		return true, true
	}
	hasCap, err := h.Cerebro.HasCerebroCapability(r.Context(), cerebrodb.HasCerebroCapabilityParams{
		WorkspaceID: ws,
		UserID:      user,
		Capability:  ShareCapability,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return false, false
	}
	if !hasCap {
		writeError(w, http.StatusForbidden, "you do not have permission to share filters")
		return false, true
	}
	return true, true
}

// loadOwned resolves the {id} path param, then enforces both workspace scope and
// owner identity so a member can never touch another member's filter. It also
// returns the caller's UUID for response shaping.
func (h *Handler) loadOwned(w http.ResponseWriter, r *http.Request) (cerebrodb.CerebroSavedFilter, pgtype.UUID, bool) {
	owner, ok := requireUserUUID(w, r)
	if !ok {
		return cerebrodb.CerebroSavedFilter{}, pgtype.UUID{}, false
	}
	wsUUID, ok := workspaceFromContext(w, r)
	if !ok {
		return cerebrodb.CerebroSavedFilter{}, pgtype.UUID{}, false
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return cerebrodb.CerebroSavedFilter{}, pgtype.UUID{}, false
	}
	row, err := h.Cerebro.GetCerebroSavedFilter(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "saved filter not found")
			return cerebrodb.CerebroSavedFilter{}, pgtype.UUID{}, false
		}
		writeError(w, http.StatusInternalServerError, "load saved filter failed")
		return cerebrodb.CerebroSavedFilter{}, pgtype.UUID{}, false
	}
	// Hide existence of other workspaces'/members' rows behind a 404.
	if !uuidEqual(row.WorkspaceID, wsUUID) || !uuidEqual(row.OwnerID, owner) {
		writeError(w, http.StatusNotFound, "saved filter not found")
		return cerebrodb.CerebroSavedFilter{}, pgtype.UUID{}, false
	}
	return row, owner, true
}

// normalizeVisibility validates the visibility, defaulting empty to private.
func normalizeVisibility(w http.ResponseWriter, raw string) (string, bool) {
	v := strings.TrimSpace(raw)
	switch v {
	case "":
		return visPrivate, true
	case visPrivate, visShared, visWorkspace:
		return v, true
	default:
		writeError(w, http.StatusBadRequest, "invalid visibility")
		return "", false
	}
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

func toResponse(row cerebrodb.CerebroSavedFilter, caller pgtype.UUID) response {
	return response{
		ID:          util.UUIDToString(row.ID),
		Name:        row.Name,
		Surface:     row.Surface,
		FilterState: rawState(row.FilterState),
		Position:    row.Position,
		Visibility:  row.Visibility,
		OwnerID:     util.UUIDToString(row.OwnerID),
		IsOwner:     uuidEqual(row.OwnerID, caller),
		CreatedAt:   row.CreatedAt.Time,
		UpdatedAt:   row.UpdatedAt.Time,
	}
}

func rawState(b []byte) json.RawMessage {
	state := json.RawMessage(b)
	if len(state) == 0 {
		return json.RawMessage("{}")
	}
	return state
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
