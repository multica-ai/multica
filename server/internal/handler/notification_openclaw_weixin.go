package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// BindOpenclawWeixinRequest is the payload for manually binding a WeChat ID.
type BindOpenclawWeixinRequest struct {
	WechatID string `json:"wechat_id"`
	Channel  string `json:"channel,omitempty"` // defaults to "openclaw-weixin"
}

// BindMyOpenclawWeixin handles PUT /api/me/notification-bindings/openclaw-weixin.
// It upserts an external_account_binding with provider=openclaw_weixin.
func (h *Handler) BindMyOpenclawWeixin(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req BindOpenclawWeixinRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	wechatID := strings.TrimSpace(req.WechatID)
	if wechatID == "" {
		writeError(w, http.StatusBadRequest, "wechat_id is required")
		return
	}

	channel := strings.TrimSpace(req.Channel)
	if channel == "" {
		channel = "openclaw-weixin"
	}

	metadata, _ := json.Marshal(map[string]string{
		"channel": channel,
	})

	binding, err := h.Queries.UpsertExternalAccountBinding(r.Context(), db.UpsertExternalAccountBindingParams{
		UserID:                parseUUID(userID),
		Provider:              "openclaw_weixin",
		ExternalUserID:        wechatID,
		DisplayName:           strToText(wechatID),
		AccessTokenEncrypted:  pgtype.Text{},
		RefreshTokenEncrypted: pgtype.Text{},
		TokenExpiresAt:        pgtype.Timestamptz{},
		Status:                "active",
		Metadata:              metadata,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save openclaw weixin binding")
		return
	}

	writeJSON(w, http.StatusOK, NotificationBindingResponse{
		ID:             uuidToString(binding.ID),
		Provider:       binding.Provider,
		ExternalUserID: binding.ExternalUserID,
		DisplayName:    textToPtr(binding.DisplayName),
		Status:         binding.Status,
		Metadata:       json.RawMessage(binding.Metadata),
		CreatedAt:      timestampToString(binding.CreatedAt),
		UpdatedAt:      timestampToString(binding.UpdatedAt),
	})
}
