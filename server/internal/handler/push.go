package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Web Push subscription endpoints.
//
// The frontend calls GetPushPublicKey on page load to learn whether push is
// enabled at the server (VAPID configured) and to get the application server
// key for pushManager.subscribe. After the browser hands back a
// PushSubscription, the frontend POSTs it to Subscribe; on tear-down it calls
// Unsubscribe with the endpoint.

type pushKeysJSON struct {
	P256dh string `json:"p256dh"`
	Auth   string `json:"auth"`
}

type subscribePushRequest struct {
	Endpoint  string       `json:"endpoint"`
	Keys      pushKeysJSON `json:"keys"`
	UserAgent string       `json:"userAgent,omitempty"`
}

type pushPublicKeyResponse struct {
	Enabled   bool   `json:"enabled"`
	PublicKey string `json:"publicKey,omitempty"`
}

type pushSubscriptionResponse struct {
	ID        string  `json:"id"`
	Endpoint  string  `json:"endpoint"`
	UserAgent *string `json:"user_agent,omitempty"`
	CreatedAt string  `json:"created_at"`
}

// GetPushPublicKey returns whether push is enabled and the VAPID public key.
// Public-ish endpoint: still requires auth so unrelated clients can't probe.
func (h *Handler) GetPushPublicKey(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	if h.PushService == nil || !h.PushService.Enabled() {
		writeJSON(w, http.StatusOK, pushPublicKeyResponse{Enabled: false})
		return
	}
	writeJSON(w, http.StatusOK, pushPublicKeyResponse{
		Enabled:   true,
		PublicKey: h.PushService.PublicKey(),
	})
}

// SubscribePush stores a Web Push subscription for the current user.
func (h *Handler) SubscribePush(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req subscribePushRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	req.Endpoint = strings.TrimSpace(req.Endpoint)
	if req.Endpoint == "" || req.Keys.P256dh == "" || req.Keys.Auth == "" {
		writeError(w, http.StatusBadRequest, "endpoint, keys.p256dh and keys.auth are required")
		return
	}

	row, err := h.Queries.UpsertPushSubscription(r.Context(), db.UpsertPushSubscriptionParams{
		UserID:    parseUUID(userID),
		Endpoint:  req.Endpoint,
		P256dh:    req.Keys.P256dh,
		Auth:      req.Keys.Auth,
		UserAgent: util.StrToText(req.UserAgent),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save subscription")
		return
	}

	writeJSON(w, http.StatusOK, pushSubscriptionResponse{
		ID:        uuidToString(row.ID),
		Endpoint:  row.Endpoint,
		UserAgent: textToPtr(row.UserAgent),
		CreatedAt: timestampToString(row.CreatedAt),
	})
}

type unsubscribePushRequest struct {
	Endpoint string `json:"endpoint"`
}

// UnsubscribePush removes a Web Push subscription for the current user.
// Idempotent: returns 200 even if the endpoint doesn't exist or belongs to
// another user (the row stays untouched in that case).
func (h *Handler) UnsubscribePush(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req unsubscribePushRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Endpoint == "" {
		writeError(w, http.StatusBadRequest, "endpoint is required")
		return
	}

	if _, err := h.Queries.DeletePushSubscriptionByEndpoint(r.Context(), db.DeletePushSubscriptionByEndpointParams{
		UserID:   parseUUID(userID),
		Endpoint: req.Endpoint,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove subscription")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListPushSubscriptions lists the current user's active subscriptions across
// all devices. Used by settings UI to show "this device + N other devices".
func (h *Handler) ListPushSubscriptions(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	rows, err := h.Queries.ListPushSubscriptionsByUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list subscriptions")
		return
	}

	resp := make([]pushSubscriptionResponse, len(rows))
	for i, row := range rows {
		resp[i] = pushSubscriptionResponse{
			ID:        uuidToString(row.ID),
			Endpoint:  row.Endpoint,
			UserAgent: textToPtr(row.UserAgent),
			CreatedAt: timestampToString(row.CreatedAt),
		}
	}
	writeJSON(w, http.StatusOK, resp)
}
