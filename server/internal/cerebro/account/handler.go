package account

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
)

type Handler struct {
	Service *Service
}

func New(cerebro *cerebrodb.Queries, bus *events.Bus) *Handler {
	return &Handler{Service: NewService(cerebro, bus)}
}

type createAccountRequest struct {
	Provider      string `json:"provider"`
	LoginIdentity string `json:"login_identity"`
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := workspaceIDFromRequest(w, r)
	if !ok {
		return
	}
	rows, err := h.Service.ListWithAvailability(r.Context(), workspaceID)
	if err != nil {
		slog.Error("list cerebro accounts failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list accounts")
		return
	}
	resp := make([]accountResponse, len(rows))
	for i, row := range rows {
		resp[i] = accountResponseFromAvailabilityRow(row)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	workspaceID, accountID, ok := h.accountIDs(w, r)
	if !ok {
		return
	}
	a, err := h.Service.Get(r.Context(), workspaceID, accountID)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, accountResponseFromModel(a))
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := workspaceIDFromRequest(w, r)
	if !ok {
		return
	}
	actorID, ok := userIDFromRequest(w, r)
	if !ok {
		return
	}
	var req createAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	a, err := h.Service.Create(r.Context(), workspaceID, actorID, req.Provider, req.LoginIdentity)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, accountResponseFromModel(a))
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	workspaceID, accountID, ok := h.accountIDs(w, r)
	if !ok {
		return
	}
	actorID, ok := userIDFromRequest(w, r)
	if !ok {
		return
	}
	if _, err := h.Service.Delete(r.Context(), workspaceID, actorID, accountID); err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// updateControlsRequest patches the UI-driven control toggles. Pointer
// fields distinguish "field present in JSON" (non-nil) from "field
// omitted" (nil → leave column alone).
type updateControlsRequest struct {
	ExtraSpendOn *bool `json:"extra_spend_on"`
	PausedManual *bool `json:"paused_manual"`
}

// UpdateControls handles PATCH /accounts/{id}/controls (UI-driven).
func (h *Handler) UpdateControls(w http.ResponseWriter, r *http.Request) {
	workspaceID, accountID, ok := h.accountIDs(w, r)
	if !ok {
		return
	}
	actorID, ok := userIDFromRequest(w, r)
	if !ok {
		return
	}
	var req updateControlsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	a, err := h.Service.UpdateControls(r.Context(), workspaceID, actorID, accountID, ControlsUpdate{
		ExtraSpendOn: req.ExtraSpendOn,
		PausedManual: req.PausedManual,
	})
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, accountResponseFromModel(a))
}

// daemonUsageRequest is the payload posted by the local daemon when it
// observes a 429 retry-after or a usage-window-percent signal in adapter
// output, or exact token usage from the agent result. All fields are optional.
// RawMessage fields use pointer semantics so a 429 patch can ship without
// disturbing a previously-reported usage value (and vice versa). A pointer
// that wraps an explicit null value clears the column.
type daemonUsageRequest struct {
	UsageWindowPct json.RawMessage `json:"usage_window_pct,omitempty"`
	ThrottledUntil json.RawMessage `json:"throttled_until,omitempty"`
	Tokens         *int64          `json:"tokens,omitempty"`
}

// UpdateUsage handles the daemon-callable usage-telemetry endpoint.
// Exposed under /api/daemon/accounts/{id}/usage (workspace is resolved
// from the account row, not the URL — the daemon does not always carry
// the workspace ID alongside the account ID).
func (h *Handler) UpdateUsage(w http.ResponseWriter, r *http.Request) {
	accountID, err := util.ParseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid account id")
		return
	}
	var raw daemonUsageRequest
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	update, err := parseDaemonUsageUpdate(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	existing, err := h.Service.Cerebro.GetCerebroAccount(r.Context(), accountID)
	if err != nil {
		h.writeServiceError(w, r, ErrAccountNotFound)
		return
	}
	// Daemon-auth middleware scopes every request to one workspace; reject
	// a daemon trying to mutate an account that belongs to a different one
	// even though it knows the account ID (multi-tenant invariant).
	if daemonWS := middleware.DaemonWorkspaceIDFromContext(r.Context()); daemonWS != "" {
		parsed, perr := util.ParseUUID(daemonWS)
		if perr != nil || parsed != existing.WorkspaceID {
			h.writeServiceError(w, r, ErrAccountNotFound)
			return
		}
	}
	actorUUID := pgtype.UUID{}
	if daemonID := middleware.DaemonIDFromContext(r.Context()); daemonID != "" {
		if parsed, perr := util.ParseUUID(daemonID); perr == nil {
			actorUUID = parsed
		}
	}

	a, err := h.Service.UpdateUsage(r.Context(), existing.WorkspaceID, actorUUID, accountID, "daemon", update)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, accountResponseFromModel(a))
}

