package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// WebhookEndpointResponse is the API response for a webhook endpoint.
type WebhookEndpointResponse struct {
	ID          string   `json:"id"`
	WorkspaceID string   `json:"workspace_id"`
	URL         string   `json:"url"`
	Description *string  `json:"description"`
	EventTypes  []string `json:"event_types"`
	Enabled     bool     `json:"enabled"`
	CreatedBy   string   `json:"created_by"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

// WebhookDeliveryResponse is the API response for a delivery log entry.
type WebhookDeliveryResponse struct {
	ID           string          `json:"id"`
	EndpointID   string          `json:"endpoint_id"`
	EventType    string          `json:"event_type"`
	Payload      json.RawMessage `json:"payload"`
	Status       string          `json:"status"`
	HTTPStatus   *int32          `json:"http_status"`
	ResponseBody *string         `json:"response_body"`
	ErrorMessage *string         `json:"error_message"`
	Attempt      int32           `json:"attempt"`
	CreatedAt    string          `json:"created_at"`
	DeliveredAt  *string         `json:"delivered_at"`
}

func webhookEndpointToResponse(ep db.WebhookEndpoint) WebhookEndpointResponse {
	var desc *string
	if ep.Description.Valid {
		desc = &ep.Description.String
	}
	eventTypes := ep.EventTypes
	if eventTypes == nil {
		eventTypes = []string{}
	}
	return WebhookEndpointResponse{
		ID:          uuidToString(ep.ID),
		WorkspaceID: uuidToString(ep.WorkspaceID),
		URL:         ep.Url,
		Description: desc,
		EventTypes:  eventTypes,
		Enabled:     ep.Enabled,
		CreatedBy:   uuidToString(ep.CreatedBy),
		CreatedAt:   timestampToString(ep.CreatedAt),
		UpdatedAt:   timestampToString(ep.UpdatedAt),
	}
}

func webhookDeliveryToResponse(d db.WebhookDelivery) WebhookDeliveryResponse {
	var httpStatus *int32
	if d.HttpStatus.Valid {
		httpStatus = &d.HttpStatus.Int32
	}
	var respBody *string
	if d.ResponseBody.Valid {
		respBody = &d.ResponseBody.String
	}
	var errMsg *string
	if d.ErrorMessage.Valid {
		errMsg = &d.ErrorMessage.String
	}
	var deliveredAt *string
	if d.DeliveredAt.Valid {
		s := timestampToString(d.DeliveredAt)
		deliveredAt = &s
	}
	return WebhookDeliveryResponse{
		ID:           uuidToString(d.ID),
		EndpointID:   uuidToString(d.EndpointID),
		EventType:    d.EventType,
		Payload:      json.RawMessage(d.Payload),
		Status:       d.Status,
		HTTPStatus:   httpStatus,
		ResponseBody: respBody,
		ErrorMessage: errMsg,
		Attempt:      d.Attempt,
		CreatedAt:    timestampToString(d.CreatedAt),
		DeliveredAt:  deliveredAt,
	}
}

func generateWebhookSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "whsec_" + hex.EncodeToString(b), nil
}

// ListWebhookEndpoints returns all webhook endpoints for a workspace.
func (h *Handler) ListWebhookEndpoints(w http.ResponseWriter, r *http.Request) {
	workspaceID := ctxWorkspaceID(r.Context())
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}

	endpoints, err := h.Queries.ListWebhookEndpoints(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list webhooks")
		return
	}

	resp := make([]WebhookEndpointResponse, len(endpoints))
	for i, ep := range endpoints {
		resp[i] = webhookEndpointToResponse(ep)
	}
	writeJSON(w, http.StatusOK, resp)
}

// CreateWebhookEndpoint creates a new webhook endpoint.
func (h *Handler) CreateWebhookEndpoint(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}

	var req struct {
		URL         string   `json:"url"`
		Description string   `json:"description"`
		EventTypes  []string `json:"event_types"`
		Enabled     *bool    `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.URL == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}

	secret, err := generateWebhookSecret()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate secret")
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	if req.EventTypes == nil {
		req.EventTypes = []string{}
	}

	ep, err := h.Queries.CreateWebhookEndpoint(r.Context(), db.CreateWebhookEndpointParams{
		WorkspaceID: parseUUID(workspaceID),
		Url:         req.URL,
		Secret:      secret,
		Description: pgtype.Text{String: req.Description, Valid: req.Description != ""},
		EventTypes:  req.EventTypes,
		Enabled:     enabled,
		CreatedBy:   parseUUID(userID),
	})
	if err != nil {
		slog.Error("webhook: create failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create webhook")
		return
	}

	// Include secret in creation response only (never returned again).
	resp := webhookEndpointToResponse(ep)
	writeJSON(w, http.StatusCreated, map[string]any{
		"endpoint": resp,
		"secret":   secret,
	})
}

