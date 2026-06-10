package wakeup

// TECH-3298: per-workspace self-wakeup limit settings, surfaced inline under the
// wakeup feature in the Cerebro features settings UI. Kept in its own file so
// upstream merges don't conflict. These endpoints live under the workspace
// route group (chi URL param "id"), matching the display-currency handler.

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
)

// EventWakeupSettingsChanged is emitted when the self-wakeup limits change so
// every member's open settings UI refetches. Cerebro-only event type.
const EventWakeupSettingsChanged = "wakeup_settings:changed"

// Bounds keep a stray value from disabling wakeups entirely or scheduling them
// absurdly far apart. They mirror the input constraints in the settings UI.
const (
	minMaxSelfPerIssue = 1
	maxMaxSelfPerIssue = 100
	minMinIntervalMin  = 0
	maxMinIntervalMin  = 1440 // 24h
)

type wakeupSettingsResponse struct {
	MaxSelfPerIssue    int `json:"max_self_per_issue"`
	MinIntervalMinutes int `json:"min_interval_minutes"`
}

type wakeupSettingsRequest struct {
	MaxSelfPerIssue    int `json:"max_self_per_issue"`
	MinIntervalMinutes int `json:"min_interval_minutes"`
}

// GetSettings returns the workspace's self-wakeup limits. A missing settings row
// resolves to the defaults rather than a 404.
func (h *Handler) GetSettings(w http.ResponseWriter, r *http.Request) {
	wsUUID, err := util.ParseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return
	}

	resp := wakeupSettingsResponse{
		MaxSelfPerIssue:    defaultMaxSelfWakeupsPerIssue,
		MinIntervalMinutes: defaultMinWakeupIntervalMin,
	}
	settings, err := h.Service.Cerebro.GetCerebroWorkspaceSettings(r.Context(), wsUUID)
	switch {
	case err == nil:
		if settings.WakeupMaxSelfPerIssue > 0 {
			resp.MaxSelfPerIssue = int(settings.WakeupMaxSelfPerIssue)
		}
		if settings.WakeupMinIntervalMinutes >= 0 {
			resp.MinIntervalMinutes = int(settings.WakeupMinIntervalMinutes)
		}
	case errors.Is(err, pgx.ErrNoRows):
		// No row yet -> defaults.
	default:
		writeError(w, http.StatusInternalServerError, "failed to load wakeup settings")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// SetSettings updates the workspace's self-wakeup limits. Admin/owner-gated by
// the router. Broadcasts wakeup_settings:changed so open UIs refetch.
func (h *Handler) SetSettings(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return
	}

	var req wakeupSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.MaxSelfPerIssue < minMaxSelfPerIssue || req.MaxSelfPerIssue > maxMaxSelfPerIssue {
		writeError(w, http.StatusBadRequest, "max_self_per_issue must be between 1 and 100")
		return
	}
	if req.MinIntervalMinutes < minMinIntervalMin || req.MinIntervalMinutes > maxMinIntervalMin {
		writeError(w, http.StatusBadRequest, "min_interval_minutes must be between 0 and 1440")
		return
	}

	var updatedBy pgtype.UUID
	if uid, err := util.ParseUUID(r.Header.Get("X-User-ID")); err == nil {
		updatedBy = uid
	}

	if err := h.Service.Cerebro.UpsertCerebroWorkspaceWakeupLimits(r.Context(), cerebrodb.UpsertCerebroWorkspaceWakeupLimitsParams{
		WorkspaceID:              wsUUID,
		WakeupMaxSelfPerIssue:    int32(req.MaxSelfPerIssue),
		WakeupMinIntervalMinutes: int32(req.MinIntervalMinutes),
		UpdatedBy:                updatedBy,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update wakeup settings")
		return
	}

	if h.Service.Bus != nil {
		h.Service.Bus.Publish(events.Event{
			Type:        EventWakeupSettingsChanged,
			WorkspaceID: workspaceID,
			ActorType:   "member",
			ActorID:     r.Header.Get("X-User-ID"),
			Payload: wakeupSettingsResponse{
				MaxSelfPerIssue:    req.MaxSelfPerIssue,
				MinIntervalMinutes: req.MinIntervalMinutes,
			},
		})
	}

	writeJSON(w, http.StatusOK, wakeupSettingsResponse{
		MaxSelfPerIssue:    req.MaxSelfPerIssue,
		MinIntervalMinutes: req.MinIntervalMinutes,
	})
}
