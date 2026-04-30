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

// validNotifGroups is the set of notification preference group keys that the
// API accepts. Keys not in this set are rejected.
var validNotifGroups = map[string]bool{
	"assignments":    true,
	"status_changes": true,
	"comments":       true,
	"updates":        true,
	"agent_activity": true,
}

// validNotifValues is the set of allowed preference values per group.
var validNotifValues = map[string]bool{
	"all":   true,
	"muted": true,
}

func (h *Handler) GetNotificationPreferences(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())

	pref, err := h.Queries.GetNotificationPreference(r.Context(), db.GetNotificationPreferenceParams{
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusOK, map[string]any{
				"workspace_id": workspaceID,
				"preferences":  map[string]any{},
			})
			return
		}
		slog.Warn("GetNotificationPreference failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to get notification preferences")
		return
	}

	var prefs map[string]string
	if err := json.Unmarshal(pref.Preferences, &prefs); err != nil {
		prefs = map[string]string{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"workspace_id": workspaceID,
		"preferences":  prefs,
	})
}

type updateNotifPrefRequest struct {
	Preferences map[string]string `json:"preferences"`
}

func (h *Handler) UpdateNotificationPreferences(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())

	var req updateNotifPrefRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Preferences == nil {
		writeError(w, http.StatusBadRequest, "preferences field is required")
		return
	}

	for k, v := range req.Preferences {
		if !validNotifGroups[k] {
			writeError(w, http.StatusBadRequest, "invalid preference group: "+k)
			return
		}
		if !validNotifValues[v] {
			writeError(w, http.StatusBadRequest, "invalid preference value: "+v)
			return
		}
	}

	prefsJSON, err := json.Marshal(req.Preferences)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to marshal preferences")
		return
	}

	pref, err := h.Queries.UpsertNotificationPreference(r.Context(), db.UpsertNotificationPreferenceParams{
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
		Preferences: prefsJSON,
	})
	if err != nil {
		slog.Warn("UpsertNotificationPreference failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update notification preferences")
		return
	}

	var prefs map[string]string
	if err := json.Unmarshal(pref.Preferences, &prefs); err != nil {
		prefs = map[string]string{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"workspace_id": workspaceID,
		"preferences":  prefs,
	})
}
