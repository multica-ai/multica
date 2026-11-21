package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The marketing site's footer star, which every visitor blows round together.
// The total is a public, anonymous counter: each request may add only a few
// turns, the route is rate limited per IP, and nothing about the visitor is
// stored.
const (
	siteStarCounter = "footer_star_turns"
	// A visitor spins the star at most about six turns a second and the page
	// reports every couple of seconds; this leaves headroom for a late flush.
	siteStarMaxTurnsPerRequest = 30
	siteStarBodyLimit          = 1 << 10
)

type siteStarResponse struct {
	Turns int64 `json:"turns"`
}

type addSiteStarTurnsRequest struct {
	Turns int64 `json:"turns"`
}

func (h *Handler) GetSiteStar(w http.ResponseWriter, r *http.Request) {
	turns, err := h.Queries.GetSiteCounter(r.Context(), siteStarCounter)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		slog.Warn("get site star failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to read star")
		return
	}
	writeJSON(w, http.StatusOK, siteStarResponse{Turns: turns})
}

func (h *Handler) AddSiteStarTurns(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, siteStarBodyLimit)
	var req addSiteStarTurnsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Turns < 1 || req.Turns > siteStarMaxTurnsPerRequest {
		writeError(w, http.StatusBadRequest, "turns is out of range")
		return
	}
	turns, err := h.Queries.AddSiteCounter(r.Context(), db.AddSiteCounterParams{Name: siteStarCounter, Value: req.Turns})
	if err != nil {
		slog.Warn("add site star turns failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to spin star")
		return
	}
	writeJSON(w, http.StatusOK, siteStarResponse{Turns: turns})
}
