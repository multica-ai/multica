// CEREBRO: feature flag handler kept in dedicated file so upstream-merges don't conflict
package feature_flags

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
)

// EventFeatureFlagChanged is the WS event type emitted when a per-user flag is
// upserted. Cerebro-only — not part of the upstream protocol package.
const EventFeatureFlagChanged = "feature_flag:changed"

// Handler exposes the cerebro feature-flag HTTP endpoints. It keeps its own
// dependencies so the package can be wired into the router without touching
// the main Handler struct (which is owned by upstream).
type Handler struct {
	Queries *cerebrodb.Queries
	Bus     *events.Bus
}

// New constructs a feature_flags.Handler. The caller wires it via dependency
// injection from the main router.
func New(queries *cerebrodb.Queries, bus *events.Bus) *Handler {
	return &Handler{Queries: queries, Bus: bus}
}

// listResponse is the GET response shape. `overrides` are the signed-in
// member's personal overrides. `workspace_overrides` are the owner-set
// workspace-level values that apply to everyone, and `locked` marks the flag
// keys whose workspace value members may NOT override. Defaults live on the
// frontend; missing rows return empty maps (not 404).
type listResponse struct {
	Overrides          map[string]bool `json:"overrides"`
	WorkspaceOverrides map[string]bool `json:"workspace_overrides"`
	Locked             map[string]bool `json:"locked"`
}

// upsertRequest is the per-user PUT body shape.
type upsertRequest struct {
	Enabled bool `json:"enabled"`
}

// upsertWorkspaceRequest is the owner/admin PUT body shape for the
// workspace-level override. `locked=true` forbids members from overriding.
type upsertWorkspaceRequest struct {
	Enabled bool `json:"enabled"`
	Locked  bool `json:"locked"`
}

// upsertPayload is the WS event payload shape for feature_flag:changed.
type upsertPayload struct {
	Key     string `json:"key"`
	Enabled bool   `json:"enabled"`
}

// workspaceFlagPayload is the WS event payload for a workspace-level change.
// It is broadcast to the whole workspace (not a single user) because it
// affects every member's effective flag value.
//
// Workspace-level rows are stored under the all-zero sentinel user_id; that
// literal lives in the SQL queries (feature_flags.sql).
type workspaceFlagPayload struct {
	Key     string `json:"key"`
	Enabled bool   `json:"enabled"`
	Locked  bool   `json:"locked"`
	Scope   string `json:"scope"` // always "workspace"
}

// List returns all per-user overrides for the authenticated user in the
// workspace. Default-on semantics: when no row exists for a flag, the
// frontend applies the default — the API never 404s on a missing flag.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := chi.URLParam(r, "id")
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace id required")
		return
	}

	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return
	}
	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	rows, err := h.Queries.ListCerebroFeatureFlags(r.Context(), cerebrodb.ListCerebroFeatureFlagsParams{
		WorkspaceID: wsUUID,
		UserID:      userUUID,
	})
	if err != nil {
		slog.Error("list cerebro feature flags failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load feature flags")
		return
	}

	overrides := make(map[string]bool, len(rows))
	for _, row := range rows {
		overrides[row.FlagKey] = row.Enabled
	}

	// Workspace-level overrides apply to every member; fetch them too so the
	// frontend can resolve precedence (locked workspace value > personal >
	// default) and disable the personal toggle where the owner locked it.
	wsRows, err := h.Queries.ListCerebroWorkspaceFeatureFlags(r.Context(), wsUUID)
	if err != nil {
		slog.Error("list cerebro workspace feature flags failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load feature flags")
		return
	}
	workspaceOverrides := make(map[string]bool, len(wsRows))
	locked := make(map[string]bool, len(wsRows))
	for _, row := range wsRows {
		workspaceOverrides[row.FlagKey] = row.Enabled
		if row.Locked {
			locked[row.FlagKey] = true
		}
	}

	writeJSON(w, http.StatusOK, listResponse{
		Overrides:          overrides,
		WorkspaceOverrides: workspaceOverrides,
		Locked:             locked,
	})
}

