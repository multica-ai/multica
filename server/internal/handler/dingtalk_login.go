package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/logger"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// DingTalk OAuth login for self-hosted deployments (企业内部应用).
//
// Flow mirrors GoogleLogin: the web frontend receives the DingTalk callback
// (authCode) and POSTs it here; the server exchanges it for the user's
// identity, then reuses the shared findOrCreateUser / issueJWT path.
//
// Email semantics (verified against the live API, see DINGTALK_OAUTH.md):
// the login email is exactly one thing — the enterprise mailbox (org_email,
// 企业邮箱) on the app-org member record, resolved server-side: app access
// token -> getbyunionid (lowercase; the camelCase spelling answers errcode 22
// "Invalid method") -> v2/user/get. The field needs the
// 「企业员工手机号信息和邮箱等个人信息」 sensitive permission. No org_email
// on the record (or no record at all) means no login — there is no fallback
// email source, by design.
//
// me.email is deliberately NOT used as a login key. The consent response
// carries account-level data that crosses org boundaries: with the app
// registered under an unrelated personal org, me still returned the user's
// company-org enterprise email (and mobile). Logging users in through that
// would bind the app's org to an address it never assigned — and for
// multi-org accounts the selection is undocumented and unstable.
//
// Requires DINGTALK_CLIENT_ID / DINGTALK_CLIENT_SECRET.

type DingTalkLoginRequest struct {
	AuthCode string `json:"auth_code"`
}

type dingtalkUserAccessTokenResponse struct {
	AccessToken string `json:"accessToken"`
	ExpireIn    int64  `json:"expireIn"`
}

type dingtalkMeResponse struct {
	Nick      string `json:"nick"`
	AvatarURL string `json:"avatarUrl"`
	OpenID    string `json:"openId"`
	UnionID   string `json:"unionId"`
}

type dingtalkAppAccessTokenResponse struct {
	AccessToken string `json:"accessToken"`
	ExpiresIn   int64  `json:"expiresIn"`
}

type dingtalkUseridByUnionidResponse struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
	Result  struct {
		UserID string `json:"userid"`
	} `json:"result"`
}

type dingtalkUserDetailResponse struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
	Result  struct {
		OrgEmail string `json:"org_email"`
	} `json:"result"`
}

var dingtalkHTTPClient = &http.Client{Timeout: 15 * time.Second}

func dingtalkPostJSON(ctx context.Context, url string, body any, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := dingtalkHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("dingtalk %s returned %d: %s", url, resp.StatusCode, string(data))
	}
	return json.Unmarshal(data, out)
}

// resolveDingTalkEmail returns the member's enterprise mailbox (org_email,
// 企业邮箱) from the app's own org via the server-side contact API:
// app token -> unionId -> userid -> user detail. The member-profile personal
// email field is deliberately not read — the login identity is the
// admin-assigned enterprise address only, and an empty org_email means no
// login. The field requires the sensitive personal-info permission; without
// it v2/user/get omits it entirely and this returns "".
func resolveDingTalkEmail(ctx context.Context, clientID, clientSecret, unionID string) (string, error) {
	var appToken dingtalkAppAccessTokenResponse
	if err := dingtalkPostJSON(ctx, "https://api.dingtalk.com/v1.0/oauth2/accessToken", map[string]string{
		"appKey":    clientID,
		"appSecret": clientSecret,
	}, &appToken); err != nil {
		return "", fmt.Errorf("app access token: %w", err)
	}
	if appToken.AccessToken == "" {
		return "", errors.New("dingtalk returned empty app access token")
	}

	var byUnionID dingtalkUseridByUnionidResponse
	if err := dingtalkPostJSON(ctx, "https://oapi.dingtalk.com/topapi/user/getbyunionid?access_token="+url.QueryEscape(appToken.AccessToken), map[string]string{
		"unionid": unionID,
	}, &byUnionID); err != nil {
		return "", fmt.Errorf("getbyunionid: %w", err)
	}
	if byUnionID.ErrCode != 0 || byUnionID.Result.UserID == "" {
		return "", fmt.Errorf("getbyunionid failed: errcode=%d errmsg=%s", byUnionID.ErrCode, byUnionID.ErrMsg)
	}

	var detail dingtalkUserDetailResponse
	if err := dingtalkPostJSON(ctx, "https://oapi.dingtalk.com/topapi/v2/user/get?access_token="+url.QueryEscape(appToken.AccessToken), map[string]string{
		"userid": byUnionID.Result.UserID,
	}, &detail); err != nil {
		return "", fmt.Errorf("v2/user/get: %w", err)
	}
	if detail.ErrCode != 0 {
		return "", fmt.Errorf("v2/user/get failed: errcode=%d errmsg=%s", detail.ErrCode, detail.ErrMsg)
	}

	return strings.TrimSpace(detail.Result.OrgEmail), nil
}

