package notify

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/auth"
)

const (
	defaultDingTalkAuthURL     = "https://login.dingtalk.com/oauth2/auth"
	defaultDingTalkTokenURL    = "https://api.dingtalk.com/v1.0/oauth2/userAccessToken"
	defaultDingTalkUserInfoURL = "https://api.dingtalk.com/v1.0/contact/users/me"
	defaultDingTalkScope       = "openid corpid Contact.User.Read"
	dingTalkStatePrefix        = "dingtalk"
)

var ErrDingTalkNotConfigured = errors.New("dingtalk is not configured")

type DingTalkConfig struct {
	ClientID     string
	ClientSecret string
	Scope        string
	AuthURL      string
	TokenURL     string
	UserInfoURL  string
	HTTPClient   *http.Client
}

type DingTalkBindingState struct {
	UserID   string `json:"user_id"`
	NextPath string `json:"next_path,omitempty"`
	IssuedAt int64  `json:"issued_at"`
}

type DingTalkToken struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int64
	CorpID       string
	OpenID       string
}

type DingTalkUserProfile struct {
	UnionID   string
	OpenID    string
	Name      string
	Nick      string
	AvatarURL string
	Mobile    string
}

func LoadDingTalkConfig() (DingTalkConfig, error) {
	cfg := DingTalkConfig{
		ClientID:     strings.TrimSpace(os.Getenv("DINGTALK_CLIENT_ID")),
		ClientSecret: strings.TrimSpace(os.Getenv("DINGTALK_CLIENT_SECRET")),
		Scope:        strings.TrimSpace(os.Getenv("DINGTALK_OAUTH_SCOPE")),
		AuthURL:      strings.TrimSpace(os.Getenv("DINGTALK_AUTH_URL")),
		TokenURL:     strings.TrimSpace(os.Getenv("DINGTALK_TOKEN_URL")),
		UserInfoURL:  strings.TrimSpace(os.Getenv("DINGTALK_USERINFO_URL")),
		HTTPClient:   &http.Client{Timeout: 10 * time.Second},
	}

	if cfg.Scope == "" {
		cfg.Scope = defaultDingTalkScope
	}
	if cfg.AuthURL == "" {
		cfg.AuthURL = defaultDingTalkAuthURL
	}
	if cfg.TokenURL == "" {
		cfg.TokenURL = defaultDingTalkTokenURL
	}
	if cfg.UserInfoURL == "" {
		cfg.UserInfoURL = defaultDingTalkUserInfoURL
	}
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return DingTalkConfig{}, ErrDingTalkNotConfigured
	}

	return cfg, nil
}

func (c DingTalkConfig) RedirectURL() string {
	return strings.TrimRight(AppURL(), "/") + "/auth/callback"
}

func (c DingTalkConfig) AuthorizationURL(state string) string {
	values := url.Values{}
	values.Set("client_id", c.ClientID)
	values.Set("redirect_uri", c.RedirectURL())
	values.Set("state", state)
	values.Set("response_type", "code")
	values.Set("prompt", "consent")
	values.Set("scope", c.Scope)
	return c.AuthURL + "?" + values.Encode()
}

func IsDingTalkState(state string) bool {
	return strings.HasPrefix(state, dingTalkStatePrefix+".")
}

func BuildDingTalkState(payload DingTalkBindingState) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	encoded := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, auth.JWTSecret())
	mac.Write([]byte(encoded))
	signature := hex.EncodeToString(mac.Sum(nil))
	return dingTalkStatePrefix + "." + encoded + "." + signature, nil
}

func ParseDingTalkState(state string) (DingTalkBindingState, error) {
	parts := strings.Split(state, ".")
	if len(parts) != 3 || parts[0] != dingTalkStatePrefix {
		return DingTalkBindingState{}, errors.New("invalid dingtalk state format")
	}

	mac := hmac.New(sha256.New, auth.JWTSecret())
	mac.Write([]byte(parts[1]))
	expected := mac.Sum(nil)
	got, err := hex.DecodeString(parts[2])
	if err != nil || !hmac.Equal(expected, got) {
		return DingTalkBindingState{}, errors.New("invalid dingtalk state signature")
	}

	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return DingTalkBindingState{}, errors.New("invalid dingtalk state payload")
	}

	var payload DingTalkBindingState
	if err := json.Unmarshal(raw, &payload); err != nil {
		return DingTalkBindingState{}, errors.New("invalid dingtalk state payload")
	}
	return payload, nil
}

func EncryptToken(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}

	key := sha256.Sum256(encryptionSecret())
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}

	sealed := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	return base64.RawURLEncoding.EncodeToString(append(nonce, sealed...)), nil
}

