package lark

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	ProviderName        = "lark"
	defaultAuthorizeURL = "https://open.feishu.cn/open-apis/authen/v1/index"
	defaultTokenURL     = "https://open.feishu.cn/open-apis/authen/v2/oauth/token"
	defaultUserInfoURL  = "https://open.feishu.cn/open-apis/authen/v1/user_info"
)

type Config struct {
	Enabled         bool
	AppID           string
	AppSecret       string
	AuthorizeURL    string
	TokenURL        string
	UserInfoURL     string
	DefaultTenant   string
	TenantAllowlist []string
}

type Profile struct {
	ExternalUserID string
	OpenID         string
	UnionID        string
	TenantKey      string
	Email          string
	Name           string
	AvatarURL      string
	Raw            json.RawMessage
}

type OAuthClient struct {
	cfg        Config
	httpClient *http.Client
}

func ConfigFromEnv() Config {
	return Config{
		Enabled:         envBool("LARK_AUTH_ENABLED"),
		AppID:           strings.TrimSpace(os.Getenv("LARK_APP_ID")),
		AppSecret:       firstNonEmpty(os.Getenv("LARK_APP_SECRET"), os.Getenv("LARK_APP_SECRET_REF")),
		AuthorizeURL:    firstNonEmpty(os.Getenv("LARK_AUTHORIZE_URL"), defaultAuthorizeURL),
		TokenURL:        firstNonEmpty(os.Getenv("LARK_TOKEN_URL"), defaultTokenURL),
		UserInfoURL:     firstNonEmpty(os.Getenv("LARK_USER_INFO_URL"), defaultUserInfoURL),
		DefaultTenant:   strings.TrimSpace(os.Getenv("LARK_TENANT_KEY")),
		TenantAllowlist: splitAndTrim(os.Getenv("LARK_TENANT_ALLOWLIST")),
	}
}

func PublicConfigFromEnv() (enabled bool, appID string, authorizeURL string) {
	cfg := ConfigFromEnv()
	return cfg.Enabled, cfg.AppID, cfg.AuthorizeURL
}

func NewOAuthClientFromEnv() *OAuthClient {
	return NewOAuthClient(ConfigFromEnv(), nil)
}

func NewOAuthClient(cfg Config, httpClient *http.Client) *OAuthClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	if cfg.AuthorizeURL == "" {
		cfg.AuthorizeURL = defaultAuthorizeURL
	}
	if cfg.TokenURL == "" {
		cfg.TokenURL = defaultTokenURL
	}
	if cfg.UserInfoURL == "" {
		cfg.UserInfoURL = defaultUserInfoURL
	}
	return &OAuthClient{cfg: cfg, httpClient: httpClient}
}

func (c *OAuthClient) ExchangeCode(ctx context.Context, code, redirectURI string) (Profile, error) {
	if !c.cfg.Enabled {
		return Profile{}, errors.New("Lark login is not enabled")
	}
	if c.cfg.AppID == "" || c.cfg.AppSecret == "" {
		return Profile{}, errors.New("Lark login is not configured")
	}
	if strings.TrimSpace(code) == "" {
		return Profile{}, errors.New("code is required")
	}

	token, err := c.exchangeToken(ctx, code, redirectURI)
	if err != nil {
		return Profile{}, err
	}
	profile := profileFromMap(token.fields, token.raw)
	if profile.OpenID == "" || profile.Name == "" || profile.TenantKey == "" || profile.Email == "" {
		userInfo, err := c.fetchUserInfo(ctx, token.accessToken)
		if err != nil {
			return Profile{}, err
		}
		profile = mergeProfile(profile, profileFromMap(userInfo.fields, userInfo.raw))
	}
	if profile.TenantKey == "" {
		profile.TenantKey = c.cfg.DefaultTenant
	}
	if profile.OpenID == "" {
		return Profile{}, errors.New("Lark account has no open_id")
	}
	if !tenantAllowed(profile.TenantKey, c.cfg.TenantAllowlist) {
		return Profile{}, errors.New("Lark tenant is not allowed")
	}
	return profile, nil
}

