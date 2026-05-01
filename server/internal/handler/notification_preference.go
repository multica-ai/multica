package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/ntfy"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// NotificationPreferenceResponse is the API representation of a user's notification preferences.
type NotificationPreferenceResponse struct {
	NtfyURL       string   `json:"ntfy_url"`
	NtfyToken     string   `json:"ntfy_token"`
	DisabledTypes []string `json:"disabled_types"`
}

func prefToResponse(p db.NotificationPreference) NotificationPreferenceResponse {
	token := ""
	if p.NtfyToken.Valid {
		token = p.NtfyToken.String
	}
	url := ""
	if p.NtfyUrl.Valid {
		url = p.NtfyUrl.String
	}
	types := p.DisabledTypes
	if types == nil {
		types = []string{}
	}
	return NotificationPreferenceResponse{
		NtfyURL:       url,
		NtfyToken:     token,
		DisabledTypes: types,
	}
}

// GetNotificationPreference returns the current user's notification preferences.
// Returns an empty default if no preference has been saved yet.
func (h *Handler) GetNotificationPreference(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	pref, err := h.Queries.GetNotificationPreference(r.Context(), parseUUID(userID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusOK, NotificationPreferenceResponse{
				NtfyURL:       "",
				NtfyToken:     "",
				DisabledTypes: []string{},
			})
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get notification preferences")
		return
	}

	writeJSON(w, http.StatusOK, prefToResponse(pref))
}

type upsertNotificationPreferenceRequest struct {
	NtfyURL       string   `json:"ntfy_url"`
	NtfyToken     string   `json:"ntfy_token"`
	DisabledTypes []string `json:"disabled_types"`
}

// UpsertNotificationPreference saves (creates or updates) the current user's notification preferences.
func (h *Handler) UpsertNotificationPreference(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req upsertNotificationPreferenceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.DisabledTypes == nil {
		req.DisabledTypes = []string{}
	}

	pref, err := h.Queries.UpsertNotificationPreference(r.Context(), db.UpsertNotificationPreferenceParams{
		UserID:        parseUUID(userID),
		NtfyUrl:       strToText(req.NtfyURL),
		NtfyToken:     strToText(req.NtfyToken),
		DisabledTypes: req.DisabledTypes,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save notification preferences")
		return
	}

	writeJSON(w, http.StatusOK, prefToResponse(pref))
}

type testNotificationPreferenceRequest struct {
	NtfyURL   string `json:"ntfy_url"`
	NtfyToken string `json:"ntfy_token"`
}

// TestNotificationPreference sends a test push to the ntfy URL supplied in the request body.
// This does not require the preference to be saved first, enabling "test before save" UX.
func (h *Handler) TestNotificationPreference(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}

	var req testNotificationPreferenceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.NtfyURL == "" {
		writeError(w, http.StatusBadRequest, "ntfy_url is required")
		return
	}

	sender := ntfy.New()
	if err := sender.Send(context.Background(), req.NtfyURL, req.NtfyToken, ntfy.Message{
		Title:    "Multica test notification",
		Body:     "Your ntfy integration is working correctly.",
		Severity: "info",
	}); err != nil {
		writeError(w, http.StatusBadGateway, "failed to send test notification: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "sent"})
}
