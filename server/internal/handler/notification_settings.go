package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type notificationPreferenceSpec struct {
	Channel         string
	EventType       string
	DefaultEnabled  bool
	RequiresBinding bool
}

var supportedNotificationPreferences = []notificationPreferenceSpec{
	{
		Channel:         "notification_trigger",
		EventType:       "mentioned",
		DefaultEnabled:  true,
		RequiresBinding: false,
	},
	{
		Channel:         "notification_trigger",
		EventType:       "issue_assigned",
		DefaultEnabled:  false,
		RequiresBinding: false,
	},
	{
		Channel:         "notification_trigger",
		EventType:       "subscribed_issue_updated",
		DefaultEnabled:  false,
		RequiresBinding: false,
	},
	{
		Channel:         "notification_trigger",
		EventType:       "task_completed",
		DefaultEnabled:  false,
		RequiresBinding: false,
	},
	{
		Channel:         "notification_trigger",
		EventType:       "task_failed",
		DefaultEnabled:  false,
		RequiresBinding: false,
	},
	{
		Channel:         "notification_trigger",
		EventType:       "replied",
		DefaultEnabled:  false,
		RequiresBinding: false,
	},
	{
		Channel:         "inbox",
		EventType:       "channel_enabled",
		DefaultEnabled:  true,
		RequiresBinding: false,
	},
	{
		Channel:         "inbox",
		EventType:       "mentioned",
		DefaultEnabled:  true,
		RequiresBinding: false,
	},
	{
		Channel:         "dingtalk",
		EventType:       "channel_enabled",
		DefaultEnabled:  false,
		RequiresBinding: true,
	},
	{
		Channel:         "dingtalk",
		EventType:       "mentioned",
		DefaultEnabled:  false,
		RequiresBinding: true,
	},
	{
		Channel:         "email",
		EventType:       "channel_enabled",
		DefaultEnabled:  false,
		RequiresBinding: true,
	},
	{
		Channel:         "email",
		EventType:       "mentioned",
		DefaultEnabled:  false,
		RequiresBinding: true,
	},
	{
		Channel:         "custom_webhook",
		EventType:       "channel_enabled",
		DefaultEnabled:  false,
		RequiresBinding: false,
	},
	{
		Channel:         "custom_webhook",
		EventType:       "mentioned",
		DefaultEnabled:  false,
		RequiresBinding: false,
	},
	{
		Channel:         "custom_webhook",
		EventType:       "issue_assigned",
		DefaultEnabled:  false,
		RequiresBinding: false,
	},
	{
		Channel:         "custom_webhook",
		EventType:       "subscribed_issue_updated",
		DefaultEnabled:  false,
		RequiresBinding: false,
	},
	{
		Channel:         "dingtalk",
		EventType:       "task_completed",
		DefaultEnabled:  false,
		RequiresBinding: true,
	},
	{
		Channel:         "dingtalk",
		EventType:       "task_failed",
		DefaultEnabled:  false,
		RequiresBinding: true,
	},
	{
		Channel:         "openclaw_weixin",
		EventType:       "channel_enabled",
		DefaultEnabled:  false,
		RequiresBinding: true,
	},
	{
		Channel:         "openclaw_weixin",
		EventType:       "mentioned",
		DefaultEnabled:  false,
		RequiresBinding: true,
	},
	{
		Channel:         "openclaw_weixin",
		EventType:       "task_completed",
		DefaultEnabled:  false,
		RequiresBinding: true,
	},
	{
		Channel:         "openclaw_weixin",
		EventType:       "task_failed",
		DefaultEnabled:  false,
		RequiresBinding: true,
	},
	{
		Channel:         "openclaw_weixin",
		EventType:       "replied",
		DefaultEnabled:  false,
		RequiresBinding: true,
	},
}

type NotificationBindingResponse struct {
	ID             string          `json:"id"`
	Provider       string          `json:"provider"`
	ExternalUserID string          `json:"external_user_id"`
	DisplayName    *string         `json:"display_name"`
	Status         string          `json:"status"`
	Metadata       json.RawMessage `json:"metadata"`
	CreatedAt      string          `json:"created_at"`
	UpdatedAt      string          `json:"updated_at"`
}

type ListNotificationBindingsResponse struct {
	Bindings []NotificationBindingResponse `json:"bindings"`
}

type NotificationPreferenceResponse struct {
	Channel         string  `json:"channel"`
	EventType       string  `json:"event_type"`
	Enabled         bool    `json:"enabled"`
	BindingID       *string `json:"binding_id"`
	RequiresBinding bool    `json:"requires_binding"`
	RenderMode      string  `json:"render_mode"`
}

