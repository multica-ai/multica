package service

import (
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
)

const (
	OAuthProviderGoogle      = "google"
	OAuthProviderFeishuLark  = "feishu_lark"
	defaultGoogleAuthorize   = "https://accounts.google.com/o/oauth2/v2/auth"
	defaultGoogleToken       = "https://oauth2.googleapis.com/token"
	defaultGoogleUserInfo    = "https://www.googleapis.com/oauth2/v2/userinfo"
	defaultLarkOpenAPIBase   = "https://open.feishu.cn"
	defaultFeishuAccountBase = "https://accounts.feishu.cn"
	defaultLarkAccountBase   = "https://accounts.larksuite.com"
	defaultLarkProviderLabel = "Feishu/Lark"
)

type OAuthProviderPublicConfig struct {
	ID               string            `json:"id"`
	Label            string            `json:"label"`
	ClientID         string            `json:"client_id"`
	AuthorizationURL string            `json:"authorization_url"`
	Scope            string            `json:"scope,omitempty"`
	PKCE             bool              `json:"pkce,omitempty"`
	ExtraAuthParams  map[string]string `json:"extra_auth_params,omitempty"`
}

type OAuthExchangeRequest struct {
	Code         string
	RedirectURI  string
	CodeVerifier string
}

type OAuthIdentity struct {
	Email     string
	Name      string
	AvatarURL string
}

type OAuthLoginProvider interface {
	ID() string
	PublicConfig() OAuthProviderPublicConfig
	Exchange(ctx context.Context, req OAuthExchangeRequest) (OAuthIdentity, error)
}

type OAuthProviderRegistry struct {
	providers map[string]OAuthLoginProvider
	order     []string
}

func NewOAuthProviderRegistryFromEnv(client *http.Client) *OAuthProviderRegistry {
	if client == nil {
		client = http.DefaultClient
	}

	registry := &OAuthProviderRegistry{providers: map[string]OAuthLoginProvider{}}
	if p := newGoogleOAuthProviderFromEnv(client); p != nil {
		registry.add(p)
	}
	if p := newLarkOAuthProviderFromEnv(client); p != nil {
		registry.add(p)
	}
	return registry
}

func (r *OAuthProviderRegistry) add(provider OAuthLoginProvider) {
	r.providers[provider.ID()] = provider
	r.order = append(r.order, provider.ID())
}

func (r *OAuthProviderRegistry) Get(id string) (OAuthLoginProvider, bool) {
	if r == nil {
		return nil, false
	}
	provider, ok := r.providers[id]
	return provider, ok
}

func (r *OAuthProviderRegistry) PublicConfigs() []OAuthProviderPublicConfig {
	if r == nil {
		return nil
	}
	configs := make([]OAuthProviderPublicConfig, 0, len(r.order))
	for _, id := range r.order {
		configs = append(configs, r.providers[id].PublicConfig())
	}
	return configs
}

type googleOAuthProvider struct {
	client           *http.Client
	clientID         string
	clientSecret     string
	authorizationURL string
	tokenURL         string
	userInfoURL      string
}

func newGoogleOAuthProviderFromEnv(client *http.Client) *googleOAuthProvider {
	clientID := strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_ID"))
	clientSecret := strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_SECRET"))
	if clientID == "" || clientSecret == "" {
		return nil
	}
	return &googleOAuthProvider{
		client:           client,
		clientID:         clientID,
		clientSecret:     clientSecret,
		authorizationURL: envOrDefault("GOOGLE_AUTHORIZATION_URL", defaultGoogleAuthorize),
		tokenURL:         envOrDefault("GOOGLE_TOKEN_URL", defaultGoogleToken),
		userInfoURL:      envOrDefault("GOOGLE_USERINFO_URL", defaultGoogleUserInfo),
	}
}

func (p *googleOAuthProvider) ID() string {
	return OAuthProviderGoogle
}

func (p *googleOAuthProvider) PublicConfig() OAuthProviderPublicConfig {
	return OAuthProviderPublicConfig{
		ID:               p.ID(),
		Label:            "Google",
		ClientID:         p.clientID,
		AuthorizationURL: p.authorizationURL,
		Scope:            "openid email profile",
		ExtraAuthParams: map[string]string{
			"access_type": "offline",
			"prompt":      "select_account",
		},
	}
}

type googleTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
}