type oauthPayload struct {
	GrantType    string `json:"grant_type"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	Code         string `json:"code"`
	RedirectURI  string `json:"redirect_uri,omitempty"`
}

type decodedPayload struct {
	accessToken string
	fields      map[string]any
	raw         json.RawMessage
}

func (c *OAuthClient) exchangeToken(ctx context.Context, code, redirectURI string) (decodedPayload, error) {
	body, _ := json.Marshal(oauthPayload{
		GrantType:    "authorization_code",
		ClientID:     c.cfg.AppID,
		ClientSecret: c.cfg.AppSecret,
		Code:         code,
		RedirectURI:  redirectURI,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.TokenURL, bytes.NewReader(body))
	if err != nil {
		return decodedPayload{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.decodeLarkResponse(req, "access_token")
}

func (c *OAuthClient) fetchUserInfo(ctx context.Context, accessToken string) (decodedPayload, error) {
	if accessToken == "" {
		return decodedPayload{}, errors.New("Lark token response has no access_token")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.UserInfoURL, nil)
	if err != nil {
		return decodedPayload{}, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	return c.decodeLarkResponse(req, "")
}

func (c *OAuthClient) decodeLarkResponse(req *http.Request, tokenField string) (decodedPayload, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return decodedPayload{}, fmt.Errorf("Lark request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return decodedPayload{}, fmt.Errorf("failed to read Lark response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return decodedPayload{}, fmt.Errorf("Lark request returned %d", resp.StatusCode)
	}

	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return decodedPayload{}, fmt.Errorf("failed to parse Lark response: %w", err)
	}
	if code, ok := root["code"].(float64); ok && code != 0 {
		return decodedPayload{}, fmt.Errorf("Lark request rejected: %s", stringField(root, "msg"))
	}

	fields := root
	if data, ok := root["data"].(map[string]any); ok {
		fields = data
	}
	accessToken := stringField(fields, tokenField)
	return decodedPayload{accessToken: accessToken, fields: fields, raw: append(json.RawMessage(nil), body...)}, nil
}

func profileFromMap(fields map[string]any, raw json.RawMessage) Profile {
	return Profile{
		ExternalUserID: firstNonEmpty(stringField(fields, "user_id"), stringField(fields, "userId")),
		OpenID:         firstNonEmpty(stringField(fields, "open_id"), stringField(fields, "openId")),
		UnionID:        firstNonEmpty(stringField(fields, "union_id"), stringField(fields, "unionId")),
		TenantKey:      firstNonEmpty(stringField(fields, "tenant_key"), stringField(fields, "tenantKey")),
		Email:          strings.ToLower(strings.TrimSpace(stringField(fields, "email"))),
		Name:           firstNonEmpty(stringField(fields, "name"), stringField(fields, "en_name")),
		AvatarURL:      firstNonEmpty(stringField(fields, "avatar_url"), stringField(fields, "avatar_thumb"), stringField(fields, "avatar_middle"), stringField(fields, "avatar_big")),
		Raw:            raw,
	}
}

func mergeProfile(primary, fallback Profile) Profile {
	if primary.ExternalUserID == "" {
		primary.ExternalUserID = fallback.ExternalUserID
	}
	if primary.OpenID == "" {
		primary.OpenID = fallback.OpenID
	}
	if primary.UnionID == "" {
		primary.UnionID = fallback.UnionID
	}
	if primary.TenantKey == "" {
		primary.TenantKey = fallback.TenantKey
	}
	if primary.Email == "" {
		primary.Email = fallback.Email
	}
	if primary.Name == "" {
		primary.Name = fallback.Name
	}
	if primary.AvatarURL == "" {
		primary.AvatarURL = fallback.AvatarURL
	}
	if len(fallback.Raw) > 0 {
		primary.Raw = fallback.Raw
	}
	return primary
}

func tenantAllowed(tenant string, allowlist []string) bool {
	if len(allowlist) == 0 {
		return true
	}
	for _, allowed := range allowlist {
		if strings.EqualFold(tenant, allowed) {
			return true
		}
	}
	return false
}

func envBool(key string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	return v == "1" || v == "true" || v == "yes"
}

func splitAndTrim(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if v := strings.TrimSpace(part); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func stringField(fields map[string]any, key string) string {
	if fields == nil || key == "" {
		return ""
	}
	if v, ok := fields[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}
