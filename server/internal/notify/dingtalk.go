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
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/auth"
)

const (
	defaultDingTalkAuthURL     = "https://login.dingtalk.com/oauth2/auth"
	defaultDingTalkTokenURL    = "https://api.dingtalk.com/v1.0/oauth2/userAccessToken"
	defaultDingTalkUserInfoURL = "https://api.dingtalk.com/v1.0/contact/users/me"
	defaultDingTalkAppTokenURL = "https://api.dingtalk.com/v1.0/oauth2/{corpId}/token"
	defaultDingTalkMessageURL  = "https://api.dingtalk.com/v1.0/robot/oToMessages/batchSend"
	defaultDingTalkScope       = "openid corpid Contact.User.Read"
	dingTalkStatePrefix        = "dingtalk"
)

var ErrDingTalkNotConfigured = errors.New("dingtalk is not configured")
var ErrDingTalkDeliveryNotConfigured = errors.New("dingtalk delivery is not configured")

var dingTalkAppTokenCache sync.Map

type DingTalkConfig struct {
	ClientID     string
	ClientSecret string
	RobotCode    string
	Scope        string
	AuthURL      string
	TokenURL     string
	UserInfoURL  string
	AppTokenURL  string
	MessageURL   string
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

type DingTalkSendResult struct {
	ProcessQueryKey string
}

type dingTalkCachedAppToken struct {
	AccessToken string
	ExpiresAt   time.Time
}

func LoadDingTalkConfig() (DingTalkConfig, error) {
	cfg := DingTalkConfig{
		ClientID:     strings.TrimSpace(os.Getenv("DINGTALK_CLIENT_ID")),
		ClientSecret: strings.TrimSpace(os.Getenv("DINGTALK_CLIENT_SECRET")),
		RobotCode:    strings.TrimSpace(os.Getenv("DINGTALK_ROBOT_CODE")),
		Scope:        strings.TrimSpace(os.Getenv("DINGTALK_OAUTH_SCOPE")),
		AuthURL:      strings.TrimSpace(os.Getenv("DINGTALK_AUTH_URL")),
		TokenURL:     strings.TrimSpace(os.Getenv("DINGTALK_TOKEN_URL")),
		UserInfoURL:  strings.TrimSpace(os.Getenv("DINGTALK_USERINFO_URL")),
		AppTokenURL:  strings.TrimSpace(os.Getenv("DINGTALK_APP_TOKEN_URL")),
		MessageURL:   strings.TrimSpace(os.Getenv("DINGTALK_MESSAGE_URL")),
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
	if cfg.AppTokenURL == "" {
		cfg.AppTokenURL = defaultDingTalkAppTokenURL
	}
	if cfg.MessageURL == "" {
		cfg.MessageURL = defaultDingTalkMessageURL
	}
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return DingTalkConfig{}, ErrDingTalkNotConfigured
	}

	return cfg, nil
}

func (c DingTalkConfig) ValidateDeliveryConfig() error {
	if strings.TrimSpace(c.RobotCode) == "" {
		return ErrDingTalkDeliveryNotConfigured
	}
	if strings.TrimSpace(c.MessageURL) == "" {
		return ErrDingTalkDeliveryNotConfigured
	}
	return nil
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

type dingTalkAppTokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
}

func (c DingTalkConfig) appTokenURL(corpID string) string {
	return strings.Replace(c.AppTokenURL, "{corpId}", url.PathEscape(corpID), 1)
}

func (c DingTalkConfig) AppAccessToken(ctx context.Context, corpID string) (string, error) {
	corpID = strings.TrimSpace(corpID)
	if corpID == "" {
		return "", errors.New("missing dingtalk corp id")
	}

	if cached, ok := dingTalkAppTokenCache.Load(corpID); ok {
		token := cached.(dingTalkCachedAppToken)
		if token.AccessToken != "" && time.Until(token.ExpiresAt) > time.Minute {
			return token.AccessToken, nil
		}
	}

	body, err := json.Marshal(map[string]any{
		"client_id":     c.ClientID,
		"client_secret": c.ClientSecret,
		"grant_type":    "client_credentials",
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.appTokenURL(corpID), strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		return "", &DingTalkAPIError{
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("dingtalk app token exchange failed: %s", strings.TrimSpace(string(raw))),
		}
	}

	var decoded dingTalkAppTokenResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return "", err
	}
	if strings.TrimSpace(decoded.AccessToken) == "" {
		return "", errors.New("dingtalk app token exchange returned empty access token")
	}

	expiresAt := time.Now().UTC().Add(90 * time.Minute)
	if decoded.ExpiresIn > 0 {
		expiresAt = time.Now().UTC().Add(time.Duration(decoded.ExpiresIn) * time.Second)
	}
	dingTalkAppTokenCache.Store(corpID, dingTalkCachedAppToken{
		AccessToken: decoded.AccessToken,
		ExpiresAt:   expiresAt,
	})
	return decoded.AccessToken, nil
}

type DingTalkAPIError struct {
	StatusCode int
	Message    string
}

func (e *DingTalkAPIError) Error() string {
	return e.Message
}

type dingTalkSendResponse struct {
	ProcessQueryKey string `json:"processQueryKey"`
}

func (c DingTalkConfig) SendTextMessage(ctx context.Context, corpID, unionID, content string) (DingTalkSendResult, error) {
	if err := c.ValidateDeliveryConfig(); err != nil {
		return DingTalkSendResult{}, err
	}
	unionID = strings.TrimSpace(unionID)
	if unionID == "" {
		return DingTalkSendResult{}, errors.New("missing dingtalk union id")
	}

	accessToken, err := c.AppAccessToken(ctx, corpID)
	if err != nil {
		return DingTalkSendResult{}, err
	}

	msgParamRaw, err := json.Marshal(map[string]string{
		"content": strings.TrimSpace(content),
	})
	if err != nil {
		return DingTalkSendResult{}, err
	}

	body, err := json.Marshal(map[string]any{
		"robotCode": c.RobotCode,
		"userIds":   []string{unionID},
		"msgKey":    "sampleText",
		"msgParam":  string(msgParamRaw),
	})
	if err != nil {
		return DingTalkSendResult{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.MessageURL, strings.NewReader(string(body)))
	if err != nil {
		return DingTalkSendResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return DingTalkSendResult{}, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return DingTalkSendResult{}, err
	}
	if resp.StatusCode >= 400 {
		return DingTalkSendResult{}, &DingTalkAPIError{
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("dingtalk send failed: %s", strings.TrimSpace(string(raw))),
		}
	}

	var decoded dingTalkSendResponse
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return DingTalkSendResult{}, err
		}
	}
	return DingTalkSendResult{
		ProcessQueryKey: decoded.ProcessQueryKey,
	}, nil
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
