package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	notifyutil "github.com/multica-ai/multica/server/internal/notify"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type NotificationWebhookResponse struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	MaskedURL       string  `json:"masked_url"`
	Enabled         bool    `json:"enabled"`
	WorkspaceID     *string `json:"workspace_id"`
	PayloadTemplate string  `json:"payload_template"`
	ContentPrefix   string  `json:"content_prefix"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

type ListNotificationWebhooksResponse struct {
	Webhooks []NotificationWebhookResponse `json:"webhooks"`
}

type CreateNotificationWebhookRequest struct {
	Name            string `json:"name"`
	URL             string `json:"url"`
	PayloadTemplate string `json:"payload_template"`
	ContentPrefix   string `json:"content_prefix"`
	Enabled         *bool  `json:"enabled"`
	Workspace       string `json:"workspace_id"`
}

type UpdateNotificationWebhookRequest struct {
	Name            string  `json:"name"`
	URL             string  `json:"url"`
	PayloadTemplate *string `json:"payload_template"`
	ContentPrefix   *string `json:"content_prefix"`
	Enabled         *bool   `json:"enabled"`
}

type TestNotificationWebhookResponse struct {
	Message string `json:"message"`
}

func notificationWebhookToResponse(endpoint db.NotificationWebhookEndpoint) NotificationWebhookResponse {
	rawURL, _ := notifyutil.DecryptToken(endpoint.UrlEncrypted)
	return NotificationWebhookResponse{
		ID:              uuidToString(endpoint.ID),
		Name:            endpoint.Name,
		MaskedURL:       maskWebhookURL(rawURL),
		Enabled:         endpoint.Enabled,
		WorkspaceID:     uuidToPtr(endpoint.WorkspaceID),
		PayloadTemplate: endpoint.PayloadTemplate,
		ContentPrefix:   endpoint.ContentPrefix,
		CreatedAt:       timestampToString(endpoint.CreatedAt),
		UpdatedAt:       timestampToString(endpoint.UpdatedAt),
	}
}

func notificationWebhooksToResponse(endpoints []db.NotificationWebhookEndpoint) []NotificationWebhookResponse {
	resp := make([]NotificationWebhookResponse, 0, len(endpoints))
	for _, endpoint := range endpoints {
		resp = append(resp, notificationWebhookToResponse(endpoint))
	}
	return resp
}

func (h *Handler) ListMyNotificationWebhooks(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	endpoints, err := h.Queries.ListNotificationWebhookEndpointsByUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load notification webhooks")
		return
	}
	writeJSON(w, http.StatusOK, ListNotificationWebhooksResponse{Webhooks: notificationWebhooksToResponse(endpoints)})
}

func (h *Handler) CreateMyNotificationWebhook(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req CreateNotificationWebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	name, endpointURL, payloadTemplate, contentPrefix, enabled, valid := normalizeWebhookRequest(
		w,
		r.Context(),
		req.Name,
		req.URL,
		req.PayloadTemplate,
		req.ContentPrefix,
		req.Enabled,
	)
	if !valid {
		return
	}

	urlEncrypted, err := notifyutil.EncryptToken(endpointURL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encrypt webhook url")
		return
	}
	workspaceID, ok := parseOptionalUUIDOrBadRequest(w, req.Workspace, "workspace_id")
	if !ok {
		return
	}
	endpoint, err := h.Queries.CreateNotificationWebhookEndpoint(r.Context(), db.CreateNotificationWebhookEndpointParams{
		UserID:          parseUUID(userID),
		WorkspaceID:     workspaceID,
		Name:            name,
		UrlEncrypted:    urlEncrypted,
		SecretEncrypted: pgtype.Text{},
		PayloadTemplate: payloadTemplate,
		ContentPrefix:   contentPrefix,
		Enabled:         enabled,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create notification webhook")
		return
	}

	writeJSON(w, http.StatusCreated, notificationWebhookToResponse(endpoint))
}

func parseOptionalUUIDOrBadRequest(w http.ResponseWriter, s, fieldName string) (pgtype.UUID, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return pgtype.UUID{}, true
	}
	return parseUUIDOrBadRequest(w, s, fieldName)
}

func (h *Handler) UpdateMyNotificationWebhook(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	endpointID := strings.TrimSpace(chi.URLParam(r, "webhookId"))
	if endpointID == "" {
		writeError(w, http.StatusBadRequest, "webhook id is required")
		return
	}

	existing, err := h.Queries.GetNotificationWebhookEndpointForUser(r.Context(), db.GetNotificationWebhookEndpointForUserParams{
		ID:     parseUUID(endpointID),
		UserID: parseUUID(userID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "notification webhook not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load notification webhook")
		return
	}

	var req UpdateNotificationWebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = existing.Name
	}
	endpointURL := strings.TrimSpace(req.URL)
	if endpointURL == "" {
		var decryptErr error
		endpointURL, decryptErr = notifyutil.DecryptToken(existing.UrlEncrypted)
		if decryptErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to decrypt webhook url")
			return
		}
	}
	enabled := existing.Enabled
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if err := notifyutil.ValidateWebhookURL(r.Context(), endpointURL, false); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	payloadTemplate := existing.PayloadTemplate
	if req.PayloadTemplate != nil {
		payloadTemplate = strings.TrimSpace(*req.PayloadTemplate)
	}
	if err := notifyutil.ValidateWebhookPayloadTemplate(payloadTemplate); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	contentPrefix := existing.ContentPrefix
	if req.ContentPrefix != nil {
		contentPrefix = *req.ContentPrefix
	}
	if len(contentPrefix) > 512 {
		writeError(w, http.StatusBadRequest, "webhook content prefix is too long")
		return
	}

	urlEncrypted, err := notifyutil.EncryptToken(endpointURL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encrypt webhook url")
		return
	}

	endpoint, err := h.Queries.UpdateNotificationWebhookEndpoint(r.Context(), db.UpdateNotificationWebhookEndpointParams{
		ID:              parseUUID(endpointID),
		UserID:          parseUUID(userID),
		Name:            name,
		UrlEncrypted:    urlEncrypted,
		SecretEncrypted: pgtype.Text{},
		PayloadTemplate: payloadTemplate,
		ContentPrefix:   contentPrefix,
		Enabled:         enabled,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update notification webhook")
		return
	}
	writeJSON(w, http.StatusOK, notificationWebhookToResponse(endpoint))
}

func (h *Handler) DeleteMyNotificationWebhook(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	endpointID := strings.TrimSpace(chi.URLParam(r, "webhookId"))
	if endpointID == "" {
		writeError(w, http.StatusBadRequest, "webhook id is required")
		return
	}
	if err := h.Queries.DeleteNotificationWebhookEndpoint(r.Context(), db.DeleteNotificationWebhookEndpointParams{
		ID:     parseUUID(endpointID),
		UserID: parseUUID(userID),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete notification webhook")
		return
	}
	endpoints, err := h.Queries.ListEnabledNotificationWebhookEndpointsByUser(r.Context(), parseUUID(userID))
	if err == nil && len(endpoints) == 0 && h.DB != nil {
		_, _ = h.DB.Exec(r.Context(), `
			UPDATE notification_channel_preference
			SET enabled = false, updated_at = now()
			WHERE user_id = $1 AND channel = 'custom_webhook'
		`, parseUUID(userID))
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) TestMyNotificationWebhook(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	endpointID := strings.TrimSpace(chi.URLParam(r, "webhookId"))
	if endpointID == "" {
		writeError(w, http.StatusBadRequest, "webhook id is required")
		return
	}

	endpoint, err := h.Queries.GetNotificationWebhookEndpointForUser(r.Context(), db.GetNotificationWebhookEndpointForUserParams{
		ID:     parseUUID(endpointID),
		UserID: parseUUID(userID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "notification webhook not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load notification webhook")
		return
	}

	endpointURL, err := decryptWebhookEndpointURL(endpoint)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to decrypt notification webhook")
		return
	}

	title := "Multica webhook test"
	body := "This is a test notification from Multica."
	payload, err := notifyutil.RenderWebhookPayload(endpoint.PayloadTemplate, notifyutil.BuildWebhookContent(title, body, "", endpoint.ContentPrefix), map[string]any{
		"event_type":        "test",
		"title":             title,
		"body":              body,
		"recipient_user_id": userID,
		"occurred_at":       time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build test payload")
		return
	}
	if err := (notifyutil.WebhookSender{}).SendJSON(r.Context(), endpointURL, "", payload); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, TestNotificationWebhookResponse{Message: "Webhook test sent"})
}

func normalizeWebhookRequest(
	w http.ResponseWriter,
	ctx context.Context,
	name string,
	endpointURL string,
	payloadTemplate string,
	contentPrefix string,
	enabled *bool,
) (string, string, string, string, bool, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Custom webhook"
	}
	endpointURL = strings.TrimSpace(endpointURL)
	if endpointURL == "" {
		writeError(w, http.StatusBadRequest, "webhook url is required")
		return "", "", "", "", false, false
	}
	if err := notifyutil.ValidateWebhookURL(ctx, endpointURL, false); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return "", "", "", "", false, false
	}
	payloadTemplate = strings.TrimSpace(payloadTemplate)
	if err := notifyutil.ValidateWebhookPayloadTemplate(payloadTemplate); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return "", "", "", "", false, false
	}
	if len(contentPrefix) > 512 {
		writeError(w, http.StatusBadRequest, "webhook content prefix is too long")
		return "", "", "", "", false, false
	}
	enabledValue := true
	if enabled != nil {
		enabledValue = *enabled
	}
	return name, endpointURL, payloadTemplate, contentPrefix, enabledValue, true
}

func decryptWebhookEndpointURL(endpoint db.NotificationWebhookEndpoint) (string, error) {
	endpointURL, err := notifyutil.DecryptToken(endpoint.UrlEncrypted)
	if err != nil {
		return "", err
	}
	return endpointURL, nil
}

func maskWebhookURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if len(raw) <= 24 {
		return raw[:min(8, len(raw))] + "..."
	}
	return raw[:16] + "..." + raw[len(raw)-8:]
}
