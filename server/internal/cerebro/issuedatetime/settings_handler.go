package issuedatetime

// FIR-1597: per-workspace default for whether a newly scheduled issue auto-starts
// its assigned agent on the start date, the due date, or not at all. Surfaced
// inline under the issue-date-times feature in the Cerebro features settings UI.
// Kept in its own file (and handler) so upstream merges don't conflict. The
// endpoints live under the workspace route group (chi URL param "id"), matching
// the wakeup-limits and display-currency handlers.

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

// EventDateTimesSettingsChanged is emitted when the workspace default changes so
// every member's open settings UI refetches. Cerebro-only event type.
const EventDateTimesSettingsChanged = "issue_date_times_settings:changed"

// defaultAgentStartKind is the safe default: nothing auto-starts until someone
// opts in (per issue or via this workspace default).
const defaultAgentStartKind = "none"

// SettingsHandler serves the workspace-level default. It only needs the cerebro
// queries plus the event bus to broadcast changes.
type SettingsHandler struct {
	Cerebro *cerebrodb.Queries
	Bus     *events.Bus
}

func NewSettingsHandler(cerebro *cerebrodb.Queries, bus *events.Bus) *SettingsHandler {
	return &SettingsHandler{Cerebro: cerebro, Bus: bus}
}

type settingsResponse struct {
	DefaultAgentStartKind string `json:"default_agent_start_kind"`
}

type settingsRequest struct {
	DefaultAgentStartKind string `json:"default_agent_start_kind"`
}

// GetSettings returns the workspace's default agent auto-start choice. A missing
// settings row resolves to the default rather than a 404.
func (h *SettingsHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	wsUUID, err := util.ParseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return
	}

	resp := settingsResponse{DefaultAgentStartKind: defaultAgentStartKind}
	settings, err := h.Cerebro.GetCerebroWorkspaceSettings(r.Context(), wsUUID)
	switch {
	case err == nil:
		if validKind(settings.DefaultAgentStartKind) {
			resp.DefaultAgentStartKind = settings.DefaultAgentStartKind
		}
	case errors.Is(err, pgx.ErrNoRows):
		// No row yet -> default.
	default:
		writeError(w, http.StatusInternalServerError, "failed to load settings")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// SetSettings updates the workspace default. Admin/owner-gated by the router.
// Broadcasts issue_date_times_settings:changed so open UIs refetch.
func (h *SettingsHandler) SetSettings(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return
	}

	var req settingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !validKind(req.DefaultAgentStartKind) {
		writeError(w, http.StatusBadRequest, "default_agent_start_kind must be none, start, or due")
		return
	}

	var updatedBy pgtype.UUID
	if uid, err := util.ParseUUID(r.Header.Get("X-User-ID")); err == nil {
		updatedBy = uid
	}

	if err := h.Cerebro.UpsertCerebroWorkspaceDefaultAgentStartKind(r.Context(), cerebrodb.UpsertCerebroWorkspaceDefaultAgentStartKindParams{
		WorkspaceID:           wsUUID,
		DefaultAgentStartKind: req.DefaultAgentStartKind,
		UpdatedBy:             updatedBy,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update settings")
		return
	}

	if h.Bus != nil {
		h.Bus.Publish(events.Event{
			Type:        EventDateTimesSettingsChanged,
			WorkspaceID: workspaceID,
			ActorType:   "member",
			ActorID:     r.Header.Get("X-User-ID"),
			Payload:     settingsResponse{DefaultAgentStartKind: req.DefaultAgentStartKind},
		})
	}

	writeJSON(w, http.StatusOK, settingsResponse{DefaultAgentStartKind: req.DefaultAgentStartKind})
}

func validKind(s string) bool {
	switch s {
	case "none", "start", "due":
		return true
	default:
		return false
	}
}
