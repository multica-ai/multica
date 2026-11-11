package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"

	"github.com/jackc/pgx/v5"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Bounded digit runs keep the advance query's int[] casts inside int32.
var whatsNewVersionPattern = regexp.MustCompile(`^\d{1,6}\.\d{1,6}\.\d{1,6}$`)

type markWhatsNewSeenRequest struct {
	Version string `json:"version"`
}

// MarkWhatsNewSeen records the newest release whose highlights the user has
// seen. The stored version only moves forward, so the response always carries
// the effective value even when the request was stale.
func (h *Handler) MarkWhatsNewSeen(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req markWhatsNewSeenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !whatsNewVersionPattern.MatchString(req.Version) {
		writeError(w, http.StatusBadRequest, "version must be MAJOR.MINOR.PATCH")
		return
	}

	user, err := h.Queries.AdvanceWhatsNewSeenVersion(r.Context(), db.AdvanceWhatsNewSeenVersionParams{
		ID:      parseUUID(userID),
		Version: req.Version,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		user, err = h.Queries.GetUser(r.Context(), parseUUID(userID))
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusUnauthorized, "user not found")
			return
		}
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update what's new state")
		return
	}

	writeJSON(w, http.StatusOK, h.userToResponse(user))
}