// Upsert sets the enabled state for a single per-user flag. Broadcasts
// feature_flag:changed restricted to the user's own audience so other
// workspace members don't receive unrelated personal-toggle traffic.
func (h *Handler) Upsert(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := chi.URLParam(r, "id")
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace id required")
		return
	}
	key := chi.URLParam(r, "key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "flag key required")
		return
	}

	var req upsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return
	}
	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	if err := h.Queries.UpsertCerebroFeatureFlag(r.Context(), cerebrodb.UpsertCerebroFeatureFlagParams{
		WorkspaceID: wsUUID,
		UserID:      userUUID,
		FlagKey:     key,
		Enabled:     req.Enabled,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "feature flag not found")
			return
		}
		slog.Error("upsert cerebro feature flag failed", append(logger.RequestAttrs(r), "error", err, "flag_key", key)...)
		writeError(w, http.StatusInternalServerError, "failed to update feature flag")
		return
	}

	// Restrict the WS event to the owning user — flag overrides are personal,
	// other members shouldn't see the toggle traffic.
	h.Bus.Publish(events.Event{
		Type:            EventFeatureFlagChanged,
		WorkspaceID:     workspaceID,
		ActorType:       "member",
		ActorID:         userID,
		Payload:         upsertPayload{Key: key, Enabled: req.Enabled},
		AudienceUserIDs: []string{userID},
	})

	writeJSON(w, http.StatusOK, upsertPayload{Key: key, Enabled: req.Enabled})
}

// UpsertWorkspace sets the workspace-level override for a flag. The route is
// gated to owner/admin in the router, so any caller reaching here is
// authorised. Broadcasts feature_flag:changed to the WHOLE workspace because
// the change affects every member's effective value.
func (h *Handler) UpsertWorkspace(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace id required")
		return
	}
	key := chi.URLParam(r, "key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "flag key required")
		return
	}

	var req upsertWorkspaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return
	}
	if err := h.Queries.UpsertCerebroWorkspaceFeatureFlag(r.Context(), cerebrodb.UpsertCerebroWorkspaceFeatureFlagParams{
		WorkspaceID: wsUUID,
		FlagKey:     key,
		Enabled:     req.Enabled,
		Locked:      req.Locked,
	}); err != nil {
		slog.Error("upsert cerebro workspace feature flag failed", append(logger.RequestAttrs(r), "error", err, "flag_key", key)...)
		writeError(w, http.StatusInternalServerError, "failed to update feature flag")
		return
	}

	h.Bus.Publish(events.Event{
		Type:        EventFeatureFlagChanged,
		WorkspaceID: workspaceID,
		ActorType:   "member",
		Payload:     workspaceFlagPayload{Key: key, Enabled: req.Enabled, Locked: req.Locked, Scope: "workspace"},
	})

	writeJSON(w, http.StatusOK, workspaceFlagPayload{Key: key, Enabled: req.Enabled, Locked: req.Locked, Scope: "workspace"})
}

// DeleteWorkspace clears the workspace-level override for a flag, reverting
// every member to their personal override or the registry default. Owner/admin
// only (gated in the router).
func (h *Handler) DeleteWorkspace(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace id required")
		return
	}
	key := chi.URLParam(r, "key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "flag key required")
		return
	}

	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return
	}
	if err := h.Queries.DeleteCerebroWorkspaceFeatureFlag(r.Context(), cerebrodb.DeleteCerebroWorkspaceFeatureFlagParams{
		WorkspaceID: wsUUID,
		FlagKey:     key,
	}); err != nil {
		slog.Error("delete cerebro workspace feature flag failed", append(logger.RequestAttrs(r), "error", err, "flag_key", key)...)
		writeError(w, http.StatusInternalServerError, "failed to clear feature flag")
		return
	}

	h.Bus.Publish(events.Event{
		Type:        EventFeatureFlagChanged,
		WorkspaceID: workspaceID,
		ActorType:   "member",
		Payload:     workspaceFlagPayload{Key: key, Enabled: false, Locked: false, Scope: "workspace"},
	})

	w.WriteHeader(http.StatusNoContent)
}

// requireUserID mirrors handler.requireUserID — kept private here so the
// package compiles without importing the main handler package (which would
// pull in its full dependency surface and bloat the cerebro module graph).
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
