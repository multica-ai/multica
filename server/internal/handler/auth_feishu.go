package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Feishu (飞书) enterprise OAuth login. Uses the v2 token endpoint which
// accepts client_id / client_secret directly and does not require a prior
// app_access_token exchange.
//
// Docs: https://open.feishu.cn/document/server-docs/authentication-management/access-token/get-user-access-token
const (
	feishuTokenURL    = "https://open.feishu.cn/open-apis/authen/v2/oauth/token"
	feishuUserInfoURL = "https://open.feishu.cn/open-apis/authen/v1/user_info"
)

type FeishuLoginRequest struct {
	Code        string `json:"code"`
	RedirectURI string `json:"redirect_uri"`
}

// feishuTokenResponse is the v2 oauth/token success body. Feishu follows the
// OAuth 2.0 standard here, so fields match RFC 6749.
type feishuTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	Scope       string `json:"scope"`
}

// feishuUserInfoResponse wraps Feishu's standard response envelope.
// An email may be absent if the user hasn't bound one; enterprise_email is
// populated when the tenant issues mailboxes, and we prefer it when present.
type feishuUserInfoResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Name            string `json:"name"`
		EnName          string `json:"en_name"`
		AvatarURL       string `json:"avatar_url"`
		Email           string `json:"email"`
		EnterpriseEmail string `json:"enterprise_email"`
	} `json:"data"`
}

func (h *Handler) FeishuLogin(w http.ResponseWriter, r *http.Request) {
	var req FeishuLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Code == "" {
		writeError(w, http.StatusBadRequest, "code is required")
		return
	}

	appID := os.Getenv("FEISHU_APP_ID")
	appSecret := os.Getenv("FEISHU_APP_SECRET")
	if appID == "" || appSecret == "" {
		writeError(w, http.StatusServiceUnavailable, "Feishu login is not configured")
		return
	}

	redirectURI := req.RedirectURI
	if redirectURI == "" {
		redirectURI = os.Getenv("FEISHU_REDIRECT_URI")
	}

	// Exchange authorization code for a user access token.
	tokenBody, err := json.Marshal(map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     appID,
		"client_secret": appSecret,
		"code":          req.Code,
		"redirect_uri":  redirectURI,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	tokenReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, feishuTokenURL, bytes.NewReader(tokenBody))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	tokenReq.Header.Set("Content-Type", "application/json")

	tokenResp, err := http.DefaultClient.Do(tokenReq)
	if err != nil {
		slog.Error("feishu oauth token exchange failed", "error", err)
		writeError(w, http.StatusBadGateway, "failed to exchange code with Feishu")
		return
	}
	defer tokenResp.Body.Close()

	tokenRespBytes, err := io.ReadAll(tokenResp.Body)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to read Feishu token response")
		return
	}

	if tokenResp.StatusCode != http.StatusOK {
		slog.Error("feishu oauth token exchange returned error", "status", tokenResp.StatusCode, "body", string(tokenRespBytes))
		writeError(w, http.StatusBadRequest, "failed to exchange code with Feishu")
		return
	}

	var fToken feishuTokenResponse
	if err := json.Unmarshal(tokenRespBytes, &fToken); err != nil {
		writeError(w, http.StatusBadGateway, "failed to parse Feishu token response")
		return
	}
	if fToken.AccessToken == "" {
		slog.Error("feishu oauth token missing from response", "body", string(tokenRespBytes))
		writeError(w, http.StatusBadGateway, "invalid Feishu token response")
		return
	}

	// Fetch user profile with the user access token.
	userInfoReq, err := http.NewRequestWithContext(r.Context(), http.MethodGet, feishuUserInfoURL, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	userInfoReq.Header.Set("Authorization", "Bearer "+fToken.AccessToken)

	userInfoResp, err := http.DefaultClient.Do(userInfoReq)
	if err != nil {
		slog.Error("feishu userinfo fetch failed", "error", err)
		writeError(w, http.StatusBadGateway, "failed to fetch user info from Feishu")
		return
	}
	defer userInfoResp.Body.Close()

	var fUser feishuUserInfoResponse
	if err := json.NewDecoder(userInfoResp.Body).Decode(&fUser); err != nil {
		writeError(w, http.StatusBadGateway, "failed to parse Feishu user info")
		return
	}
	if fUser.Code != 0 {
		slog.Error("feishu userinfo returned error", "code", fUser.Code, "msg", fUser.Msg)
		writeError(w, http.StatusBadGateway, "failed to fetch user info from Feishu")
		return
	}

	// Email is required — we reject login when absent. enterprise_email is
	// the company-issued address (preferred when set), email is the personal
	// one the user linked to their Feishu account.
	rawEmail := fUser.Data.EnterpriseEmail
	if rawEmail == "" {
		rawEmail = fUser.Data.Email
	}
	if rawEmail == "" {
		writeError(w, http.StatusBadRequest, "Feishu account has no email. Please bind an email to your Feishu account or contact your tenant admin to enable enterprise email.")
		return
	}
	email := strings.ToLower(strings.TrimSpace(rawEmail))

	user, isNew, err := h.findOrCreateUser(r.Context(), email)
	if err != nil {
		var signupErr SignupError
		if errors.As(err, &signupErr) {
			writeError(w, http.StatusForbidden, signupErr.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create user")
		return
	}
	if isNew {
		h.Analytics.Capture(analytics.Signup(uuidToString(user.ID), user.Email, "feishu"))
	}

	// Backfill name and avatar from Feishu profile if the user was just
	// created (default name is email prefix) or has no avatar yet.
	displayName := fUser.Data.Name
	if displayName == "" {
		displayName = fUser.Data.EnName
	}

	needsUpdate := false
	newName := user.Name
	newAvatar := user.AvatarUrl

	if displayName != "" && user.Name == strings.Split(email, "@")[0] {
		newName = displayName
		needsUpdate = true
	}
	if fUser.Data.AvatarURL != "" && !user.AvatarUrl.Valid {
		newAvatar = pgtype.Text{String: fUser.Data.AvatarURL, Valid: true}
		needsUpdate = true
	}

	if needsUpdate {
		updated, err := h.Queries.UpdateUser(r.Context(), db.UpdateUserParams{
			ID:        user.ID,
			Name:      newName,
			AvatarUrl: newAvatar,
		})
		if err == nil {
			user = updated
		}
	}

	tokenString, err := h.issueJWT(user)
	if err != nil {
		slog.Warn("feishu login failed", append(logger.RequestAttrs(r), "error", err, "email", email)...)
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	if err := auth.SetAuthCookies(w, tokenString); err != nil {
		slog.Warn("failed to set auth cookies", "error", err)
	}

	if h.CFSigner != nil {
		for _, cookie := range h.CFSigner.SignedCookies(time.Now().Add(72 * time.Hour)) {
			http.SetCookie(w, cookie)
		}
	}

	slog.Info("user logged in via feishu", append(logger.RequestAttrs(r), "user_id", uuidToString(user.ID), "email", user.Email)...)
	writeJSON(w, http.StatusOK, LoginResponse{
		Token: tokenString,
		User:  userToResponse(user),
	})
}
