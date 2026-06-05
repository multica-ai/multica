package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type mobilePushTokenRequest struct {
	Provider    string  `json:"provider"`
	Token       string  `json:"token"`
	DeviceID    *string `json:"device_id"`
	Platform    string  `json:"platform"`
	AppVersion  *string `json:"app_version"`
	Environment string  `json:"environment"`
}

type mobilePushTokenResponse struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Provider    string `json:"provider"`
	Platform    string `json:"platform"`
	Environment string `json:"environment"`
	Enabled     bool   `json:"enabled"`
}

func mobilePushTokenToResponse(t db.MobilePushDeviceToken) mobilePushTokenResponse {
	return mobilePushTokenResponse{
		ID:          uuidToString(t.ID),
		WorkspaceID: uuidToString(t.WorkspaceID),
		Provider:    t.Provider,
		Platform:    t.Platform,
		Environment: t.Environment,
		Enabled:     t.Enabled,
	}
}

func (h *Handler) RegisterMobilePushToken(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	var req mobilePushTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	provider := strings.TrimSpace(req.Provider)
	if provider == "" {
		provider = "expo"
	}
	if provider != "expo" {
		writeError(w, http.StatusBadRequest, "unsupported push provider")
		return
	}

	token := strings.TrimSpace(req.Token)
	if !strings.HasPrefix(token, "ExponentPushToken[") && !strings.HasPrefix(token, "ExpoPushToken[") {
		writeError(w, http.StatusBadRequest, "invalid expo push token")
		return
	}

	platform := strings.TrimSpace(req.Platform)
	if platform == "" {
		_, _, clientOS := middleware.ClientMetadataFromContext(r.Context())
		platform = clientOS
	}
	if platform == "" {
		platform = "ios"
	}

	environment := strings.TrimSpace(req.Environment)
	if environment == "" {
		environment = "development"
	}

	appVersion := req.AppVersion
	if appVersion == nil {
		_, version, _ := middleware.ClientMetadataFromContext(r.Context())
		if version != "" {
			appVersion = &version
		}
	}

	item, err := h.Queries.UpsertMobilePushDeviceToken(r.Context(), db.UpsertMobilePushDeviceTokenParams{
		UserID:      parseUUID(userID),
		WorkspaceID: wsUUID,
		Provider:    provider,
		Token:       token,
		DeviceID:    ptrToText(trimStringPtr(req.DeviceID)),
		Platform:    platform,
		AppVersion:  ptrToText(trimStringPtr(appVersion)),
		Environment: environment,
	})
	if err != nil {
		slog.Warn("UpsertMobilePushDeviceToken failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to register push token")
		return
	}

	writeJSON(w, http.StatusOK, mobilePushTokenToResponse(item))
}

func (h *Handler) UnregisterMobilePushToken(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	var req mobilePushTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	provider := strings.TrimSpace(req.Provider)
	if provider == "" {
		provider = "expo"
	}
	token := strings.TrimSpace(req.Token)
	if provider != "expo" || token == "" {
		writeError(w, http.StatusBadRequest, "provider and token are required")
		return
	}

	item, err := h.Queries.DisableMobilePushDeviceToken(r.Context(), db.DisableMobilePushDeviceTokenParams{
		UserID:      parseUUID(userID),
		WorkspaceID: wsUUID,
		Provider:    provider,
		Token:       token,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
			return
		}
		slog.Warn("DisableMobilePushDeviceToken failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to unregister push token")
		return
	}

	writeJSON(w, http.StatusOK, mobilePushTokenToResponse(item))
}

func trimStringPtr(s *string) *string {
	if s == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*s)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