func (h *Handler) DingTalkLogin(w http.ResponseWriter, r *http.Request) {
	var req DingTalkLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.AuthCode == "" {
		writeError(w, http.StatusBadRequest, "auth_code is required")
		return
	}

	clientID := os.Getenv("DINGTALK_CLIENT_ID")
	clientSecret := os.Getenv("DINGTALK_CLIENT_SECRET")
	if clientID == "" || clientSecret == "" {
		writeError(w, http.StatusServiceUnavailable, "DingTalk login is not configured")
		return
	}

	// Exchange authCode for a user access token.
	var userToken dingtalkUserAccessTokenResponse
	if err := dingtalkPostJSON(r.Context(), "https://api.dingtalk.com/v1.0/oauth2/userAccessToken", map[string]string{
		"clientId":     clientID,
		"clientSecret": clientSecret,
		"code":         req.AuthCode,
		"grantType":    "authorization_code",
	}, &userToken); err != nil {
		slog.Error("dingtalk oauth token exchange failed", "error", err)
		writeError(w, http.StatusBadGateway, "failed to exchange code with DingTalk")
		return
	}
	if userToken.AccessToken == "" {
		slog.Error("dingtalk oauth token exchange returned empty token")
		writeError(w, http.StatusBadRequest, "failed to exchange code with DingTalk")
		return
	}

	// Fetch the authenticated user's identity via /contact/users/me.
	meReq, err := http.NewRequestWithContext(r.Context(), http.MethodGet, "https://api.dingtalk.com/v1.0/contact/users/me", nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// DingTalk's new-API auth header is x-acs-dingtalk-access-token, not
	// Authorization: Bearer (the API answers 400 MissingParameter otherwise).
	meReq.Header.Set("x-acs-dingtalk-access-token", userToken.AccessToken)
	meResp, err := dingtalkHTTPClient.Do(meReq)
	if err != nil {
		slog.Error("dingtalk userinfo fetch failed", "error", err)
		writeError(w, http.StatusBadGateway, "failed to fetch user info from DingTalk")
		return
	}
	defer meResp.Body.Close()
	meBody, err := io.ReadAll(meResp.Body)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to read DingTalk user info")
		return
	}
	if meResp.StatusCode != http.StatusOK {
		// DingTalk answers permission/scope problems here with a JSON error
		// body — surface it verbatim; it names the exact missing permission.
		slog.Error("dingtalk users/me failed", "status", meResp.StatusCode, "body", string(meBody))
		writeError(w, http.StatusBadGateway, "failed to fetch user info from DingTalk")
		return
	}
	var me dingtalkMeResponse
	if err := json.Unmarshal(meBody, &me); err != nil {
		writeError(w, http.StatusBadGateway, "failed to parse DingTalk user info")
		return
	}
	// Field presence only (never values) — pinning down which identity fields
	// DingTalk actually returned is the first diagnostic when scope/permission
	// gating hides them.
	slog.Info("dingtalk users/me identity fields",
		"has_union_id", me.UnionID != "",
		"has_open_id", me.OpenID != "",
		"has_nick", me.Nick != "",
		"has_avatar", me.AvatarURL != "",
		"http_status", meResp.StatusCode)
	if me.UnionID == "" {
		writeError(w, http.StatusBadRequest, "DingTalk account has no unionId")
		return
	}

	// The login email is the enterprise mailbox on the app-org member record
	// — the single source (see header). No record, no permission, or no
	// org_email means no DingTalk login.
	email, err := resolveDingTalkEmail(r.Context(), clientID, clientSecret, me.UnionID)
	if err != nil {
		slog.Error("dingtalk org email resolution failed", "error", err, "union_id", me.UnionID)
		writeError(w, http.StatusForbidden, "DingTalk login is unavailable (user is not a member of the app's organization, or the app lacks the contact permissions)")
		return
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		writeError(w, http.StatusForbidden, "DingTalk login is unavailable: the app's organization has no enterprise email on record for this user")
		return
	}

	if auth.IsTemporarilyDisabledUserEmail(email) {
		writeError(w, http.StatusForbidden, auth.TemporarilyDisabledUserError)
		return
	}

	user, isNew, err := h.findOrCreateUser(r.Context(), email)
	if err != nil {
		if errors.Is(err, auth.ErrTemporarilyDisabledUser) {
			writeError(w, http.StatusForbidden, auth.TemporarilyDisabledUserError)
			return
		}
		var signupErr SignupError
		if errors.As(err, &signupErr) {
			writeError(w, http.StatusForbidden, signupErr.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create user")
		return
	}
	if isNew {
		evt := analytics.Signup(uuidToString(user.ID), user.Email, signupSourceFromRequest(r))
		evt.Properties["auth_method"] = "dingtalk"
		obsmetrics.RecordEvent(h.Analytics, h.Metrics, evt)
	}

	// Update name and avatar from the DingTalk profile if the user was just
	// created (default name is the email prefix) or has no avatar yet.
	needsUpdate := false
	newName := user.Name
	newAvatar := user.AvatarUrl
	if me.Nick != "" && user.Name == strings.Split(email, "@")[0] {
		newName = me.Nick
		needsUpdate = true
	}
	if me.AvatarURL != "" && !user.AvatarUrl.Valid {
		newAvatar = pgtype.Text{String: me.AvatarURL, Valid: true}
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
		if errors.Is(err, auth.ErrTemporarilyDisabledUser) {
			writeError(w, http.StatusForbidden, auth.TemporarilyDisabledUserError)
			return
		}
		slog.Warn("dingtalk login failed", append(logger.RequestAttrs(r), "error", err, "email", email)...)
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

	slog.Info("user logged in via dingtalk", append(logger.RequestAttrs(r), "user_id", uuidToString(user.ID), "email", user.Email)...)
	writeJSON(w, http.StatusOK, LoginResponse{
		Token: tokenString,
		User:  h.userToResponse(user),
	})
}
