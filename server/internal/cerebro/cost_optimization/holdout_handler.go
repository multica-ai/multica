// CEREBRO: cost-optimization per-saving holdout-share handler kept in its own
// file so upstream-merges don't conflict (FIR-2640).
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
)

// EventCostOptimizationHoldoutChanged is the WS event emitted when a saving's
// holdout share is set or cleared for a workspace. Cerebro-only.
const EventCostOptimizationHoldoutChanged = "cost_optimization:holdout_changed"

// holdoutListResponse is the GET response: a flat map of per-saving overrides.
// A saving missing from the map has no override and uses the server default.
type holdoutListResponse struct {
	Overrides map[string]int `json:"overrides"`
}

// holdoutUpsertRequest is the PUT body shape.
type holdoutUpsertRequest struct {
	Pct int `json:"holdout_pct"`
}

// holdoutChangedPayload is the WS event payload. Pct is nil when the override
// was cleared (reverted to the server default).
type holdoutChangedPayload struct {
	Key string `json:"key"`
	Pct *int   `json:"holdout_pct"`
}

// ListHoldout returns every per-saving holdout override for the workspace. Any
// member may read it; missing savings simply aren't in the map.
func (h *Handler) ListHoldout(w http.ResponseWriter, r *http.Request) {
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

	rows, err := h.Queries.ListCerebroCostOptimizationHoldout(r.Context(), wsUUID)
	if err != nil {
		slog.Error("list cerebro cost optimization holdout failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load holdout settings")
		return
	}

	overrides := make(map[string]int, len(rows))
	for _, row := range rows {
		overrides[row.SavingKey] = int(row.HoldoutPct)
	}
	writeJSON(w, http.StatusOK, holdoutListResponse{Overrides: overrides})
}

// UpsertHoldout sets the holdout share for a single saving. Admin-gated by the
// router. Rejects out-of-range values (0-100) before they reach the DB CHECK,
// turning a bad write into a 400 instead of a 500. Broadcasts to the workspace.
func (h *Handler) UpsertHoldout(w http.ResponseWriter, r *http.Request) {
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

	var req holdoutUpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Pct < 0 || req.Pct > 100 {
		writeError(w, http.StatusBadRequest, "holdout_pct must be between 0 and 100")
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

	if err := h.Queries.UpsertCerebroCostOptimizationHoldout(r.Context(), cerebrodb.UpsertCerebroCostOptimizationHoldoutParams{
		WorkspaceID: wsUUID,
		SavingKey:   key,
		HoldoutPct:  int32(req.Pct),
		UpdatedBy:   updatedBy,
	}); err != nil {
		slog.Error("upsert cerebro cost optimization holdout failed", append(logger.RequestAttrs(r), "error", err, "saving_key", key)...)
		writeError(w, http.StatusInternalServerError, "failed to update holdout setting")
		return
	}

	pct := req.Pct
	h.Bus.Publish(events.Event{
		Type:        EventCostOptimizationHoldoutChanged,
		WorkspaceID: workspaceID,
		ActorType:   "member",
		ActorID:     userID,
		Payload:     holdoutChangedPayload{Key: key, Pct: &pct},
	})

	writeJSON(w, http.StatusOK, holdoutChangedPayload{Key: key, Pct: &pct})
}

// DeleteHoldout clears a saving's holdout override, reverting it to the server
// default. Admin-gated by the router. Deleting an absent row is a no-op (still
// 204) so the UI's "reset to default" is idempotent.
func (h *Handler) DeleteHoldout(w http.ResponseWriter, r *http.Request) {
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

	if err := h.Queries.DeleteCerebroCostOptimizationHoldout(r.Context(), cerebrodb.DeleteCerebroCostOptimizationHoldoutParams{
		WorkspaceID: wsUUID,
		SavingKey:   key,
	}); err != nil {
		slog.Error("delete cerebro cost optimization holdout failed", append(logger.RequestAttrs(r), "error", err, "saving_key", key)...)
		writeError(w, http.StatusInternalServerError, "failed to clear holdout setting")
		return
	}

	// Nil pct signals a revert-to-default to listeners.
	h.Bus.Publish(events.Event{
		Type:        EventCostOptimizationHoldoutChanged,
		WorkspaceID: workspaceID,
		ActorType:   "member",
		ActorID:     userID,
		Payload:     holdoutChangedPayload{Key: key, Pct: nil},
	})

	w.WriteHeader(http.StatusNoContent)
}
