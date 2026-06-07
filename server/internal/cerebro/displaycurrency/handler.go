// CEREBRO: display-currency handler kept in a dedicated file so upstream-merges
// don't conflict. FIR-40 — per-workspace display currency for cost.
package displaycurrency

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
)

// baseCurrency is the currency cost is stored in. Rates convert USD -> target.
const baseCurrency = "USD"

// EventDisplayCurrencyChanged is emitted when a workspace changes its display
// currency, so every member's open UI refetches. Cerebro-only event type.
const EventDisplayCurrencyChanged = "display_currency:changed"

// validCurrencies mirrors SUPPORTED_CURRENCIES in @multica/cerebro-format-cost
// and the DB CHECK constraint. Validating here turns a bad value into a 400.
var validCurrencies = map[string]struct{}{
	"USD": {},
	"DKK": {},
	"EUR": {},
}

// Handler exposes the cerebro display-currency endpoints. Keeps its own deps so
// it wires into the router without touching the upstream Handler struct.
type Handler struct {
	Queries *cerebrodb.Queries
	Bus     *events.Bus
}

// New constructs a displaycurrency.Handler.
func New(queries *cerebrodb.Queries, bus *events.Bus) *Handler {
	return &Handler{Queries: queries, Bus: bus}
}

// getResponse folds the per-workspace currency together with the cached rate so
// the frontend hook needs a single request. rate/fetched_at are null for USD or
// when today's rate hasn't been cached yet (the frontend then shows raw USD).
type getResponse struct {
	Currency  string   `json:"currency"`
	Base      string   `json:"base"`
	Rate      *float64 `json:"rate"`
	FetchedAt *string  `json:"fetched_at"`
}

type setRequest struct {
	Currency string `json:"currency"`
}

type changedPayload struct {
	Currency string `json:"currency"`
}

// Get returns the workspace's display currency plus the cached USD->currency
// rate. A missing settings row means the default (USD); a missing rate row for a
// non-USD currency returns a null rate so display degrades to USD, never 404s.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	wsUUID, err := util.ParseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return
	}

	currency := baseCurrency
	settings, err := h.Queries.GetCerebroWorkspaceSettings(r.Context(), wsUUID)
	switch {
	case err == nil:
		if _, ok := validCurrencies[settings.DisplayCurrency]; ok {
			currency = settings.DisplayCurrency
		}
	case errors.Is(err, pgx.ErrNoRows):
		// No row yet -> default USD.
	default:
		slog.Error("get cerebro workspace settings failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load display currency")
		return
	}

	resp := getResponse{Currency: currency, Base: baseCurrency}
	if currency != baseCurrency {
		rateRow, rateErr := h.Queries.GetCerebroExchangeRate(r.Context(), cerebrodb.GetCerebroExchangeRateParams{
			BaseCurrency:   baseCurrency,
			TargetCurrency: currency,
		})
		switch {
		case rateErr == nil:
			if f, convErr := rateRow.Rate.Float64Value(); convErr == nil && f.Valid {
				rate := f.Float64
				resp.Rate = &rate
			}
			if rateRow.FetchedAt.Valid {
				ts := rateRow.FetchedAt.Time.UTC().Format("2006-01-02T15:04:05Z07:00")
				resp.FetchedAt = &ts
			}
		case errors.Is(rateErr, pgx.ErrNoRows):
			// No cached rate yet -> null rate, frontend shows raw USD.
		default:
			slog.Error("get cerebro exchange rate failed", append(logger.RequestAttrs(r), "error", rateErr)...)
			writeError(w, http.StatusInternalServerError, "failed to load exchange rate")
			return
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// SetCurrency changes the workspace display currency. Admin/owner-gated by the
// router. Broadcasts display_currency:changed so every member's UI refetches.
func (h *Handler) SetCurrency(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return
	}

	var req setRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if _, ok := validCurrencies[req.Currency]; !ok {
		writeError(w, http.StatusBadRequest, "currency must be one of USD, DKK, EUR")
		return
	}

	userID := r.Header.Get("X-User-ID")
	var updatedBy pgtype.UUID
	if uid, err := util.ParseUUID(userID); err == nil {
		updatedBy = uid
	}

	if err := h.Queries.UpsertCerebroWorkspaceDisplayCurrency(r.Context(), cerebrodb.UpsertCerebroWorkspaceDisplayCurrencyParams{
		WorkspaceID:     wsUUID,
		DisplayCurrency: req.Currency,
		UpdatedBy:       updatedBy,
	}); err != nil {
		slog.Error("upsert cerebro workspace display currency failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update display currency")
		return
	}

	if h.Bus != nil {
		h.Bus.Publish(events.Event{
			Type:        EventDisplayCurrencyChanged,
			WorkspaceID: workspaceID,
			ActorType:   "member",
			ActorID:     userID,
			Payload:     changedPayload{Currency: req.Currency},
		})
	}

	writeJSON(w, http.StatusOK, setRequest{Currency: req.Currency})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