type ListNotificationPreferencesResponse struct {
	Preferences []NotificationPreferenceResponse `json:"preferences"`
}

type UpdateNotificationPreferenceRequest struct {
	Channel    string `json:"channel"`
	EventType  string `json:"event_type"`
	Enabled    *bool  `json:"enabled"`
	RenderMode string `json:"render_mode,omitempty"`
}

func normalizeNotificationPreference(channel, eventType string) (string, string) {
	return strings.ToLower(strings.TrimSpace(channel)), strings.ToLower(strings.TrimSpace(eventType))
}

func normalizeRenderMode(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "compact", "detail":
		return mode
	default:
		return "auto"
	}
}

func findNotificationPreferenceSpec(channel, eventType string) (notificationPreferenceSpec, bool) {
	for _, spec := range supportedNotificationPreferences {
		if spec.Channel == channel && spec.EventType == eventType {
			return spec, true
		}
	}
	return notificationPreferenceSpec{}, false
}

func notificationBindingsToResponse(bindings []db.ExternalAccountBinding) []NotificationBindingResponse {
	resp := make([]NotificationBindingResponse, 0, len(bindings))
	for _, binding := range bindings {
		metadata := binding.Metadata
		if len(metadata) == 0 {
			metadata = []byte("{}")
		}
		resp = append(resp, NotificationBindingResponse{
			ID:             uuidToString(binding.ID),
			Provider:       binding.Provider,
			ExternalUserID: binding.ExternalUserID,
			DisplayName:    textToPtr(binding.DisplayName),
			Status:         binding.Status,
			Metadata:       json.RawMessage(metadata),
			CreatedAt:      timestampToString(binding.CreatedAt),
			UpdatedAt:      timestampToString(binding.UpdatedAt),
		})
	}
	return resp
}

func notificationPreferenceToResponse(pref db.NotificationChannelPreference, spec notificationPreferenceSpec) NotificationPreferenceResponse {
	renderMode := pref.RenderMode
	if renderMode == "" {
		renderMode = "auto"
	}
	return NotificationPreferenceResponse{
		Channel:         pref.Channel,
		EventType:       pref.EventType,
		Enabled:         pref.Enabled,
		BindingID:       uuidToPtr(pref.BindingID),
		RequiresBinding: spec.RequiresBinding,
		RenderMode:      renderMode,
	}
}

func mergeNotificationPreferences(prefs []db.NotificationChannelPreference) []NotificationPreferenceResponse {
	byKey := make(map[string]db.NotificationChannelPreference, len(prefs))
	for _, pref := range prefs {
		byKey[pref.Channel+":"+pref.EventType] = pref
	}

	resp := make([]NotificationPreferenceResponse, 0, len(supportedNotificationPreferences))
	for _, spec := range supportedNotificationPreferences {
		if pref, ok := byKey[spec.Channel+":"+spec.EventType]; ok {
			resp = append(resp, notificationPreferenceToResponse(pref, spec))
			continue
		}
		resp = append(resp, NotificationPreferenceResponse{
			Channel:         spec.Channel,
			EventType:       spec.EventType,
			Enabled:         spec.DefaultEnabled,
			BindingID:       nil,
			RequiresBinding: spec.RequiresBinding,
			RenderMode:      "auto",
		})
	}

	return resp
}

func activeBindingForProvider(bindings []db.ExternalAccountBinding, provider string) *db.ExternalAccountBinding {
	for i := range bindings {
		if bindings[i].Provider == provider && bindings[i].Status == "active" {
			return &bindings[i]
		}
	}
	return nil
}

func (h *Handler) GetMyNotificationBindings(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	bindings, err := h.Queries.ListExternalAccountBindingsByUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load notification bindings")
		return
	}

	// Lazy-create email binding for users who have a real email but logged in
	// before the auto-bind code was deployed (or who registered via DingTalk
	// and later verified their email).
	hasEmailBinding := false
	for _, b := range bindings {
		if b.Provider == "email" {
			hasEmailBinding = true
			break
		}
	}
	if !hasEmailBinding {
		user, err := h.Queries.GetUser(r.Context(), parseUUID(userID))
		if err == nil && user.Email != "" && !strings.HasSuffix(user.Email, "@dingtalk.local") {
			binding, err := h.Queries.UpsertExternalAccountBinding(r.Context(), db.UpsertExternalAccountBindingParams{
				UserID:                parseUUID(userID),
				Provider:              "email",
				ExternalUserID:        user.Email,
				DisplayName:           strToText(user.Email),
				AccessTokenEncrypted:  pgtype.Text{},
				RefreshTokenEncrypted: pgtype.Text{},
				TokenExpiresAt:        pgtype.Timestamptz{},
				Status:                "active",
				Metadata:              []byte("{}"),
			})
			if err != nil {
				slog.Warn("failed to lazy-create email binding", "user_id", userID, "error", err)
			} else {
				bindings = append(bindings, binding)
			}
		}
	}

	writeJSON(w, http.StatusOK, ListNotificationBindingsResponse{
		Bindings: notificationBindingsToResponse(bindings),
	})
}

