package handler

import (
	"encoding/json"
	"net/http"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type RegisterDeviceTokenRequest struct {
	Token    string `json:"token"`
	Platform string `json:"platform"`
}

func (h *Handler) RegisterDeviceToken(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req RegisterDeviceTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	if req.Token == "" || req.Platform == "" {
		writeError(w, http.StatusBadRequest, "token and platform are required")
		return
	}

	err := h.Queries.UpsertUserDeviceToken(r.Context(), db.UpsertUserDeviceTokenParams{
		UserID:   parseUUID(userID),
		Token:    req.Token,
		Platform: req.Platform,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to register device token")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
