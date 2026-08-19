package handler

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

type webPushSubscriptionRequest struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256DH string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

func (h *Handler) GetWebPushConfig(w http.ResponseWriter, _ *http.Request) {
	if h.WebPush == nil {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false, "public_key": ""})
		return
	}
	publicKey, enabled := h.WebPush.PublicKey()
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":    enabled,
		"public_key": publicKey,
	})
}

func decodeWebPushSubscription(w http.ResponseWriter, r *http.Request) (webPushSubscriptionRequest, bool) {
	var request webPushSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return request, false
	}
	parsed, err := url.Parse(request.Endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || len(request.Endpoint) > 4096 || strings.TrimSpace(request.Keys.P256DH) == "" || strings.TrimSpace(request.Keys.Auth) == "" {
		writeError(w, http.StatusBadRequest, "invalid web push subscription")
		return request, false
	}
	return request, true
}

func (h *Handler) UpsertWebPushSubscription(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	request, ok := decodeWebPushSubscription(w, r)
	if !ok {
		return
	}
	if h.WebPush == nil {
		writeError(w, http.StatusServiceUnavailable, "web push is not configured")
		return
	}
	if err := h.WebPush.Upsert(r.Context(), userID, request.Endpoint, request.Keys.P256DH, request.Keys.Auth); err != nil {
		writeError(w, http.StatusServiceUnavailable, "failed to save web push subscription")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) DeleteWebPushSubscription(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var request struct {
		Endpoint string `json:"endpoint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || strings.TrimSpace(request.Endpoint) == "" {
		writeError(w, http.StatusBadRequest, "push endpoint is required")
		return
	}
	if h.WebPush == nil {
		writeError(w, http.StatusServiceUnavailable, "web push is not configured")
		return
	}
	if err := h.WebPush.Delete(r.Context(), userID, request.Endpoint); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete web push subscription")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
