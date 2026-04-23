package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// OAuthProviderPublicConfig is the per-provider slice surfaced via /api/config.
// CallbackPath is the redirect_uri and must be echoed verbatim on token exchange.
type OAuthProviderPublicConfig struct {
	ClientID        string            `json:"client_id"`
	AuthorizeURL    string            `json:"authorize_url"`
	CallbackPath    string            `json:"callback_path"`
	Scope           string            `json:"scope"`
	ExtraAuthParams map[string]string `json:"extra_auth_params,omitempty"`
}

// OAuthProfile is the provider-normalised view of the authenticated user.
// Email is expected to be trimmed but not necessarily lower-cased; the
// handler lower-cases before DB lookup.
type OAuthProfile struct {
	Email         string
	Name          string
	Picture       string
	EmailVerified bool
}

// OAuthProvider is the surface the handler uses. Exchange and FetchProfile
// are split so the handler can return distinct HTTP codes (400 vs 502) for
// token-exchange vs userinfo failures.
type OAuthProvider interface {
	ID() string
	Configured() bool
	PublicConfig() OAuthProviderPublicConfig
	RedirectURI() string
	Exchange(ctx context.Context, code, redirectURI string) (accessToken string, err error)
	FetchProfile(ctx context.Context, accessToken string) (OAuthProfile, error)
}

// ProviderSpec is the data-only description of a provider. Adding a new
// provider should be: one ProviderSpec literal + one ParseProfile function.
// The callback path is derived from <PROVIDER>_REDIRECT_URI env at runtime —
// there is no hardcoded default, so operators have a single source of truth.
type ProviderSpec struct {
	ID              string
	AuthorizeURL    string
	TokenURL        string
	UserinfoURL     string
	Scope           string
	ClientIDEnv     string
	ClientSecretEnv string
	RedirectURIEnv  string

	ExtraAuthParams  map[string]string
	ExtraTokenParams map[string]string

	// TokenErrorFromBody surfaces errors that providers return with HTTP
	// 200 + a JSON error body (GitHub). Nil means HTTP status is
	// authoritative.
	TokenErrorFromBody func(body []byte) error

	// ParseProfile turns a userinfo response into an OAuthProfile. It may
	// make additional authenticated requests (e.g. GitHub's /user/emails
	// fallback when the primary email is private).
	ParseProfile func(ctx context.Context, client *http.Client, accessToken string, userinfoBody []byte) (OAuthProfile, error)
}

// HTTPOAuthProvider is the single HTTP implementation driven by a
// ProviderSpec. All providers share this type; spec data supplies the
// per-provider differences.
type HTTPOAuthProvider struct {
	spec   *ProviderSpec
	client *http.Client
}

func NewHTTPOAuthProvider(spec *ProviderSpec) *HTTPOAuthProvider {
	return &HTTPOAuthProvider{
		spec:   spec,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (p *HTTPOAuthProvider) ID() string { return p.spec.ID }

func (p *HTTPOAuthProvider) clientID() string {
	return strings.TrimSpace(os.Getenv(p.spec.ClientIDEnv))
}

func (p *HTTPOAuthProvider) clientSecret() string {
	return strings.TrimSpace(os.Getenv(p.spec.ClientSecretEnv))
}

// Configured reports whether every required env var for this provider is set.
// The redirect URI is required alongside credentials — without it the client
// can't build a matching redirect_uri, so there's no point advertising the
// provider via /api/config.
func (p *HTTPOAuthProvider) Configured() bool {
	return p.clientID() != "" && p.clientSecret() != "" && p.callbackPath() != ""
}

func (p *HTTPOAuthProvider) RedirectURI() string {
	if p.spec.RedirectURIEnv == "" {
		return ""
	}
	return strings.TrimSpace(os.Getenv(p.spec.RedirectURIEnv))
}

// callbackPath returns the path portion of <PROVIDER>_REDIRECT_URI env. Empty
// when the env var is unset or malformed — which is intentional, see Configured.
func (p *HTTPOAuthProvider) callbackPath() string {
	raw := p.RedirectURI()
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Path
}

func (p *HTTPOAuthProvider) PublicConfig() OAuthProviderPublicConfig {
	return OAuthProviderPublicConfig{
		ClientID:        p.clientID(),
		AuthorizeURL:    p.spec.AuthorizeURL,
		CallbackPath:    p.callbackPath(),
		Scope:           p.spec.Scope,
		ExtraAuthParams: p.spec.ExtraAuthParams,
	}
}

func (p *HTTPOAuthProvider) Exchange(ctx context.Context, code, redirectURI string) (string, error) {
	clientID := p.clientID()
	clientSecret := p.clientSecret()
	if clientID == "" || clientSecret == "" {
		return "", fmt.Errorf("%s: env not configured", p.spec.ID)
	}

	form := url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"code":          {code},
	}
	if redirectURI != "" {
		form.Set("redirect_uri", redirectURI)
	}
	for k, v := range p.spec.ExtraTokenParams {
		form.Set(k, v)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		p.spec.TokenURL,
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s token exchange status %d: %s", p.spec.ID, resp.StatusCode, truncateForLog(body))
	}
	if p.spec.TokenErrorFromBody != nil {
		if err := p.spec.TokenErrorFromBody(body); err != nil {
			return "", err
		}
	}

	var payload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("%s token exchange: parse body: %w", p.spec.ID, err)
	}
	if payload.AccessToken == "" {
		return "", fmt.Errorf("%s token exchange: empty access_token", p.spec.ID)
	}
	return payload.AccessToken, nil
}

func (p *HTTPOAuthProvider) FetchProfile(ctx context.Context, accessToken string) (OAuthProfile, error) {
	body, err := getBearerJSON(ctx, p.client, accessToken, p.spec.UserinfoURL, nil)
	if err != nil {
		return OAuthProfile{}, err
	}
	if p.spec.ParseProfile == nil {
		return OAuthProfile{}, errors.New(p.spec.ID + ": ParseProfile not configured")
	}
	return p.spec.ParseProfile(ctx, p.client, accessToken, body)
}

// getBearerJSON is the shared "GET with bearer token" helper used by both
// FetchProfile and provider-specific follow-up calls (e.g. GitHub's
// /user/emails). extraHeaders may be nil.
func getBearerJSON(ctx context.Context, client *http.Client, accessToken, endpoint string, extraHeaders map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: status %d: %s", endpoint, resp.StatusCode, truncateForLog(body))
	}
	return body, nil
}

// truncateForLog returns a single-line, size-capped view of a response body.
// Keeps logs readable when a provider returns an HTML error page instead of
// the expected JSON (e.g. CDN 503 from a flaky network path).
func truncateForLog(body []byte) string {
	const maxLen = 200
	s := strings.Join(strings.Fields(string(body)), " ")
	if len(s) > maxLen {
		return s[:maxLen] + "…"
	}
	return s
}
