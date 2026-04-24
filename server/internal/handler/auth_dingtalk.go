package handler

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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

type DingTalkLoginRequest struct {
	Code        string `json:"code"`
	RedirectURI string `json:"redirect_uri"`
}

type dingtalkTokenRequest struct {
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
	Code         string `json:"code"`
	GrantType    string `json:"grantType"`
}

type dingtalkTokenResponse struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpireIn     int64  `json:"expireIn"`
	CorpID       string `json:"corpId"`
}

type dingtalkUserInfo struct {
	UnionID   string `json:"unionId"`
	OpenID    string `json:"openId"`
	Nick      string `json:"nick"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatarUrl"`
	Mobile    string `json:"mobile"`
	Email     string `json:"email"`
}

// dingtalkBaseURL is the DingTalk API base URL. Overridden in tests.
var dingtalkBaseURL = "https://api.dingtalk.com"

func (h *Handler) DingTalkLogin(w http.ResponseWriter, r *http.Request) {
	var req DingTalkLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Code == "" {
		writeError(w, http.StatusBadRequest, "code is required")
		return
	}

	clientID := os.Getenv("DINGTALK_CLIENT_ID")
	clientSecret := os.Getenv("DINGTALK_CLIENT_SECRET")
	if clientID == "" || clientSecret == "" {
		writeError(w, http.StatusServiceUnavailable, "DingTalk login is not configured")
		return
	}

	// Exchange authorization code for tokens.
	tokenReqBody, _ := json.Marshal(dingtalkTokenRequest{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Code:         req.Code,
		GrantType:    "authorization_code",
	})

	tokenResp, err := http.Post(
		dingtalkBaseURL+"/v1.0/oauth2/userAccessToken",
		"application/json",
		bytes.NewReader(tokenReqBody),
	)
	if err != nil {
		slog.Error("dingtalk oauth token exchange failed", "error", err)
		writeError(w, http.StatusBadGateway, "failed to exchange code with DingTalk")
		return
	}
	defer tokenResp.Body.Close()

	tokenBody, err := io.ReadAll(tokenResp.Body)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to read DingTalk token response")
		return
	}

	if tokenResp.StatusCode != http.StatusOK {
		slog.Error("dingtalk oauth token exchange returned error", "status", tokenResp.StatusCode, "body", string(tokenBody))
		writeError(w, http.StatusBadRequest, "failed to exchange code with DingTalk")
		return
	}

	var dtToken dingtalkTokenResponse
	if err := json.Unmarshal(tokenBody, &dtToken); err != nil {
		writeError(w, http.StatusBadGateway, "failed to parse DingTalk token response")
		return
	}

	if dtToken.AccessToken == "" {
		slog.Error("dingtalk token response missing access token", "body", string(tokenBody))
		writeError(w, http.StatusBadGateway, "DingTalk returned empty access token")
		return
	}

	// Fetch user info from DingTalk.
	userInfoReq, err := http.NewRequestWithContext(r.Context(), http.MethodGet, dingtalkBaseURL+"/v1.0/contact/users/me", nil)
	if err != nil {
		slog.Error("failed to create dingtalk userinfo request", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	userInfoReq.Header.Set("x-acs-dingtalk-access-token", dtToken.AccessToken)

	userInfoResp, err := http.DefaultClient.Do(userInfoReq)
	if err != nil {
		slog.Error("dingtalk userinfo fetch failed", "error", err)
		writeError(w, http.StatusBadGateway, "failed to fetch user info from DingTalk")
		return
	}
	defer userInfoResp.Body.Close()

	if userInfoResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(userInfoResp.Body)
		slog.Error("dingtalk userinfo returned error", "status", userInfoResp.StatusCode, "body", string(body))
		writeError(w, http.StatusBadGateway, "failed to fetch user info from DingTalk")
		return
	}

	var dtUser dingtalkUserInfo
	if err := json.NewDecoder(userInfoResp.Body).Decode(&dtUser); err != nil {
		writeError(w, http.StatusBadGateway, "failed to parse DingTalk user info")
		return
	}

	if dtUser.UnionID == "" {
		writeError(w, http.StatusBadRequest, "DingTalk account has no unionId")
		return
	}

	// User matching strategy:
	// 1. Check external_account_binding by (provider=dingtalk, external_user_id=unionId)
	// 2. If not found, try email matching
	// 3. If still not found, create new user
	var user db.User
	var isNew bool

	binding, err := h.Queries.GetExternalAccountBindingByProviderAndExternalID(r.Context(), db.GetExternalAccountBindingByProviderAndExternalIDParams{
		Provider:       "dingtalk",
		ExternalUserID: dtUser.UnionID,
	})
	if err == nil {
		// Found existing binding — load the user
		user, err = h.Queries.GetUser(r.Context(), binding.UserID)
		if err != nil {
			slog.Error("dingtalk login: bound user not found", "user_id", binding.UserID, "error", err)
			writeError(w, http.StatusInternalServerError, "bound user account not found")
			return
		}
	} else if isNotFound(err) {
		// No binding — try to match by email
		email := strings.ToLower(strings.TrimSpace(dtUser.Email))
		if email == "" {
			// No email from DingTalk — use a synthetic email
			email = fmt.Sprintf("%s@dingtalk.local", dtUser.UnionID)
		}

		user, isNew, err = h.findOrCreateUser(r.Context(), email)
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
			evt := analytics.Signup(uuidToString(user.ID), user.Email, signupSourceFromRequest(r))
			evt.Properties["auth_method"] = "dingtalk"
			h.Analytics.Capture(evt)
		}
	} else {
		slog.Error("dingtalk login: failed to query binding", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to check account binding")
		return
	}

	// Upsert external_account_binding
	displayName := dtUser.Nick
	if displayName == "" {
		displayName = dtUser.Name
	}

	encryptedAccess := encryptToken(dtToken.AccessToken)
	encryptedRefresh := encryptToken(dtToken.RefreshToken)

	var tokenExpires pgtype.Timestamptz
	if dtToken.ExpireIn > 0 {
		tokenExpires = pgtype.Timestamptz{Time: time.Now().Add(time.Duration(dtToken.ExpireIn) * time.Second), Valid: true}
	}

	_, err = h.Queries.UpsertExternalAccountBinding(r.Context(), db.UpsertExternalAccountBindingParams{
		UserID:                user.ID,
		Provider:              "dingtalk",
		ExternalUserID:        dtUser.UnionID,
		DisplayName:           pgtype.Text{String: displayName, Valid: displayName != ""},
		AccessTokenEncrypted:  pgtype.Text{String: encryptedAccess, Valid: encryptedAccess != ""},
		RefreshTokenEncrypted: pgtype.Text{String: encryptedRefresh, Valid: encryptedRefresh != ""},
		TokenExpiresAt:        tokenExpires,
		Metadata:              []byte("{}"),
	})
	if err != nil {
		slog.Error("dingtalk login: failed to upsert binding", "error", err)
		// Non-fatal: login can proceed even if binding upsert fails
	}

	// Update user name/avatar from DingTalk profile (same logic as GoogleLogin)
	needsUpdate := false
	newName := user.Name
	newAvatar := user.AvatarUrl

	dtName := dtUser.Nick
	if dtName == "" {
		dtName = dtUser.Name
	}
	email := strings.ToLower(strings.TrimSpace(user.Email))
	if dtName != "" && user.Name == strings.Split(email, "@")[0] {
		newName = dtName
		needsUpdate = true
	}
	if dtUser.AvatarURL != "" && !user.AvatarUrl.Valid {
		newAvatar = pgtype.Text{String: dtUser.AvatarURL, Valid: true}
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
		slog.Warn("dingtalk login failed", append(logger.RequestAttrs(r), "error", err, "user_id", uuidToString(user.ID))...)
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
		User:  userToResponse(user),
	})
}

// tokenEncryptionKey returns the AES-256 key for encrypting DingTalk tokens.
// Priority: DINGTALK_TOKEN_ENCRYPTION_KEY > JWT_SECRET (first 32 bytes).
func tokenEncryptionKey() []byte {
	if key := os.Getenv("DINGTALK_TOKEN_ENCRYPTION_KEY"); key != "" {
		b, err := hex.DecodeString(key)
		if err == nil && len(b) == 32 {
			return b
		}
	}
	secret := os.Getenv("JWT_SECRET")
	if len(secret) >= 32 {
		return []byte(secret[:32])
	}
	// Pad short secrets to 32 bytes
	padded := make([]byte, 32)
	copy(padded, []byte(secret))
	return padded
}

// encryptToken encrypts a token string using AES-256-GCM.
// Returns hex-encoded ciphertext, or empty string on failure.
func encryptToken(plaintext string) string {
	if plaintext == "" {
		return ""
	}
	key := tokenEncryptionKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		slog.Error("failed to create AES cipher for token encryption", "error", err)
		return ""
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		slog.Error("failed to create GCM for token encryption", "error", err)
		return ""
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		slog.Error("failed to generate nonce for token encryption", "error", err)
		return ""
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return hex.EncodeToString(ciphertext)
}
