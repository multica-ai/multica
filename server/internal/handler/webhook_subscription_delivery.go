package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ── Response type ─────────────────────────────────────────────────────────────

// OutboundWebhookDeliveryResponse is the authenticated-API view of an
// outbound_webhook_delivery row (see migration 122). The list endpoint returns
// these without request/response bodies; the detail endpoint includes both for
// debugging. The signing secret is never echoed through this surface.
type OutboundWebhookDeliveryResponse struct {
	ID                string  `json:"id"`
	WorkspaceID       string  `json:"workspace_id"`
	SubscriptionID    string  `json:"subscription_id"`
	Event             string  `json:"event"`
	Status            string  `json:"status"`
	AttemptCount      int32   `json:"attempt_count"`
	ResponseStatus    *int32  `json:"response_status"`
	Error             *string `json:"error"`
	RedeliveredFromID *string `json:"redelivered_from_id"`
	CreatedAt         string  `json:"created_at"`

	// Detail-only fields. List responses leave these nil so a page of N
	// deliveries never serialises N copies of the signed payload. Detail
	// requests opt in by hitting GET /deliveries/{deliveryId}.
	RequestBody  *string `json:"request_body,omitempty"`
	ResponseBody *string `json:"response_body,omitempty"`
}

// slimOutboundDeliveryToResponse maps the projected list row (no bodies) into
// the wire shape.
func slimOutboundDeliveryToResponse(d db.ListOutboundWebhookDeliveriesRow) OutboundWebhookDeliveryResponse {
	resp := OutboundWebhookDeliveryResponse{
		ID:                uuidToString(d.ID),
		WorkspaceID:       uuidToString(d.WorkspaceID),
		SubscriptionID:    uuidToString(d.SubscriptionID),
		Event:             d.Event,
		Status:            d.Status,
		AttemptCount:      d.AttemptCount,
		Error:             textToPtr(d.Error),
		RedeliveredFromID: uuidToPtr(d.RedeliveredFromID),
		CreatedAt:         timestampToString(d.CreatedAt),
	}
	if d.ResponseStatus.Valid {
		v := d.ResponseStatus.Int32
		resp.ResponseStatus = &v
	}
	return resp
}

// outboundDeliveryToResponse maps a full row; detail=true includes bodies.
func outboundDeliveryToResponse(d db.OutboundWebhookDelivery, detail bool) OutboundWebhookDeliveryResponse {
	resp := OutboundWebhookDeliveryResponse{
		ID:                uuidToString(d.ID),
		WorkspaceID:       uuidToString(d.WorkspaceID),
		SubscriptionID:    uuidToString(d.SubscriptionID),
		Event:             d.Event,
		Status:            d.Status,
		AttemptCount:      d.AttemptCount,
		Error:             textToPtr(d.Error),
		RedeliveredFromID: uuidToPtr(d.RedeliveredFromID),
		CreatedAt:         timestampToString(d.CreatedAt),
	}
	if d.ResponseStatus.Valid {
		v := d.ResponseStatus.Int32
		resp.ResponseStatus = &v
	}
	if detail {
		if len(d.RequestBody) > 0 {
			s := string(d.RequestBody)
			resp.RequestBody = &s
		}
		if d.ResponseBody.Valid {
			v := d.ResponseBody.String
			resp.ResponseBody = &v
		}
	}
	return resp
}

// ── Handlers ──────────────────────────────────────────────────────────────────

