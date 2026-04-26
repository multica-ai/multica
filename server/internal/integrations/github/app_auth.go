package github

// GitHub App authentication — mint installation tokens by signing a JWT with
// the App's private key, then exchanging it for an installation access token.
//
// References:
//   https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-a-json-web-token-jwt-for-a-github-app
//   https://docs.github.com/en/rest/apps/apps#create-an-installation-access-token-for-an-app

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// AppAuth mints installation tokens for a single GitHub App. Safe for
// concurrent use; tokens are cached per-installation until shortly before
// they expire.
type AppAuth struct {
	AppID      int64
	privateKey *rsa.PrivateKey
	httpClient *http.Client

	mu     sync.Mutex
	tokens map[int64]cachedToken // installation_id → token
}

type cachedToken struct {
	value     string
	expiresAt time.Time
}

// NewAppAuthFromEnv loads GITHUB_APP_ID and GITHUB_APP_PRIVATE_KEY_PATH from
// the environment and returns a ready AppAuth. Returns an error if either
// is unset or if the private key fails to parse.
func NewAppAuthFromEnv() (*AppAuth, error) {
	appIDStr := os.Getenv("GITHUB_APP_ID")
	keyPath := os.Getenv("GITHUB_APP_PRIVATE_KEY_PATH")
	if appIDStr == "" || keyPath == "" {
		return nil, errors.New("GITHUB_APP_ID and GITHUB_APP_PRIVATE_KEY_PATH must be set")
	}
	var appID int64
	if _, err := fmt.Sscanf(appIDStr, "%d", &appID); err != nil || appID <= 0 {
		return nil, fmt.Errorf("invalid GITHUB_APP_ID: %q", appIDStr)
	}
	pemBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("read private key: %w", err)
	}
	key, err := jwt.ParseRSAPrivateKeyFromPEM(pemBytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	return &AppAuth{
		AppID:      appID,
		privateKey: key,
		httpClient: &http.Client{Timeout: 15 * time.Second},
		tokens:     make(map[int64]cachedToken),
	}, nil
}

// signJWT produces a 10-minute App-level JWT signed with RS256.
func (a *AppAuth) signJWT() (string, error) {
	now := time.Now().UTC()
	claims := jwt.MapClaims{
		// "iat" is recommended to be 60s in the past to allow for clock skew.
		"iat": now.Add(-30 * time.Second).Unix(),
		"exp": now.Add(9 * time.Minute).Unix(),
		"iss": a.AppID,
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	return tok.SignedString(a.privateKey)
}

// InstallationToken returns a valid installation access token for the given
// installation ID. Tokens are cached and refreshed automatically. The
// returned token is safe to use as a bearer token against the GitHub REST
// API for ~1 hour from issue.
func (a *AppAuth) InstallationToken(ctx context.Context, installationID int64) (string, error) {
	a.mu.Lock()
	if t, ok := a.tokens[installationID]; ok && time.Until(t.expiresAt) > 60*time.Second {
		a.mu.Unlock()
		return t.value, nil
	}
	a.mu.Unlock()

	jwtStr, err := a.signJWT()
	if err != nil {
		return "", fmt.Errorf("sign jwt: %w", err)
	}

	url := fmt.Sprintf("https://api.github.com/app/installations/%d/access_tokens", installationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+jwtStr)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "multica-coderabbit-bridge")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("create installation token: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("create installation token: status %d: %s", resp.StatusCode, string(body))
	}

	var out struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}
	if out.Token == "" || out.ExpiresAt.IsZero() {
		return "", errors.New("installation token response missing fields")
	}

	a.mu.Lock()
	a.tokens[installationID] = cachedToken{value: out.Token, expiresAt: out.ExpiresAt}
	a.mu.Unlock()
	return out.Token, nil
}
