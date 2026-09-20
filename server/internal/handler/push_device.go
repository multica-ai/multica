package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type pushDeviceRequest struct {
	ExpoPushToken string `json:"expo_push_token"`
	Platform      string `json:"platform"`
}

func validExpoPushToken(token string) bool {
	return (strings.HasPrefix(token, "ExponentPushToken[") ||
		strings.HasPrefix(token, "ExpoPushToken[")) &&
		strings.HasSuffix(token, "]") && len(token) <= 512
}

func decodePushDeviceRequest(w http.ResponseWriter, r *http.Request) (pushDeviceRequest, bool) {
	var req pushDeviceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return req, false
	}
	if !validExpoPushToken(req.ExpoPushToken) {
		writeError(w, http.StatusBadRequest, "invalid Expo push token")
		return req, false
	}
	if req.Platform != "ios" && req.Platform != "android" {
		writeError(w, http.StatusBadRequest, "platform must be ios or android")
		return req, false
	}
	return req, true
}

// RegisterPushDevice associates an Expo push token with the signed-in member.
// A token is globally unique and may move between accounts after sign-out/sign-in
// on a shared device, so the upsert deliberately replaces its previous owner.
func (h *Handler) RegisterPushDevice(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	req, ok := decodePushDeviceRequest(w, r)
	if !ok {
		return
	}

	device, err := h.Queries.UpsertPushDevice(r.Context(), db.UpsertPushDeviceParams{
		UserID:        parseUUID(userID),
		ExpoPushToken: req.ExpoPushToken,
		Platform:      req.Platform,
	})
	if err != nil {
		slog.Warn("UpsertPushDevice failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to register push device")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":       device.ID,
		"platform": device.Platform,
	})
}

// UnregisterPushDevice stops delivery to this token for the signed-in member.
// It is idempotent so logout can safely retry it.
func (h *Handler) UnregisterPushDevice(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	req, ok := decodePushDeviceRequest(w, r)
	if !ok {
		return
	}

	if err := h.Queries.DeletePushDevice(r.Context(), db.DeletePushDeviceParams{
		UserID:        parseUUID(userID),
		ExpoPushToken: req.ExpoPushToken,
	}); err != nil {
		slog.Warn("DeletePushDevice failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to unregister push device")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
