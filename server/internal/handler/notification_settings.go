package handler

import (
	"encoding/json"
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
		Channel:         "inbox",
		EventType:       "mentioned",
		DefaultEnabled:  true,
		RequiresBinding: false,
	},
	{
		Channel:         "dingtalk",
		EventType:       "mentioned",
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
}

type ListNotificationPreferencesResponse struct {
	Preferences []NotificationPreferenceResponse `json:"preferences"`
}

type UpdateNotificationPreferenceRequest struct {
	Channel   string `json:"channel"`
	EventType string `json:"event_type"`
	Enabled   *bool  `json:"enabled"`
}

func normalizeNotificationPreference(channel, eventType string) (string, string) {
	return strings.ToLower(strings.TrimSpace(channel)), strings.ToLower(strings.TrimSpace(eventType))
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
	return NotificationPreferenceResponse{
		Channel:         pref.Channel,
		EventType:       pref.EventType,
		Enabled:         pref.Enabled,
		BindingID:       uuidToPtr(pref.BindingID),
		RequiresBinding: spec.RequiresBinding,
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
	if req.Enabled == nil {
		writeError(w, http.StatusBadRequest, "enabled is required")
		return
	}

	bindingID := pgtype.UUID{}
	if spec.RequiresBinding && *req.Enabled {
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

	pref, err := h.Queries.UpsertNotificationChannelPreference(r.Context(), db.UpsertNotificationChannelPreferenceParams{
		UserID:    parseUUID(userID),
		Channel:   channel,
		EventType: eventType,
		Enabled:   *req.Enabled,
		BindingID: bindingID,
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