// GetWebhookEndpoint returns a single webhook endpoint.
func (h *Handler) GetWebhookEndpoint(w http.ResponseWriter, r *http.Request) {
	workspaceID := ctxWorkspaceID(r.Context())
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}

	id := chi.URLParam(r, "id")
	ep, err := h.Queries.GetWebhookEndpointInWorkspace(r.Context(), db.GetWebhookEndpointInWorkspaceParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "webhook not found")
		return
	}

	writeJSON(w, http.StatusOK, webhookEndpointToResponse(ep))
}

// UpdateWebhookEndpoint updates an existing webhook endpoint.
func (h *Handler) UpdateWebhookEndpoint(w http.ResponseWriter, r *http.Request) {
	workspaceID := ctxWorkspaceID(r.Context())
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}

	id := chi.URLParam(r, "id")
	var req struct {
		URL         *string  `json:"url"`
		Description *string  `json:"description"`
		EventTypes  []string `json:"event_types"`
		Enabled     *bool    `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	params := db.UpdateWebhookEndpointParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	}
	if req.URL != nil {
		params.Url = pgtype.Text{String: *req.URL, Valid: true}
	}
	if req.Description != nil {
		params.Description = pgtype.Text{String: *req.Description, Valid: true}
	}
	if req.EventTypes != nil {
		params.EventTypes = req.EventTypes
	}
	if req.Enabled != nil {
		params.Enabled = pgtype.Bool{Bool: *req.Enabled, Valid: true}
	}

	ep, err := h.Queries.UpdateWebhookEndpoint(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusNotFound, "webhook not found")
		return
	}

	writeJSON(w, http.StatusOK, webhookEndpointToResponse(ep))
}

// DeleteWebhookEndpoint deletes a webhook endpoint.
func (h *Handler) DeleteWebhookEndpoint(w http.ResponseWriter, r *http.Request) {
	workspaceID := ctxWorkspaceID(r.Context())
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}

	id := chi.URLParam(r, "id")
	err := h.Queries.DeleteWebhookEndpoint(r.Context(), db.DeleteWebhookEndpointParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "webhook not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// TestWebhookEndpoint sends a test event to a webhook endpoint.
func (h *Handler) TestWebhookEndpoint(w http.ResponseWriter, r *http.Request) {
	workspaceID := ctxWorkspaceID(r.Context())
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}

	id := chi.URLParam(r, "id")
	ep, err := h.Queries.GetWebhookEndpointInWorkspace(r.Context(), db.GetWebhookEndpointInWorkspaceParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "webhook not found")
		return
	}

	webhookSvc := service.NewWebhookService(h.Queries)
	webhookSvc.Deliver(r.Context(), ep, "webhook:test", map[string]string{
		"message": "This is a test webhook delivery from Multica.",
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "test_sent"})
}

// ListWebhookDeliveries returns delivery history for a webhook endpoint.
func (h *Handler) ListWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	workspaceID := ctxWorkspaceID(r.Context())
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}

	id := chi.URLParam(r, "id")

	// Verify endpoint belongs to workspace.
	_, err := h.Queries.GetWebhookEndpointInWorkspace(r.Context(), db.GetWebhookEndpointInWorkspaceParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "webhook not found")
		return
	}

	deliveries, err := h.Queries.ListWebhookDeliveries(r.Context(), db.ListWebhookDeliveriesParams{
		EndpointID: parseUUID(id),
		Limit:      50,
		Offset:     0,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list deliveries")
		return
	}

	resp := make([]WebhookDeliveryResponse, len(deliveries))
	for i, d := range deliveries {
		resp[i] = webhookDeliveryToResponse(d)
	}
	writeJSON(w, http.StatusOK, resp)
}