// ListWebhookSubscriptionDeliveries returns recent deliveries for one outbound
// webhook subscription. Slim projection — request/response bodies are omitted;
// use GetWebhookSubscriptionDelivery for the full payload. Owner/admin gated +
// workspace-scoped via loadWebhookSubscription.
func (h *Handler) ListWebhookSubscriptionDeliveries(w http.ResponseWriter, r *http.Request) {
	sub, ok := h.loadWebhookSubscription(w, r)
	if !ok {
		return
	}

	limit, offset := parsePagination(r, 20, 100)

	rows, err := h.Queries.ListOutboundWebhookDeliveries(r.Context(), db.ListOutboundWebhookDeliveriesParams{
		SubscriptionID: sub.ID,
		WorkspaceID:    sub.WorkspaceID,
		Limit:          limit,
		Offset:         offset,
	})
	if err != nil {
		slog.Error("list outbound webhook deliveries failed", "error", err, "subscription_id", uuidToString(sub.ID))
		writeError(w, http.StatusInternalServerError, "failed to list deliveries")
		return
	}

	// total comes from the window count on each row; an empty page means 0.
	var total int64
	if len(rows) > 0 {
		total = rows[0].Total
	}
	resp := make([]OutboundWebhookDeliveryResponse, len(rows))
	for i, row := range rows {
		resp[i] = slimOutboundDeliveryToResponse(row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"deliveries": resp, "total": total})
}

// GetWebhookSubscriptionDelivery returns one delivery in full, including the
// request body we sent and the (truncated) response. The delivery is re-checked
// to belong to the loaded subscription so a guessed id from another
// subscription/workspace cannot leak data.
func (h *Handler) GetWebhookSubscriptionDelivery(w http.ResponseWriter, r *http.Request) {
	sub, ok := h.loadWebhookSubscription(w, r)
	if !ok {
		return
	}
	delivery, ok := h.loadOutboundDeliveryForSubscription(w, r, sub)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, outboundDeliveryToResponse(delivery, true))
}

// RedeliverWebhookSubscriptionDelivery re-POSTs the stored payload of a prior
// delivery to the subscription's CURRENT url with a fresh signature. Async
// (enqueued on the dispatcher, like a normal delivery), so it returns 202; the
// new row — carrying redelivered_from_id — appears in the list once it lands.
func (h *Handler) RedeliverWebhookSubscriptionDelivery(w http.ResponseWriter, r *http.Request) {
	sub, ok := h.loadWebhookSubscription(w, r)
	if !ok {
		return
	}
	delivery, ok := h.loadOutboundDeliveryForSubscription(w, r, sub)
	if !ok {
		return
	}
	if len(delivery.RequestBody) == 0 {
		writeError(w, http.StatusBadRequest, "delivery has no stored payload to redeliver")
		return
	}
	if h.WebhookDispatcher == nil {
		writeError(w, http.StatusServiceUnavailable, "webhook delivery is not available")
		return
	}
	if !h.WebhookDispatcher.Redeliver(sub, delivery.Event, delivery.RequestBody, delivery.ID) {
		// Queue full — transient back-pressure; the operator can retry.
		writeError(w, http.StatusServiceUnavailable, "delivery queue is full, try again shortly")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "queued"})
}

// loadOutboundDeliveryForSubscription loads a delivery by id, scoped to the
// loaded subscription's workspace, and verifies it belongs to that
// subscription (404 otherwise — no cross-subscription/workspace leak).
func (h *Handler) loadOutboundDeliveryForSubscription(w http.ResponseWriter, r *http.Request, sub db.WebhookSubscription) (db.OutboundWebhookDelivery, bool) {
	deliveryID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "deliveryId"), "delivery id")
	if !ok {
		return db.OutboundWebhookDelivery{}, false
	}
	delivery, err := h.Queries.GetOutboundWebhookDeliveryInWorkspace(r.Context(), db.GetOutboundWebhookDeliveryInWorkspaceParams{
		ID:          deliveryID,
		WorkspaceID: sub.WorkspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "delivery not found")
			return db.OutboundWebhookDelivery{}, false
		}
		writeError(w, http.StatusInternalServerError, "failed to load delivery")
		return db.OutboundWebhookDelivery{}, false
	}
	if delivery.SubscriptionID != sub.ID {
		// Belongs to a different subscription in the same workspace.
		writeError(w, http.StatusNotFound, "delivery not found")
		return db.OutboundWebhookDelivery{}, false
	}
	return delivery, true
}
