package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// SquadAttentionResponse is a squad's adaptive attention (spec 2.6). pending_cap
// and weekly_cap are the effective caps filings are checked against. reason says
// why they are what they are: pinned, sample_too_small, below_threshold,
// above_threshold or default.
type SquadAttentionResponse struct {
	SquadID            string   `json:"squad_id"`
	PendingCap         int      `json:"pending_cap"`
	WeeklyCap          int      `json:"weekly_cap"`
	ComputedPendingCap int      `json:"computed_pending_cap"`
	ComputedWeeklyCap  int      `json:"computed_weekly_cap"`
	Pinned             bool     `json:"pinned"`
	PinnedPendingCap   *int     `json:"pinned_pending_cap"`
	PinnedWeeklyCap    *int     `json:"pinned_weekly_cap"`
	AcceptanceRate     *float64 `json:"acceptance_rate"`
	SampleSize         int      `json:"sample_size"`
	Reason             string   `json:"reason"`
	ComputedAt         *string  `json:"computed_at"`
}

func squadAttentionToResponse(squadID string, v service.SquadAttentionView) SquadAttentionResponse {
	resp := SquadAttentionResponse{
		SquadID:            squadID,
		PendingCap:         v.Effective.Pending,
		WeeklyCap:          v.Effective.Weekly,
		ComputedPendingCap: v.Computed.Pending,
		ComputedWeeklyCap:  v.Computed.Weekly,
		Pinned:             v.Pinned,
		AcceptanceRate:     v.Rate,
		SampleSize:         int(v.SampleSize),
		Reason:             v.Reason,
	}
	if v.PinnedCaps != nil {
		pending, weekly := v.PinnedCaps.Pending, v.PinnedCaps.Weekly
		resp.PinnedPendingCap, resp.PinnedWeeklyCap = &pending, &weekly
	}
	if v.ComputedAt != nil {
		s := v.ComputedAt.UTC().Format(time.RFC3339)
		resp.ComputedAt = &s
	}
	return resp
}

// RecomputeSquadAttention is the work of the nightly attention job. It returns
// how many squads had decided proposals in the window.
func (h *Handler) RecomputeSquadAttention(ctx context.Context) (int, error) {
	return service.RecomputeSquadAttention(ctx, h.TxStarter, h.Queries, time.Now())
}

// loadSquadForAttention returns the squad named by the URL in the workspace the
// request selects, or writes the refusal.
func (h *Handler) loadSquadForAttention(w http.ResponseWriter, r *http.Request, wsUUID pgtype.UUID) (db.Squad, bool) {
	squadID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "squad id")
	if !ok {
		return db.Squad{}, false
	}
	squad, err := h.Queries.GetSquadInWorkspace(r.Context(), db.GetSquadInWorkspaceParams{ID: squadID, WorkspaceID: wsUUID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "squad not found")
			return db.Squad{}, false
		}
		writeError(w, http.StatusInternalServerError, "failed to load squad")
		return db.Squad{}, false
	}
	return squad, true
}

// GetSquadAttention returns a squad's attention caps, their rate and sample, and
// the reason. Any workspace member may read it.
//
//	GET /api/squads/{id}/attention
func (h *Handler) GetSquadAttention(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	squad, ok := h.loadSquadForAttention(w, r, wsUUID)
	if !ok {
		return
	}
	view, err := service.SquadAttentionFor(r.Context(), h.Queries, wsUUID, squad.ID)
	if err != nil {
		slog.Warn("read squad attention failed", append(logger.RequestAttrs(r), "error", err, "squad_id", uuidToString(squad.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to load squad attention")
		return
	}
	writeJSON(w, http.StatusOK, squadAttentionToResponse(uuidToString(squad.ID), view))
}