func (h *Handler) GetMyNotificationPreferences(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	prefs, err := h.Queries.ListNotificationChannelPreferencesByUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load notification preferences")
		return
	}

	writeJSON(w, http.StatusOK, ListNotificationPreferencesResponse{
		Preferences: mergeNotificationPreferences(prefs),
	})
}

func (h *Handler) UpdateMyNotificationPreference(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req UpdateNotificationPreferenceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	channel, eventType := normalizeNotificationPreference(req.Channel, req.EventType)
	spec, supported := findNotificationPreferenceSpec(channel, eventType)
	if !supported {
		writeError(w, http.StatusBadRequest, "unsupported notification preference")
		return
	}

	// Load existing preference (if any) to support partial updates.
	existingPrefs, err := h.Queries.ListNotificationChannelPreferencesByUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load notification preferences")
		return
	}
	var existing *db.NotificationChannelPreference
	for i := range existingPrefs {
		if existingPrefs[i].Channel == channel && existingPrefs[i].EventType == eventType {
			existing = &existingPrefs[i]
			break
		}
	}

	// Resolve enabled: use request value > existing value > spec default.
	enabled := spec.DefaultEnabled
	if existing != nil {
		enabled = existing.Enabled
	}
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	// Resolve render_mode: use request value > existing value > "auto".
	renderMode := "auto"
	if existing != nil && existing.RenderMode != "" {
		renderMode = existing.RenderMode
	}
	if req.RenderMode != "" {
		renderMode = normalizeRenderMode(req.RenderMode)
	}

	// Resolve binding_id: preserve existing if not changing enabled state.
	bindingID := pgtype.UUID{}
	if existing != nil {
		bindingID = existing.BindingID
	}
	if spec.RequiresBinding && enabled {
		bindings, err := h.Queries.ListExternalAccountBindingsByUser(r.Context(), parseUUID(userID))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load notification bindings")
			return
		}
		binding := activeBindingForProvider(bindings, channel)
		if binding == nil {
			writeError(w, http.StatusBadRequest, channel+" account is not connected")
			return
		}
		bindingID = binding.ID
	}
	if channel == "custom_webhook" && enabled {
		endpoints, err := h.Queries.ListEnabledNotificationWebhookEndpointsByUser(r.Context(), parseUUID(userID))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load notification webhooks")
			return
		}
		if len(endpoints) == 0 {
			writeError(w, http.StatusBadRequest, "custom webhook is not configured")
			return
		}
	}

	pref, err := h.Queries.UpsertNotificationChannelPreference(r.Context(), db.UpsertNotificationChannelPreferenceParams{
		UserID:     parseUUID(userID),
		Channel:    channel,
		EventType:  eventType,
		Enabled:    enabled,
		BindingID:  bindingID,
		RenderMode: renderMode,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update notification preference")
		return
	}

	writeJSON(w, http.StatusOK, notificationPreferenceToResponse(pref, spec))
}

func (h *Handler) DeleteMyNotificationBinding(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	bindingID := strings.TrimSpace(chi.URLParam(r, "bindingId"))
	if bindingID == "" {
		writeError(w, http.StatusBadRequest, "binding id is required")
		return
	}

	bindings, err := h.Queries.ListExternalAccountBindingsByUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load notification bindings")
		return
	}

	var binding *db.ExternalAccountBinding
	for i := range bindings {
		if uuidToString(bindings[i].ID) == bindingID {
			binding = &bindings[i]
			break
		}
	}
	if binding == nil {
		writeError(w, http.StatusNotFound, "notification binding not found")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())

	if _, err := tx.Exec(r.Context(), `
		UPDATE notification_channel_preference
		SET enabled = false, binding_id = NULL, updated_at = now()
		WHERE user_id = $1 AND channel = $2
	`, parseUUID(userID), binding.Provider); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to disable notification preferences")
		return
	}

	if _, err := tx.Exec(r.Context(), `
		DELETE FROM external_account_binding
		WHERE id = $1 AND user_id = $2
	`, parseUUID(bindingID), parseUUID(userID)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete notification binding")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete notification binding")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
