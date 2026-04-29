package handler

import (
	"encoding/json"
	"net/http"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// validNotificationTypes is the set of notification types that users can configure.
var validNotificationTypes = map[string]bool{
	"issue_assigned":    true,
	"unassigned":        true,
	"assignee_changed":  true,
	"status_changed":    true,
	"priority_changed":  true,
	"due_date_changed":  true,
	"new_comment":       true,
	"mentioned":         true,
	"reaction_added":    true,
	"task_completed":    true,
	"task_failed":       true,
	"agent_blocked":     true,
	"agent_completed":   true,
	"quick_create_done": true,
	"quick_create_failed": true,
}

// DefaultDisabledTypes are notification types that are OFF by default (no row = disabled).
// All other types default to ON (no row = enabled).
// Exported so notification_listeners can reference the same defaults.
var DefaultDisabledTypes = map[string]bool{
	"status_changed": true,
}

type NotificationPreferenceResponse struct {
	NotificationType string `json:"notification_type"`
	Enabled          bool   `json:"enabled"`
}

func (h *Handler) ListNotificationPreferences(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	prefs, err := h.Queries.ListNotificationPreferences(r.Context(), db.ListNotificationPreferencesParams{
		WorkspaceID: wsUUID,
		UserID:      parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list notification preferences")
		return
	}

	// Build a map of explicitly set preferences
	explicit := make(map[string]bool, len(prefs))
	for _, p := range prefs {
		explicit[p.NotificationType] = p.Enabled
	}

	// Build full response with defaults for all valid types
	resp := make([]NotificationPreferenceResponse, 0, len(validNotificationTypes))
	for t := range validNotificationTypes {
		enabled := !DefaultDisabledTypes[t] // default: ON unless in defaultDisabledTypes
		if v, ok := explicit[t]; ok {
			enabled = v
		}
		resp = append(resp, NotificationPreferenceResponse{
			NotificationType: t,
			Enabled:          enabled,
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

type updateNotificationPreferencesRequest struct {
	Preferences []NotificationPreferenceResponse `json:"preferences"`
}

func (h *Handler) UpdateNotificationPreferences(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	var req updateNotificationPreferencesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(req.Preferences) == 0 {
		writeError(w, http.StatusBadRequest, "preferences cannot be empty")
		return
	}

	// Validate all notification types
	for _, p := range req.Preferences {
		if !validNotificationTypes[p.NotificationType] {
			writeError(w, http.StatusBadRequest, "invalid notification type: "+p.NotificationType)
			return
		}
	}

	// Upsert each preference
	for _, p := range req.Preferences {
		_, err := h.Queries.UpsertNotificationPreference(r.Context(), db.UpsertNotificationPreferenceParams{
			WorkspaceID:      wsUUID,
			UserID:           parseUUID(userID),
			NotificationType: p.NotificationType,
			Enabled:          p.Enabled,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update notification preference")
			return
		}
	}

	// Return updated preferences
	h.ListNotificationPreferences(w, r)
}
