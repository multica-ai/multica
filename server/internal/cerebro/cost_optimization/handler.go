// CEREBRO: cost-optimization handler kept in dedicated file so upstream-merges don't conflict
package cost_optimization

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// EventCostOptimizationChanged is the WS event type emitted when a per-workspace
// saving mode is upserted or cleared. Cerebro-only — not part of the upstream
// protocol package.
const EventCostOptimizationChanged = "cost_optimization:changed"

// validModes mirrors CostSavingMode in packages/cerebro-cost-optimization. The
// DB CHECK enforces the same set; validating here turns a bad value into a 400
// instead of a 500 from the constraint.
var validModes = map[string]struct{}{
	"off":    {},
	"shadow": {},
	"on":     {},
}

// Handler exposes the cerebro cost-optimization HTTP endpoints. It keeps its own
// dependencies so the package can be wired into the router without touching the
// main Handler struct (which is owned by upstream).
type Handler struct {
	Queries     *cerebrodb.Queries
	MainQueries *db.Queries
	Bus         *events.Bus
}

// New constructs a cost_optimization.Handler. The caller wires it via dependency
// injection from the main router.
func New(queries *cerebrodb.Queries, mainQueries *db.Queries, bus *events.Bus) *Handler {
	return &Handler{Queries: queries, MainQueries: mainQueries, Bus: bus}
}

// listResponse is the GET response shape: a flat map of overrides. Defaults
// live on the frontend; missing rows return an empty map (not 404).
type listResponse struct {
	Overrides map[string]string `json:"overrides"`
}

// upsertRequest is the PUT body shape.
type upsertRequest struct {
	Mode string `json:"mode"`
}

// changedPayload is the WS event payload shape for cost_optimization:changed.
// Mode is empty when the override was cleared (reverted to the default).
type changedPayload struct {
	Key  string `json:"key"`
	Mode string `json:"mode"`
}

// List returns all per-workspace saving-mode overrides. Default semantics: when
// no row exists for a saving, the frontend applies the registry default — the
// API never 404s on a missing saving.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
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
	rows, err := h.Queries.ListCerebroCostOptimization(r.Context(), wsUUID)
	if err != nil {
		slog.Error("list cerebro cost optimization failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load cost optimization settings")
		return
	}

	overrides := make(map[string]string, len(rows))
	for _, row := range rows {
		overrides[row.SavingKey] = row.Mode
	}
	writeJSON(w, http.StatusOK, listResponse{Overrides: overrides})
}

// Upsert sets the mode for a single per-workspace saving. Admin-gated by the
// router. Broadcasts cost_optimization:changed to the whole workspace audience
// — unlike per-user feature flags, this is a workspace-level production setting
// every member's settings page should reflect.
func (h *Handler) Upsert(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	workspaceID := chi.URLParam(r, "id")
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace id required")
		return
	}
	key := chi.URLParam(r, "key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "saving key required")
		return
	}

	var req upsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if _, ok := validModes[req.Mode]; !ok {
		writeError(w, http.StatusBadRequest, "mode must be one of off, shadow, on")
		return
	}

	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return
	}
	// updated_by is best-effort audit metadata; a malformed header should not
	// block the write, so an unparseable id is left as a NULL pgtype.UUID.
	var updatedBy pgtype.UUID
	if userID != "" {
		if parsed, perr := util.ParseUUID(userID); perr == nil {
			updatedBy = parsed
		}
	}

	if err := h.Queries.UpsertCerebroCostOptimization(r.Context(), cerebrodb.UpsertCerebroCostOptimizationParams{
		WorkspaceID: wsUUID,
		SavingKey:   key,
		Mode:        req.Mode,
		UpdatedBy:   updatedBy,
	}); err != nil {
		slog.Error("upsert cerebro cost optimization failed", append(logger.RequestAttrs(r), "error", err, "saving_key", key)...)
		writeError(w, http.StatusInternalServerError, "failed to update cost optimization setting")
		return
	}

	h.Bus.Publish(events.Event{
		Type:        EventCostOptimizationChanged,
		WorkspaceID: workspaceID,
		ActorType:   "member",
		ActorID:     userID,
		Payload:     changedPayload{Key: key, Mode: req.Mode},
	})

	writeJSON(w, http.StatusOK, changedPayload{Key: key, Mode: req.Mode})
}

// Delete clears a saving's override, reverting it to the registry default.
// Admin-gated by the router. Deleting an absent row is a no-op (still 200) so
// the UI's "reset to default" is idempotent.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	workspaceID := chi.URLParam(r, "id")
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace id required")
		return
	}
	key := chi.URLParam(r, "key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "saving key required")
		return
	}

	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return
	}

	if err := h.Queries.DeleteCerebroCostOptimization(r.Context(), cerebrodb.DeleteCerebroCostOptimizationParams{
		WorkspaceID: wsUUID,
		SavingKey:   key,
	}); err != nil {
		slog.Error("delete cerebro cost optimization failed", append(logger.RequestAttrs(r), "error", err, "saving_key", key)...)
		writeError(w, http.StatusInternalServerError, "failed to clear cost optimization setting")
		return
	}

	// Empty mode signals a revert-to-default to listeners.
	h.Bus.Publish(events.Event{
		Type:        EventCostOptimizationChanged,
		WorkspaceID: workspaceID,
		ActorType:   "member",
		ActorID:     userID,
		Payload:     changedPayload{Key: key, Mode: ""},
	})

	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