func parseDaemonUsageUpdate(raw daemonUsageRequest) (UsageUpdate, error) {
	var update UsageUpdate
	if raw.UsageWindowPct != nil {
		update.UsageWindowPct = &NullableFloat32{}
		// json.RawMessage of literal "null" means "clear".
		if string(raw.UsageWindowPct) != "null" {
			var v float32
			if err := json.Unmarshal(raw.UsageWindowPct, &v); err != nil {
				return UsageUpdate{}, errors.New("usage_window_pct must be a number")
			}
			update.UsageWindowPct.Value = &v
		}
	}
	if raw.ThrottledUntil != nil {
		update.ThrottledUntil = &NullableTime{}
		if string(raw.ThrottledUntil) != "null" {
			var s string
			if err := json.Unmarshal(raw.ThrottledUntil, &s); err != nil {
				return UsageUpdate{}, errors.New("throttled_until must be an RFC3339 timestamp or null")
			}
			t, err := time.Parse(time.RFC3339, s)
			if err != nil {
				return UsageUpdate{}, errors.New("throttled_until must be an RFC3339 timestamp or null")
			}
			ts := pgtype.Timestamptz{Time: t.UTC(), Valid: true}
			update.ThrottledUntil.Value = &ts
		}
	}
	if raw.Tokens != nil {
		if *raw.Tokens < 0 {
			return UsageUpdate{}, errors.New("tokens must be zero or greater")
		}
		update.Tokens = raw.Tokens
	}
	return update, nil
}

func (h *Handler) accountIDs(w http.ResponseWriter, r *http.Request) (workspaceID, accountID pgtype.UUID, ok bool) {
	workspaceID, ok = workspaceIDFromRequest(w, r)
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	accountID, err := util.ParseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid account id")
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	return workspaceID, accountID, true
}

func (h *Handler) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrAccountNotFound):
		writeError(w, http.StatusNotFound, "account not found")
	case errors.Is(err, ErrInvalidProvider):
		writeError(w, http.StatusBadRequest, "provider is required")
	case errors.Is(err, ErrInvalidLoginIdentity):
		writeError(w, http.StatusBadRequest, "login_identity is required")
	case errors.Is(err, ErrInvalidUsagePct):
		writeError(w, http.StatusBadRequest, "usage_window_pct must be between 0 and 100")
	case errors.Is(err, ErrInvalidTokenCount):
		writeError(w, http.StatusBadRequest, "tokens must be zero or greater")
	case errors.Is(err, ErrAccountAlreadyExists):
		writeError(w, http.StatusConflict, "account already exists for this provider and login_identity")
	default:
		slog.Error("cerebro accounts request failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "accounts request failed")
	}
}

func workspaceIDFromRequest(w http.ResponseWriter, r *http.Request) (workspaceID pgtype.UUID, ok bool) {
	id := middleware.WorkspaceIDFromContext(r.Context())
	if id == "" {
		id = chi.URLParam(r, "id")
	}
	if id == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return pgtype.UUID{}, false
	}
	parsed, err := util.ParseUUID(id)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace_id")
		return pgtype.UUID{}, false
	}
	return parsed, true
}

func userIDFromRequest(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	id := r.Header.Get("X-User-ID")
	if id == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return pgtype.UUID{}, false
	}
	parsed, err := util.ParseUUID(id)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return pgtype.UUID{}, false
	}
	return parsed, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