type googleUserInfo struct {
	Email   string `json:"email"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
}

func (p *googleOAuthProvider) Exchange(ctx context.Context, req OAuthExchangeRequest) (OAuthIdentity, error) {
	form := url.Values{
		"code":          {req.Code},
		"client_id":     {p.clientID},
		"client_secret": {p.clientSecret},
		"redirect_uri":  {req.RedirectURI},
		"grant_type":    {"authorization_code"},
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return OAuthIdentity{}, err
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	tokenResp, err := p.client.Do(httpReq)
	if err != nil {
		return OAuthIdentity{}, fmt.Errorf("google token exchange failed: %w", err)
	}
	defer tokenResp.Body.Close()

	tokenBody, err := io.ReadAll(tokenResp.Body)
	if err != nil {
		return OAuthIdentity{}, fmt.Errorf("read google token response: %w", err)
	}
	if tokenResp.StatusCode != http.StatusOK {
		return OAuthIdentity{}, fmt.Errorf("google token exchange returned %d: %s", tokenResp.StatusCode, string(tokenBody))
	}

	var token googleTokenResponse
	if err := json.Unmarshal(tokenBody, &token); err != nil {
		return OAuthIdentity{}, fmt.Errorf("parse google token response: %w", err)
	}
	if token.AccessToken == "" {
		return OAuthIdentity{}, errors.New("google token response missing access_token")
	}

	userReq, err := http.NewRequestWithContext(ctx, http.MethodGet, p.userInfoURL, nil)
	if err != nil {
		return OAuthIdentity{}, err
	}
	userReq.Header.Set("Authorization", "Bearer "+token.AccessToken)

	userResp, err := p.client.Do(userReq)
	if err != nil {
		return OAuthIdentity{}, fmt.Errorf("google userinfo fetch failed: %w", err)
	}
	defer userResp.Body.Close()
	if userResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(userResp.Body)
		return OAuthIdentity{}, fmt.Errorf("google userinfo returned %d: %s", userResp.StatusCode, string(body))
	}

	var user googleUserInfo
	if err := json.NewDecoder(userResp.Body).Decode(&user); err != nil {
		return OAuthIdentity{}, fmt.Errorf("parse google userinfo: %w", err)
	}
	email := strings.ToLower(strings.TrimSpace(user.Email))
	if email == "" {
		return OAuthIdentity{}, errors.New("Google account has no email")
	}
	return OAuthIdentity{Email: email, Name: strings.TrimSpace(user.Name), AvatarURL: strings.TrimSpace(user.Picture)}, nil
}

type larkOAuthProvider struct {
	client           *http.Client
	clientID         string
	clientSecret     string
	label            string
	authorizationURL string
	tokenURL         string
	userInfoURL      string
	scope            string
	pkce             bool
}

func newLarkOAuthProviderFromEnv(client *http.Client) *larkOAuthProvider {
	clientID := firstEnv("LARK_OAUTH_CLIENT_ID", "LARK_APP_ID")
	clientSecret := firstEnv("LARK_OAUTH_CLIENT_SECRET", "LARK_APP_SECRET")
	if clientID == "" || clientSecret == "" {
		return nil
	}

	baseURL := normalizeBaseURL(firstEnv("LARK_OAUTH_BASE_URL", "LARK_OPENAPI_BASE_URL"))
	if baseURL == "" {
		baseURL = defaultLarkOpenAPIBase
	}

	return &larkOAuthProvider{
		client:           client,
		clientID:         clientID,
		clientSecret:     clientSecret,
		label:            envOrDefault("LARK_OAUTH_LABEL", defaultLarkProviderLabel),
		authorizationURL: envOrDefault("LARK_OAUTH_AUTHORIZATION_URL", defaultLarkAuthorizationURL(baseURL)),
		tokenURL:         envOrDefault("LARK_OAUTH_TOKEN_URL", baseURL+"/open-apis/authen/v2/oauth/token"),
		userInfoURL:      envOrDefault("LARK_OAUTH_USERINFO_URL", baseURL+"/open-apis/authen/v1/user_info"),
		scope:            strings.TrimSpace(os.Getenv("LARK_OAUTH_SCOPE")),
		pkce:             envBoolDefault("LARK_OAUTH_PKCE", true),
	}
}

func (p *larkOAuthProvider) ID() string {
	return OAuthProviderFeishuLark
}

func (p *larkOAuthProvider) PublicConfig() OAuthProviderPublicConfig {
	return OAuthProviderPublicConfig{
		ID:               p.ID(),
		Label:            p.label,
		ClientID:         p.clientID,
		AuthorizationURL: p.authorizationURL,
		Scope:            p.scope,
		PKCE:             p.pkce,
	}
}

type larkTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Code        int    `json:"code"`
	Msg         string `json:"msg"`
	Data        *struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	} `json:"data"`
}

type larkUserInfoEnvelope struct {
	Code int          `json:"code"`
	Msg  string       `json:"msg"`
	Data larkUserInfo `json:"data"`
}

type larkUserInfo struct {
	Name            string `json:"name"`
	EnName          string `json:"en_name"`
	AvatarURL       string `json:"avatar_url"`
	Email           string `json:"email"`
	EnterpriseEmail string `json:"enterprise_email"`
}

func (p *larkOAuthProvider) Exchange(ctx context.Context, req OAuthExchangeRequest) (OAuthIdentity, error) {
	body := map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     p.clientID,
		"client_secret": p.clientSecret,
		"code":          req.Code,
		"redirect_uri":  req.RedirectURI,
	}
	if req.CodeVerifier != "" {
		body["code_verifier"] = req.CodeVerifier
	}

	tokenBody, err := json.Marshal(body)
	if err != nil {
		return OAuthIdentity{}, err
	}
	tokenReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.tokenURL, strings.NewReader(string(tokenBody)))
	if err != nil {
		return OAuthIdentity{}, err
	}
	tokenReq.Header.Set("Content-Type", "application/json; charset=utf-8")

	tokenResp, err := p.client.Do(tokenReq)
	if err != nil {
		return OAuthIdentity{}, fmt.Errorf("Feishu/Lark token exchange failed: %w", err)
	}
	defer tokenResp.Body.Close()

	rawTokenBody, err := io.ReadAll(tokenResp.Body)
	if err != nil {
		return OAuthIdentity{}, fmt.Errorf("read Feishu/Lark token response: %w", err)
	}
	if tokenResp.StatusCode != http.StatusOK {
		return OAuthIdentity{}, fmt.Errorf("Feishu/Lark token exchange returned %d: %s", tokenResp.StatusCode, string(rawTokenBody))
	}

	var token larkTokenResponse
	if err := json.Unmarshal(rawTokenBody, &token); err != nil {
		return OAuthIdentity{}, fmt.Errorf("parse Feishu/Lark token response: %w", err)
	}
	accessToken := strings.TrimSpace(token.AccessToken)
	if accessToken == "" && token.Data != nil {
		accessToken = strings.TrimSpace(token.Data.AccessToken)
	}
	if accessToken == "" {
		return OAuthIdentity{}, fmt.Errorf("Feishu/Lark token response missing access_token: code=%d msg=%s", token.Code, token.Msg)
	}

	userReq, err := http.NewRequestWithContext(ctx, http.MethodGet, p.userInfoURL, nil)
	if err != nil {
		return OAuthIdentity{}, err
	}
	userReq.Header.Set("Authorization", "Bearer "+accessToken)

	userResp, err := p.client.Do(userReq)
	if err != nil {
		return OAuthIdentity{}, fmt.Errorf("Feishu/Lark userinfo fetch failed: %w", err)
	}
	defer userResp.Body.Close()
	if userResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(userResp.Body)
		return OAuthIdentity{}, fmt.Errorf("Feishu/Lark userinfo returned %d: %s", userResp.StatusCode, string(body))
	}

	var envelope larkUserInfoEnvelope
	if err := json.NewDecoder(userResp.Body).Decode(&envelope); err != nil {
		return OAuthIdentity{}, fmt.Errorf("parse Feishu/Lark userinfo: %w", err)
	}
	if envelope.Code != 0 {
		return OAuthIdentity{}, fmt.Errorf("Feishu/Lark userinfo error: code=%d msg=%s", envelope.Code, envelope.Msg)
	}

	user := envelope.Data
	email := strings.ToLower(strings.TrimSpace(user.EnterpriseEmail))
	if email == "" {
		email = strings.ToLower(strings.TrimSpace(user.Email))
	}
	if email == "" {
		return OAuthIdentity{}, errors.New("Feishu/Lark account has no email")
	}

	name := strings.TrimSpace(user.Name)
	if name == "" {
		name = strings.TrimSpace(user.EnName)
	}
	return OAuthIdentity{Email: email, Name: name, AvatarURL: strings.TrimSpace(user.AvatarURL)}, nil
}

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envBoolDefault(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	switch strings.ToLower(value) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		slog.Warn("invalid boolean env value; using default", "key", key, "value", value, "default", fallback)
		return fallback
	}
}

func normalizeBaseURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	return strings.TrimRight(raw, "/")
}

func defaultLarkAuthorizationURL(openAPIBaseURL string) string {
	accountBaseURL := normalizeBaseURL(os.Getenv("LARK_OAUTH_ACCOUNT_BASE_URL"))
	if accountBaseURL == "" {
		switch {
		case strings.Contains(openAPIBaseURL, "open.larksuite.com"):
			accountBaseURL = defaultLarkAccountBase
		case strings.Contains(openAPIBaseURL, "open.feishu.cn"):
			accountBaseURL = defaultFeishuAccountBase
		default:
			accountBaseURL = openAPIBaseURL
		}
	}
	return accountBaseURL + "/open-apis/authen/v1/authorize"
}
