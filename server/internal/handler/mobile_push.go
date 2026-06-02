package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type UpsertMobilePushRegistrationRequest struct {
	InstallationID   string  `json:"installation_id"`
	Platform         string  `json:"platform"`
	Provider         string  `json:"provider"`
	ProviderClientID string  `json:"provider_client_id"`
	AppVersion       *string `json:"app_version"`
}

type MobilePushRegistrationResponse struct {
	ID               string  `json:"id"`
	UserID           string  `json:"user_id"`
	InstallationID   string  `json:"installation_id"`
	Platform         string  `json:"platform"`
	Provider         string  `json:"provider"`
	ProviderClientID string  `json:"provider_client_id"`
	AppVersion       *string `json:"app_version"`
	Enabled          bool    `json:"enabled"`
	LastSeenAt       string  `json:"last_seen_at"`
	CreatedAt        string  `json:"created_at"`
	UpdatedAt        string  `json:"updated_at"`
}

func mobilePushRegistrationToResponse(reg db.MobilePushRegistration) MobilePushRegistrationResponse {
	return MobilePushRegistrationResponse{
		ID:               uuidToString(reg.ID),
		UserID:           uuidToString(reg.UserID),
		InstallationID:   reg.InstallationID,
		Platform:         reg.Platform,
		Provider:         reg.Provider,
		ProviderClientID: reg.ProviderClientID,
		AppVersion:       textToPtr(reg.AppVersion),
		Enabled:          reg.Enabled,
		LastSeenAt:       timestampToString(reg.LastSeenAt),
		CreatedAt:        timestampToString(reg.CreatedAt),
		UpdatedAt:        timestampToString(reg.UpdatedAt),
	}
}

func normalizeMobilePushProvider(provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return "getui"
	}
	return provider
}

func normalizeMobilePushPlatform(platform string) string {
	return strings.ToLower(strings.TrimSpace(platform))
}

func validateMobilePushRegistrationRequest(req UpsertMobilePushRegistrationRequest) (UpsertMobilePushRegistrationRequest, string) {
	req.InstallationID = strings.TrimSpace(req.InstallationID)
	req.Platform = normalizeMobilePushPlatform(req.Platform)
	req.Provider = normalizeMobilePushProvider(req.Provider)
	req.ProviderClientID = strings.TrimSpace(req.ProviderClientID)
	if req.AppVersion != nil {
		appVersion := strings.TrimSpace(*req.AppVersion)
		if appVersion == "" {
			req.AppVersion = nil
		} else {
			req.AppVersion = &appVersion
		}
	}

	if req.InstallationID == "" {
		return req, "installation_id is required"
	}
	if len(req.InstallationID) > 128 {
		return req, "installation_id is too long"
	}
	if req.Platform != "android" && req.Platform != "ios" {
		return req, "platform must be android or ios"
	}
	if req.Provider != "getui" {
		return req, "unsupported mobile push provider"
	}
	if req.ProviderClientID == "" {
		return req, "provider_client_id is required"
	}
	if len(req.ProviderClientID) > 256 {
		return req, "provider_client_id is too long"
	}
	if req.AppVersion != nil && len(*req.AppVersion) > 64 {
		return req, "app_version is too long"
	}

	return req, ""
}

func (h *Handler) UpsertMyMobilePushRegistration(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req UpsertMobilePushRegistrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req, validationErr := validateMobilePushRegistrationRequest(req)
	if validationErr != "" {
		writeError(w, http.StatusBadRequest, validationErr)
		return
	}

	reg, err := h.Queries.UpsertMobilePushRegistration(r.Context(), db.UpsertMobilePushRegistrationParams{
		UserID:           parseUUID(userID),
		InstallationID:   req.InstallationID,
		Platform:         req.Platform,
		Provider:         req.Provider,
		ProviderClientID: req.ProviderClientID,
		AppVersion:       ptrToText(req.AppVersion),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to upsert mobile push registration")
		return
	}

	writeJSON(w, http.StatusOK, mobilePushRegistrationToResponse(reg))
}

func (h *Handler) DisableMyMobilePushRegistration(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	installationID := strings.TrimSpace(chi.URLParam(r, "installationId"))
	if installationID == "" {
		writeError(w, http.StatusBadRequest, "installation_id is required")
		return
	}
	if len(installationID) > 128 {
		writeError(w, http.StatusBadRequest, "installation_id is too long")
		return
	}

	provider := normalizeMobilePushProvider(r.URL.Query().Get("provider"))
	if provider != "getui" {
		writeError(w, http.StatusBadRequest, "unsupported mobile push provider")
		return
	}

	if err := h.Queries.DisableMobilePushRegistration(r.Context(), db.DisableMobilePushRegistrationParams{
		UserID:         parseUUID(userID),
		InstallationID: installationID,
		Provider:       provider,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to disable mobile push registration")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
