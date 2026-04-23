package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	notifyutil "github.com/multica-ai/multica/server/internal/notify"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const dingTalkStateTTL = 15 * time.Minute

type StartDingTalkBindingRequest struct {
	NextPath string `json:"next_path"`
}

type StartDingTalkBindingResponse struct {
	AuthURL string `json:"auth_url"`
}

type CompleteDingTalkBindingRequest struct {
	Code  string `json:"code"`
	State string `json:"state"`
}

type CompleteDingTalkBindingResponse struct {
	Binding  NotificationBindingResponse `json:"binding"`
	NextPath *string                     `json:"next_path"`
}

func firstNotificationValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func sanitizeRelativePath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") {
		return ""
	}
	return raw
}

func (h *Handler) StartMyDingTalkBinding(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	cfg, err := notifyutil.LoadDingTalkConfig()
	if err != nil {
		if errors.Is(err, notifyutil.ErrDingTalkNotConfigured) {
			writeError(w, http.StatusServiceUnavailable, "DingTalk binding is not configured")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load DingTalk configuration")
		return
	}

	var req StartDingTalkBindingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	state, err := notifyutil.BuildDingTalkState(notifyutil.DingTalkBindingState{
		UserID:   userID,
		NextPath: sanitizeRelativePath(req.NextPath),
		IssuedAt: time.Now().UTC().Unix(),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start DingTalk binding")
		return
	}

	writeJSON(w, http.StatusOK, StartDingTalkBindingResponse{
		AuthURL: cfg.AuthorizationURL(state),
	})
}

func (h *Handler) CompleteMyDingTalkBinding(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req CompleteDingTalkBindingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Code) == "" || strings.TrimSpace(req.State) == "" {
		writeError(w, http.StatusBadRequest, "code and state are required")
		return
	}

	cfg, err := notifyutil.LoadDingTalkConfig()
	if err != nil {
		if errors.Is(err, notifyutil.ErrDingTalkNotConfigured) {
			writeError(w, http.StatusServiceUnavailable, "DingTalk binding is not configured")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load DingTalk configuration")
		return
	}

	state, err := notifyutil.ParseDingTalkState(req.State)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid DingTalk callback state")
		return
	}
	if state.UserID != userID {
		writeError(w, http.StatusForbidden, "DingTalk callback state does not match the current user")
		return
	}
	if time.Since(time.Unix(state.IssuedAt, 0)) > dingTalkStateTTL {
		writeError(w, http.StatusBadRequest, "DingTalk callback state has expired")
		return
	}

	token, err := cfg.ExchangeCode(r.Context(), strings.TrimSpace(req.Code))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	profile, err := cfg.GetUserProfile(r.Context(), token.AccessToken)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	accessTokenEncrypted, err := notifyutil.EncryptToken(token.AccessToken)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encrypt DingTalk access token")
		return
	}
	refreshTokenEncrypted, err := notifyutil.EncryptToken(token.RefreshToken)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encrypt DingTalk refresh token")
		return
	}

	externalUserID := firstNotificationValue(profile.UnionID, profile.OpenID, token.OpenID)
	displayName := firstNotificationValue(profile.Name, profile.Nick)
	if externalUserID == "" {
		writeError(w, http.StatusBadGateway, "DingTalk user info missing supported identifiers")
		return
	}

	metadata, err := json.Marshal(map[string]any{
		"corp_id":    token.CorpID,
		"open_id":    firstNotificationValue(profile.OpenID, token.OpenID),
		"union_id":   profile.UnionID,
		"avatar_url": profile.AvatarURL,
		"mobile":     profile.Mobile,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode DingTalk binding metadata")
		return
	}

	expiresAt := pgtype.Timestamptz{}
	if token.ExpiresIn > 0 {
		expiresAt = pgtype.Timestamptz{
			Time:  time.Now().UTC().Add(time.Duration(token.ExpiresIn) * time.Second),
			Valid: true,
		}
	}

	binding, err := h.Queries.UpsertExternalAccountBinding(r.Context(), db.UpsertExternalAccountBindingParams{
		UserID:                parseUUID(userID),
		Provider:              "dingtalk",
		ExternalUserID:        externalUserID,
		DisplayName:           strToText(displayName),
		AccessTokenEncrypted:  strToText(accessTokenEncrypted),
		RefreshTokenEncrypted: strToText(refreshTokenEncrypted),
		TokenExpiresAt:        expiresAt,
		Status:                "active",
		Metadata:              metadata,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to persist DingTalk binding")
		return
	}

	resp := notificationBindingsToResponse([]db.ExternalAccountBinding{binding})
	writeJSON(w, http.StatusOK, CompleteDingTalkBindingResponse{
		Binding: resp[0],
		NextPath: func() *string {
			if next := sanitizeRelativePath(state.NextPath); next != "" {
				return &next
			}
			return nil
		}(),
	})
}
