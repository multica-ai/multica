package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const feishuProvider = "feishu"

var feishuEmailLocalPartSanitizer = regexp.MustCompile(`[^a-z0-9._+-]+`)

type FeishuLoginRequest struct {
	Code        string `json:"code"`
	RedirectURI string `json:"redirect_uri"`
}

type feishuTokenRequest struct {
	GrantType   string `json:"grant_type"`
	Code        string `json:"code"`
	AppID       string `json:"app_id"`
	AppSecret   string `json:"app_secret"`
	RedirectURI string `json:"redirect_uri,omitempty"`
}

type feishuTokenResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
	} `json:"data"`
}

type feishuUserInfoResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		OpenID    string `json:"open_id"`
		UnionID   string `json:"union_id"`
		TenantKey string `json:"tenant_key"`
		Email     string `json:"email"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
	} `json:"data"`
}

func feishuRedirectURI(reqRedirectURI string) string {
	if strings.TrimSpace(reqRedirectURI) != "" {
		return strings.TrimSpace(reqRedirectURI)
	}
	return strings.TrimSpace(os.Getenv("FEISHU_REDIRECT_URI"))
}

func feishuPlaceholderEmail(openID string) string {
	sanitized := strings.ToLower(strings.TrimSpace(openID))
	sanitized = feishuEmailLocalPartSanitizer.ReplaceAllString(sanitized, "-")
	sanitized = strings.Trim(sanitized, ".-+")
	if sanitized == "" {
		sanitized = "user"
	}
	return fmt.Sprintf("feishu+%s@users.multica.local", sanitized)
}

func (h *Handler) FeishuLogin(w http.ResponseWriter, r *http.Request) {
	var req FeishuLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if strings.TrimSpace(req.Code) == "" {
		writeError(w, http.StatusBadRequest, "code is required")
		return
	}

	appID := strings.TrimSpace(os.Getenv("FEISHU_APP_ID"))
	appSecret := strings.TrimSpace(os.Getenv("FEISHU_APP_SECRET"))
	if appID == "" || appSecret == "" {
		writeError(w, http.StatusServiceUnavailable, "Feishu login is not configured")
		return
	}

	redirectURI := feishuRedirectURI(req.RedirectURI)
	accessToken, err := exchangeFeishuCode(r, req.Code, redirectURI, appID, appSecret)
	if err != nil {
		slog.Error("feishu oauth token exchange failed", "error", err)
		writeError(w, http.StatusBadGateway, "failed to exchange code with Feishu")
		return
	}

	profile, rawProfile, err := fetchFeishuUserInfo(r, accessToken)
	if err != nil {
		slog.Error("feishu userinfo fetch failed", "error", err)
		writeError(w, http.StatusBadGateway, "failed to fetch user info from Feishu")
		return
	}

	if strings.TrimSpace(profile.Data.OpenID) == "" {
		writeError(w, http.StatusBadRequest, "Feishu account has no open_id")
		return
	}

	user, err := h.resolveFeishuUser(r, profile, rawProfile)
	if err != nil {
		slog.Warn("feishu login failed", append(logger.RequestAttrs(r), "error", err, "open_id", profile.Data.OpenID, "email", profile.Data.Email)...)
		switch typed := err.(type) {
		case accountConflictError:
			writeError(w, http.StatusConflict, typed.Error())
		case missingEmailError:
			writeError(w, http.StatusBadRequest, typed.Error())
		default:
			writeError(w, http.StatusInternalServerError, "failed to create or link user")
		}
		return
	}

	tokenString, err := h.issueJWT(user)
	if err != nil {
		slog.Warn("feishu login failed", append(logger.RequestAttrs(r), "error", err, "open_id", profile.Data.OpenID, "email", profile.Data.Email)...)
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

	slog.Info("user logged in via feishu", append(logger.RequestAttrs(r), "user_id", uuidToString(user.ID), "open_id", profile.Data.OpenID, "email", user.Email)...)
	writeJSON(w, http.StatusOK, LoginResponse{
		Token: tokenString,
		User:  userToResponse(user),
	})
}

func exchangeFeishuCode(r *http.Request, code, redirectURI, appID, appSecret string) (string, error) {
	payload, err := json.Marshal(feishuTokenRequest{
		GrantType:   "authorization_code",
		Code:        code,
		AppID:       appID,
		AppSecret:   appSecret,
		RedirectURI: redirectURI,
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, "https://open.feishu.cn/open-apis/authen/v1/access_token", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("feishu token exchange status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed feishuTokenResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	if parsed.Code != 0 {
		return "", fmt.Errorf("feishu token exchange failed: code=%d msg=%s", parsed.Code, strings.TrimSpace(parsed.Msg))
	}
	if strings.TrimSpace(parsed.Data.AccessToken) == "" {
		return "", fmt.Errorf("feishu token exchange returned empty access token")
	}

	return parsed.Data.AccessToken, nil
}

func fetchFeishuUserInfo(r *http.Request, accessToken string) (feishuUserInfoResponse, []byte, error) {
	var parsed feishuUserInfoResponse

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, "https://open.feishu.cn/open-apis/authen/v1/user_info", nil)
	if err != nil {
		return parsed, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return parsed, nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return parsed, nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return parsed, nil, fmt.Errorf("feishu userinfo status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return parsed, nil, err
	}
	if parsed.Code != 0 {
		return parsed, nil, fmt.Errorf("feishu userinfo failed: code=%d msg=%s", parsed.Code, strings.TrimSpace(parsed.Msg))
	}

	return parsed, body, nil
}

func (h *Handler) resolveFeishuUser(r *http.Request, profile feishuUserInfoResponse, rawProfile []byte) (db.User, error) {
	identity, err := h.Queries.GetExternalIdentityByProvider(r.Context(), db.GetExternalIdentityByProviderParams{
		Provider:       feishuProvider,
		ProviderUserID: strings.TrimSpace(profile.Data.OpenID),
	})
	if err == nil {
		_, _ = h.Queries.UpdateExternalIdentity(r.Context(), db.UpdateExternalIdentityParams{
			ID:         identity.ID,
			UnionID:    strToText(strings.TrimSpace(profile.Data.UnionID)),
			TenantKey:  strToText(strings.TrimSpace(profile.Data.TenantKey)),
			Email:      strToText(strings.ToLower(strings.TrimSpace(profile.Data.Email))),
			Name:       strToText(strings.TrimSpace(profile.Data.Name)),
			AvatarUrl:  strToText(strings.TrimSpace(profile.Data.AvatarURL)),
			RawProfile: rawProfile,
		})

		user, userErr := h.Queries.GetUser(r.Context(), identity.UserID)
		if userErr != nil {
			return db.User{}, userErr
		}
		return h.refreshExternalProfile(r, user, strings.TrimSpace(profile.Data.Name), strings.TrimSpace(profile.Data.AvatarURL))
	}
	if !isNotFound(err) {
		return db.User{}, err
	}

	email := strings.ToLower(strings.TrimSpace(profile.Data.Email))
	if email == "" {
		email = feishuPlaceholderEmail(profile.Data.OpenID)
	}

	if existing, existingErr := h.Queries.GetUserByEmail(r.Context(), email); existingErr == nil {
		return db.User{}, accountConflictError(existing.Email)
	} else if !isNotFound(existingErr) {
		return db.User{}, existingErr
	}

	name := strings.TrimSpace(profile.Data.Name)
	if name == "" {
		if at := strings.Index(email, "@"); at > 0 {
			name = email[:at]
		} else {
			name = email
		}
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		return db.User{}, err
	}
	defer tx.Rollback(r.Context())

	qtx := h.Queries.WithTx(tx)

	user, err := qtx.CreateUser(r.Context(), db.CreateUserParams{
		Name:      name,
		Email:     email,
		AvatarUrl: strToText(strings.TrimSpace(profile.Data.AvatarURL)),
	})
	if err != nil {
		if isUniqueViolation(err) {
			identity, identityErr := h.Queries.GetExternalIdentityByProvider(r.Context(), db.GetExternalIdentityByProviderParams{
				Provider:       feishuProvider,
				ProviderUserID: strings.TrimSpace(profile.Data.OpenID),
			})
			if identityErr == nil {
				if existingUser, userErr := h.Queries.GetUser(r.Context(), identity.UserID); userErr == nil {
					return h.refreshExternalProfile(r, existingUser, strings.TrimSpace(profile.Data.Name), strings.TrimSpace(profile.Data.AvatarURL))
				}
			}
			if existing, existingErr := h.Queries.GetUserByEmail(r.Context(), email); existingErr == nil {
				return db.User{}, accountConflictError(existing.Email)
			}
		}
		return db.User{}, err
	}

	_, err = qtx.CreateExternalIdentity(r.Context(), db.CreateExternalIdentityParams{
		UserID:         user.ID,
		Provider:       feishuProvider,
		ProviderUserID: strings.TrimSpace(profile.Data.OpenID),
		UnionID:        strToText(strings.TrimSpace(profile.Data.UnionID)),
		TenantKey:      strToText(strings.TrimSpace(profile.Data.TenantKey)),
		Email:          strToText(email),
		Name:           strToText(strings.TrimSpace(profile.Data.Name)),
		AvatarUrl:      strToText(strings.TrimSpace(profile.Data.AvatarURL)),
		RawProfile:     rawProfile,
	})
	if err != nil {
		if isUniqueViolation(err) {
			identity, identityErr := h.Queries.GetExternalIdentityByProvider(r.Context(), db.GetExternalIdentityByProviderParams{
				Provider:       feishuProvider,
				ProviderUserID: strings.TrimSpace(profile.Data.OpenID),
			})
			if identityErr == nil {
				if existingUser, userErr := h.Queries.GetUser(r.Context(), identity.UserID); userErr == nil {
					return h.refreshExternalProfile(r, existingUser, strings.TrimSpace(profile.Data.Name), strings.TrimSpace(profile.Data.AvatarURL))
				}
			}
		}
		return db.User{}, err
	}

	if err := tx.Commit(r.Context()); err != nil {
		return db.User{}, err
	}

	return user, nil
}

func (h *Handler) refreshExternalProfile(r *http.Request, user db.User, name, avatarURL string) (db.User, error) {
	needsUpdate := false
	newName := user.Name
	newAvatar := user.AvatarUrl

	trimmedName := strings.TrimSpace(name)
	if trimmedName != "" && user.Name == strings.Split(user.Email, "@")[0] {
		newName = trimmedName
		needsUpdate = true
	}
	trimmedAvatar := strings.TrimSpace(avatarURL)
	if trimmedAvatar != "" && !user.AvatarUrl.Valid {
		newAvatar = strToText(trimmedAvatar)
		needsUpdate = true
	}
	if !needsUpdate {
		return user, nil
	}

	updated, err := h.Queries.UpdateUser(r.Context(), db.UpdateUserParams{
		ID:        user.ID,
		Name:      newName,
		AvatarUrl: newAvatar,
	})
	if err != nil {
		return user, nil
	}
	return updated, nil
}

type missingEmailError string

func (e missingEmailError) Error() string {
	return string(e)
}

type accountConflictError string

func (e accountConflictError) Error() string {
	return "existing account with email " + string(e) + " must be linked manually before Feishu login can be used"
}