func DecryptToken(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}

	raw, err := base64.RawURLEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}

	key := sha256.Sum256(encryptionSecret())
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("invalid encrypted token payload")
	}

	nonce := raw[:gcm.NonceSize()]
	payload := raw[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, payload, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func encryptionSecret() []byte {
	for _, key := range []string{"DINGTALK_TOKEN_ENCRYPTION_KEY", "MULTICA_ENCRYPTION_KEY"} {
		if raw := strings.TrimSpace(os.Getenv(key)); raw != "" {
			return []byte(raw)
		}
	}
	return auth.JWTSecret()
}

type dingTalkTokenResponse struct {
	AccessTokenSnake  string `json:"access_token"`
	AccessTokenCamel  string `json:"accessToken"`
	RefreshTokenSnake string `json:"refresh_token"`
	RefreshTokenCamel string `json:"refreshToken"`
	ExpiresInSnake    int64  `json:"expires_in"`
	ExpiresInCamel    int64  `json:"expireIn"`
	CorpIDSnake       string `json:"corp_id"`
	CorpIDCamel       string `json:"corpId"`
	OpenIDSnake       string `json:"open_id"`
	OpenIDCamel       string `json:"openId"`
}

func (c DingTalkConfig) ExchangeCode(ctx context.Context, code string) (DingTalkToken, error) {
	body, err := json.Marshal(map[string]any{
		"clientId":     c.ClientID,
		"clientSecret": c.ClientSecret,
		"code":         code,
		"grantType":    "authorization_code",
	})
	if err != nil {
		return DingTalkToken{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenURL, strings.NewReader(string(body)))
	if err != nil {
		return DingTalkToken{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return DingTalkToken{}, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return DingTalkToken{}, err
	}
	if resp.StatusCode >= 400 {
		return DingTalkToken{}, fmt.Errorf("dingtalk token exchange failed: %s", strings.TrimSpace(string(raw)))
	}

	var decoded dingTalkTokenResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return DingTalkToken{}, err
	}

	token := DingTalkToken{
		AccessToken:  firstNonEmpty(decoded.AccessTokenCamel, decoded.AccessTokenSnake),
		RefreshToken: firstNonEmpty(decoded.RefreshTokenCamel, decoded.RefreshTokenSnake),
		ExpiresIn:    firstNonZero(decoded.ExpiresInCamel, decoded.ExpiresInSnake),
		CorpID:       firstNonEmpty(decoded.CorpIDCamel, decoded.CorpIDSnake),
		OpenID:       firstNonEmpty(decoded.OpenIDCamel, decoded.OpenIDSnake),
	}
	if token.AccessToken == "" {
		return DingTalkToken{}, errors.New("dingtalk token exchange returned empty access token")
	}
	return token, nil
}

type dingTalkUserProfileResponse struct {
	UnionIDSnake   string `json:"union_id"`
	UnionIDCamel   string `json:"unionId"`
	OpenIDSnake    string `json:"open_id"`
	OpenIDCamel    string `json:"openId"`
	Name           string `json:"name"`
	Nick           string `json:"nick"`
	AvatarURLSnake string `json:"avatar_url"`
	AvatarURLCamel string `json:"avatarUrl"`
	Mobile         string `json:"mobile"`
}

func (c DingTalkConfig) GetUserProfile(ctx context.Context, accessToken string) (DingTalkUserProfile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.UserInfoURL, nil)
	if err != nil {
		return DingTalkUserProfile{}, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return DingTalkUserProfile{}, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return DingTalkUserProfile{}, err
	}
	if resp.StatusCode >= 400 {
		return DingTalkUserProfile{}, fmt.Errorf("dingtalk user info fetch failed: %s", strings.TrimSpace(string(raw)))
	}

	var decoded dingTalkUserProfileResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return DingTalkUserProfile{}, err
	}

	profile := DingTalkUserProfile{
		UnionID:   firstNonEmpty(decoded.UnionIDCamel, decoded.UnionIDSnake),
		OpenID:    firstNonEmpty(decoded.OpenIDCamel, decoded.OpenIDSnake),
		Name:      decoded.Name,
		Nick:      decoded.Nick,
		AvatarURL: firstNonEmpty(decoded.AvatarURLCamel, decoded.AvatarURLSnake),
		Mobile:    decoded.Mobile,
	}
	if profile.UnionID == "" && profile.OpenID == "" {
		return DingTalkUserProfile{}, errors.New("dingtalk user info missing user identifiers")
	}
	return profile, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstNonZero(values ...int64) int64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}
