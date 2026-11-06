package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/integrations/googlechat"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// validNotifGroups is the set of notification preference group keys that the
// API accepts. Keys not in this set are rejected. `system_notifications` is
// not an inbox event group — it's a delivery-channel toggle controlling
// whether native OS notification banners fire — but it shares the same
// preferences map so a single endpoint covers all user notification
// preferences.
var validNotifGroups = map[string]bool{
	"assignments":          true,
	"status_changes":       true,
	"comments":             true,
	"mentions":             true,
	"updates":              true,
	"agent_activity":       true,
	"system_notifications": true,
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

	prefs, err := h.loadNotificationPreferences(r, workspaceID, userID)
	if err != nil {
		slog.Warn("GetNotificationPreference failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to get notification preferences")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"workspace_id": workspaceID,
		"preferences":  maskedNotificationPreferences(prefs),
	})
}

type updateNotifPrefRequest struct {
	Preferences map[string]string `json:"preferences"`
}

func decodeNotificationPreferenceRequest(w http.ResponseWriter, r *http.Request) (map[string]string, bool) {
	var req updateNotifPrefRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return nil, false
	}

	if req.Preferences == nil {
		writeError(w, http.StatusBadRequest, "preferences field is required")
		return nil, false
	}

	for k, v := range req.Preferences {
		if k == "google_chat_webhook" {
			continue
		}
		if !validNotifGroups[k] {
			writeError(w, http.StatusBadRequest, "invalid preference group: "+k)
			return nil, false
		}
		if !validNotifValues[v] {
			writeError(w, http.StatusBadRequest, "invalid preference value: "+v)
			return nil, false
		}
	}

	return req.Preferences, true
}

func writeNotificationPreferenceResponse(
	w http.ResponseWriter,
	workspaceID string,
	pref db.NotificationPreference,
) {
	var prefs map[string]string
	if err := json.Unmarshal(pref.Preferences, &prefs); err != nil {
		prefs = map[string]string{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"workspace_id": workspaceID,
		"preferences":  maskedNotificationPreferences(prefs),
	})
}

func (h *Handler) loadNotificationPreferences(
	r *http.Request,
	workspaceID string,
	userID string,
) (map[string]string, error) {
	pref, err := h.Queries.GetNotificationPreference(r.Context(), db.GetNotificationPreferenceParams{
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var prefs map[string]string
	if err := json.Unmarshal(pref.Preferences, &prefs); err != nil {
		return map[string]string{}, nil
	}
	return prefs, nil
}

func maskedNotificationPreferences(prefs map[string]string) map[string]string {
	masked := make(map[string]string, len(prefs))
	for key, value := range prefs {
		if key == "google_chat_webhook" && value != "" {
			value = googlechat.MaskWebhookURL(value)
		}
		masked[key] = value
	}
	return masked
}

func (h *Handler) prepareNotificationPreferenceUpdate(
	w http.ResponseWriter,
	r *http.Request,
	workspaceID string,
	userID string,
	preserveWebhookOnOmission bool,
) ([]byte, bool) {
	current, err := h.loadNotificationPreferences(r, workspaceID, userID)
	if err != nil {
		slog.Warn("GetNotificationPreference before update failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load notification preferences")
		return nil, false
	}

	next, ok := decodeNotificationPreferenceRequest(w, r)
	if !ok {
		return nil, false
	}

	incomingWebhook, webhookIncluded := next["google_chat_webhook"]
	currentWebhook := current["google_chat_webhook"]
	if preserveWebhookOnOmission && !webhookIncluded && currentWebhook != "" {
		next["google_chat_webhook"] = currentWebhook
	}
	if webhookIncluded && incomingWebhook != "" {
		if currentWebhook != "" && incomingWebhook == googlechat.MaskWebhookURL(currentWebhook) {
			next["google_chat_webhook"] = currentWebhook
			incomingWebhook = currentWebhook
		} else if _, err := googlechat.ValidateWebhookURL(incomingWebhook); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return nil, false
		}
	}

	if webhookIncluded && incomingWebhook != "" && incomingWebhook != currentWebhook {
		workspace, err := h.Queries.GetWorkspace(r.Context(), parseUUID(workspaceID))
		if err != nil {
			slog.Warn("GetWorkspace for Google Chat confirmation failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to load workspace")
			return nil, false
		}
		if h.GoogleChatSender == nil {
			writeError(w, http.StatusBadRequest, "Google Chat delivery is unavailable")
			return nil, false
		}
		if err := h.GoogleChatSender.Send(
			r.Context(),
			incomingWebhook,
			googlechat.FormatConfirmation(workspace.Name),
		); err != nil {
			writeError(w, http.StatusBadRequest, "failed to connect Google Chat webhook; verify the URL and try again")
			return nil, false
		}
	}

	prefsJSON, err := json.Marshal(next)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to marshal preferences")
		return nil, false
	}
	return prefsJSON, true
}

// UpdateNotificationPreferences preserves the original replace-all PUT
// contract for compatibility with installed clients.
func (h *Handler) UpdateNotificationPreferences(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	prefsJSON, ok := h.prepareNotificationPreferenceUpdate(w, r, workspaceID, userID, true)
	if !ok {
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
	writeNotificationPreferenceResponse(w, workspaceID, pref)
}

// PatchNotificationPreferences atomically merges only the supplied keys. This
// prevents stale tabs or devices from replacing unrelated mute settings.
func (h *Handler) PatchNotificationPreferences(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	prefsJSON, ok := h.prepareNotificationPreferenceUpdate(w, r, workspaceID, userID, false)
	if !ok {
		return
	}

	pref, err := h.Queries.PatchNotificationPreference(r.Context(), db.PatchNotificationPreferenceParams{
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
		Preferences: prefsJSON,
	})
	if err != nil {
		slog.Warn("PatchNotificationPreference failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update notification preferences")
		return
	}
	writeNotificationPreferenceResponse(w, workspaceID, pref)
}
