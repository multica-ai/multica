package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/inbox"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// validNotifGroups is the set of notification preference group keys that the
// API accepts. Keys not in this set are rejected. `system_notifications` is
// not an inbox event group — it's a delivery-channel toggle controlling
// whether native OS notification banners fire — but it shares the same
// preferences map so a single endpoint covers all user notification
// preferences. The `auto_archive_*` keys are not event groups either: each
// says whether one action you take on an issue archives your notifications on
// it. Completing, commenting, other status changes and reassigning archive
// unless "muted"; priority, project, date and label edits only with "all".
// `after_auto_archive` is where the client goes when the notification you are
// reading is archived that way.
var validNotifGroups = map[string]bool{
	"assignments":              true,
	"status_changes":           true,
	"comments":                 true,
	"chat_replies":             true,
	"mentions":                 true,
	"reactions":                true,
	"updates":                  true,
	"agent_activity":           true,
	"new_issues":               true,
	"system_notifications":     true,
	"auto_archive_completed":   true,
	"auto_archive_on_comment":  true,
	"auto_archive_on_status":   true,
	"auto_archive_on_assignee": true,
	"auto_archive_on_priority": true,
	"auto_archive_on_project":  true,
	"auto_archive_on_dates":    true,
	"auto_archive_on_labels":   true,
	"after_auto_archive":       true,
	"others_issues":            true,
}

// validNotifValues is the set of allowed preference values for a group not
// listed in notifValuesByGroup.
var validNotifValues = map[string]bool{
	"all":   true,
	"muted": true,
}

// notifValuesByGroup lists the groups whose values are not on/off.
// `others_issues` is how much of someone else's issue a passive subscriber
// receives: absent or "key_updates" for mentions, thread replies and handoffs,
// "all" for every event. Muting it would only hide what still needs you.
// `after_auto_archive` is absent or "stay" to keep the archived notification
// open, "next" to open the next one.
var notifValuesByGroup = map[string]map[string]bool{
	"others_issues":      {"all": true, "key_updates": true},
	"after_auto_archive": {"stay": true, "next": true},
}

// autoArchivePromptKey marks a workspace where the member has not yet answered
// the one-time question of which actions archive their notifications. Migration
// 718 writes it for members who archived notifications before replying,
// changing status and reassigning did so by default. It is stored with the
// preferences but answered as the response's own `auto_archive_prompt` flag,
// so no client ever echoes it back in a write.
const autoArchivePromptKey = "auto_archive_prompt"

func validNotifValue(group, value string) bool {
	if values, ok := notifValuesByGroup[group]; ok {
		return values[value]
	}
	return validNotifValues[value]
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
				"workspace_id":        workspaceID,
				"preferences":         inbox.WithDefaults(nil),
				"auto_archive_prompt": false,
			})
			return
		}
		slog.Warn("GetNotificationPreference failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to get notification preferences")
		return
	}
	writeNotificationPreferenceResponse(w, workspaceID, pref)
}

type updateNotifPrefRequest struct {
	Preferences map[string]string `json:"preferences"`
}

// decodeNotificationPreferenceRequest reads and validates the preferences in
// a write request.
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
		if !validNotifGroups[k] {
			writeError(w, http.StatusBadRequest, "invalid preference group: "+k)
			return nil, false
		}
		if !validNotifValue(k, v) {
			writeError(w, http.StatusBadRequest, "invalid preference value: "+v)
			return nil, false
		}
	}

	return req.Preferences, true
}

func marshalNotificationPreferences(w http.ResponseWriter, prefs map[string]string) ([]byte, bool) {
	prefsJSON, err := json.Marshal(prefs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to marshal preferences")
		return nil, false
	}
	return prefsJSON, true
}

// writeNotificationPreferenceResponse answers with the stored preferences plus
// the groups muted by default (inbox.WithDefaults), so every client renders
// those switches as off whichever defaults it was built with.
func writeNotificationPreferenceResponse(
	w http.ResponseWriter,
	workspaceID string,
	pref db.NotificationPreference,
) {
	var prefs map[string]string
	if err := json.Unmarshal(pref.Preferences, &prefs); err != nil {
		prefs = map[string]string{}
	}
	_, prompt := prefs[autoArchivePromptKey]
	delete(prefs, autoArchivePromptKey)

	writeJSON(w, http.StatusOK, map[string]any{
		"workspace_id":        workspaceID,
		"preferences":         inbox.WithDefaults(prefs),
		"auto_archive_prompt": prompt,
	})
}

// UpdateNotificationPreferences preserves the original replace-all PUT
// contract for compatibility with installed clients.
func (h *Handler) UpdateNotificationPreferences(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	prefs, ok := decodeNotificationPreferenceRequest(w, r)
	if !ok {
		return
	}
	// Only clients built before PATCH existed send this full replacement, and
	// they leave a group out to mean "on".
	prefsJSON, ok := marshalNotificationPreferences(w, inbox.WithLegacyDefaults(prefs))
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
	prefs, ok := decodeNotificationPreferenceRequest(w, r)
	if !ok {
		return
	}
	prefsJSON, ok := marshalNotificationPreferences(w, prefs)
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

// ResolveAutoArchivePrompt answers the one-time auto-archive question once for
// all of the caller's workspaces: the supplied `auto_archive_*` values are
// merged into every one of their preference rows still marked with
// auto_archive_prompt, and the marker is dropped. An empty map keeps what each
// workspace has. Answers with the current workspace's preferences.
func (h *Handler) ResolveAutoArchivePrompt(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	prefs, ok := decodeNotificationPreferenceRequest(w, r)
	if !ok {
		return
	}
	for k := range prefs {
		if !strings.HasPrefix(k, "auto_archive_") {
			writeError(w, http.StatusBadRequest, "invalid preference group: "+k)
			return
		}
	}
	prefsJSON, ok := marshalNotificationPreferences(w, prefs)
	if !ok {
		return
	}

	if err := h.Queries.ResolveAutoArchivePrompt(r.Context(), db.ResolveAutoArchivePromptParams{
		UserID:      parseUUID(userID),
		Preferences: prefsJSON,
	}); err != nil {
		slog.Warn("ResolveAutoArchivePrompt failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update notification preferences")
		return
	}
	h.GetNotificationPreferences(w, r)
}
