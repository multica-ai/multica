package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Frequency rules for the "Star us on GitHub" prompt. The client mirrors them
// to avoid pointless claims; these are the authority.
const (
	starPromptMaxShowings   = 2
	starPromptCooldown      = 30 * 24 * time.Hour
	starPromptMinAccountAge = 3 * 24 * time.Hour
)

// ClaimStarPromptShowing grants the caller one showing of the star prompt.
// The client asks before showing, so 409 means "do not show" and covers a
// second tab or device that reacted to the same moment.
func (h *Handler) ClaimStarPromptShowing(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	if h.cfg.DisableStarPrompt {
		writeError(w, http.StatusConflict, "star prompt is disabled")
		return
	}
	now := time.Now()
	user, err := h.Queries.ClaimStarPromptShowing(r.Context(), db.ClaimStarPromptShowingParams{
		ID:            parseUUID(userID),
		MaxShowings:   starPromptMaxShowings,
		CreatedBefore: pgtype.Timestamptz{Time: now.Add(-starPromptMinAccountAge), Valid: true},
		ShownBefore:   pgtype.Timestamptz{Time: now.Add(-starPromptCooldown), Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "star prompt is not due")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to claim star prompt")
		return
	}
	writeJSON(w, http.StatusOK, h.userToResponse(user))
}

// MarkStarPromptStarred records that the user clicked through to GitHub, which
// retires the prompt for good. GitHub cannot confirm the star, so the click
// stands in for it.
func (h *Handler) MarkStarPromptStarred(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	user, err := h.Queries.MarkStarPromptStarred(r.Context(), parseUUID(userID))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusUnauthorized, "user not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update star prompt")
		return
	}
	writeJSON(w, http.StatusOK, h.userToResponse(user))
}
