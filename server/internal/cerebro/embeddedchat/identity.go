// Package embeddedchat verifies the human identity delegated by an embedded app.
package embeddedchat

import (
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

const HeaderUserAssertion = "X-Multica-User-Assertion"

var ErrInvalidIdentity = errors.New("embedded chat: invalid user identity")

// Provider delegates token validation to the identity provider's authoritative
// user endpoint. This supports Supabase projects that still sign access tokens
// with HS256 without copying their JWT signing secret into Multica.
type Provider struct {
	UserInfoURL string `json:"user_info_url"`
	APIKeyEnv   string `json:"api_key_env"`
	Provider    string `json:"required_provider"`
	APIKey      string `json:"-"`
}

type Identity struct{ Email string }

type Verifier struct {
	providers map[string]Provider
	http      *http.Client
}

func NewVerifier(providers map[string]Provider) *Verifier {
	return &Verifier{
		providers: providers,
		http:      &http.Client{Timeout: 10 * time.Second},
	}
}

func ParseProviders(raw string) (map[string]Provider, error) {
	var providers map[string]Provider
	if err := json.Unmarshal([]byte(raw), &providers); err != nil {
		return nil, fmt.Errorf("parse embedded chat providers: %w", err)
	}
	if len(providers) == 0 {
		return nil, errors.New("embedded chat providers are empty")
	}
	for source, p := range providers {
		if strings.TrimSpace(source) == "" || p.UserInfoURL == "" || p.APIKeyEnv == "" || p.Provider == "" {
			return nil, fmt.Errorf("embedded chat provider %q is incomplete", source)
		}
		p.APIKey = os.Getenv(p.APIKeyEnv)
		if strings.TrimSpace(p.APIKey) == "" {
			return nil, fmt.Errorf("embedded chat provider %q API key is not configured", source)
		}
		providers[source] = p
	}
	return providers, nil
}

func (v *Verifier) Verify(ctx context.Context, source, assertion string) (Identity, error) {
	p, ok := v.providers[source]
	if !ok || strings.TrimSpace(assertion) == "" || strings.TrimSpace(p.APIKey) == "" {
		return Identity{}, ErrInvalidIdentity
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.UserInfoURL, nil)
	if err != nil {
		return Identity{}, fmt.Errorf("%w: create user verification request", ErrInvalidIdentity)
	}
	req.Header.Set("Authorization", "Bearer "+assertion)
	req.Header.Set("apikey", p.APIKey)
	resp, err := v.http.Do(req)
	if err != nil {
		return Identity{}, fmt.Errorf("%w: user verification failed", ErrInvalidIdentity)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Identity{}, fmt.Errorf("%w: identity provider rejected token", ErrInvalidIdentity)
	}
	var user struct {
		ID          string         `json:"id"`
		Email       string         `json:"email"`
		AppMetadata map[string]any `json:"app_metadata"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&user); err != nil {
		return Identity{}, fmt.Errorf("%w: invalid user response", ErrInvalidIdentity)
	}
	email := strings.ToLower(strings.TrimSpace(user.Email))
	if user.ID == "" || email == "" || !providerMatches(user.AppMetadata, p.Provider) {
		return Identity{}, ErrInvalidIdentity
	}
	return Identity{Email: email}, nil
}

func providerMatches(app map[string]any, required string) bool {
	if provider, _ := app["provider"].(string); provider == required {
		return true
	}
	if providers, ok := app["providers"].([]any); ok {
		for _, provider := range providers {
			if provider == required {
				return true
			}
		}
	}
	return false
}
