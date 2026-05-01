package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Fork-specific: per-user preferences endpoint.
// Kept in a dedicated file so upstream-merges don't conflict on auth.go.

// UpdateMyPreferences merges the request body into the user's preferences blob.
// Partial updates only — fields not present in the body retain their existing
// values, so callers don't need to round-trip the full preferences object.
func (h *Handler) UpdateMyPreferences(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var patch map[string]any
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if patch == nil {
		patch = map[string]any{}
	}

	// Load existing prefs and merge.
	existingRaw, err := h.Queries.GetUserPreferences(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	existing := map[string]any{}
	if len(existingRaw) > 0 {
		_ = json.Unmarshal(existingRaw, &existing)
	}
	for k, v := range patch {
		existing[k] = v
	}

	merged, err := json.Marshal(existing)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode preferences")
		return
	}

	user, err := h.Queries.UpdateUserPreferences(r.Context(), db.UpdateUserPreferencesParams{
		UserID:      parseUUID(userID),
		Preferences: merged,
	})
	if err != nil {
		slog.Error("update user preferences failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update preferences")
		return
	}

	writeJSON(w, http.StatusOK, userToResponse(user))
}
