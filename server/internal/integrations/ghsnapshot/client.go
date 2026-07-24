// Package ghsnapshot fetches a GitHub pull request's CI + mergeability state
// from the GitHub API and treats that response as the single source of truth
// for the PR card (MUL-5265, Plan C). Webhooks and page visits only trigger a
// refresh; nothing here infers state incrementally from webhook payloads.
//
// The package has three layers:
//   - Client (this file): GitHub App auth — App JWT → installation access
//     token (cached per installation, refreshed early) → GraphQL/REST calls.
//   - snapshot.go: the single GraphQL query, contexts pagination, and
//     normalization into a flat per-check snapshot.
//   - refresh.go: the outbound work queue (dedup, single in-flight per PR,
//     bounded concurrency, Retry-After backoff), the head-SHA-guarded atomic
//     write, and the trigger surfaces (webhook / page visit / TTL sweep).
//
// Credential hygiene (acceptance criterion 6): the App private key and every
// installation token are treated as opaque secrets and are NEVER written to a
// log or embedded in an error message.
package ghsnapshot

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	defaultAPIBase = "https://api.github.com"
	// Renew an installation token this long before it actually expires so an
	// in-flight request never races the expiry boundary. GitHub tokens live
	// one hour.
	tokenRenewSkew = 5 * time.Minute
)

// RateLimitError signals that GitHub asked us to back off. RetryAfter is how
// long to wait before retrying; it is derived from the Retry-After header, or
// the X-RateLimit-Reset header, or a conservative default.
type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("github rate limited, retry after %s", e.RetryAfter)
}

// Client is an installation-token-authenticated GitHub API client. A nil
// *Client is a valid "feature disabled" value — every method the refresh
// pipeline calls tolerates it — so a deployment without a GitHub App private
// key degrades cleanly (acceptance criterion 4).
type Client struct {
	appID      string
	privateKey *rsa.PrivateKey
	apiBase    string
	httpClient *http.Client
	now        func() time.Time

	mu     sync.Mutex
	tokens map[int64]cachedToken
}

type cachedToken struct {
	token  string
	expiry time.Time
}

// NewClientFromEnv builds a Client from GITHUB_APP_ID and GITHUB_APP_PRIVATE_KEY.
//
//   - Both unset → (nil, nil): the App API is simply not configured for this
//     deployment; the caller degrades the whole feature off.
//   - Key present but malformed → (nil, err): operator-actionable, surface it.
func NewClientFromEnv() (*Client, error) {
	appID := strings.TrimSpace(os.Getenv("GITHUB_APP_ID"))
	pemKey := strings.TrimSpace(os.Getenv("GITHUB_APP_PRIVATE_KEY"))
	if appID == "" || pemKey == "" {
		return nil, nil
	}
	key, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(pemKey))
	if err != nil {
		// Deliberately do not include the key material in the error.
		return nil, fmt.Errorf("parse GITHUB_APP_PRIVATE_KEY: %w", err)
	}
	return &Client{
		appID:      appID,
		privateKey: key,
		apiBase:    defaultAPIBase,
		httpClient: &http.Client{Timeout: 20 * time.Second},
		now:        time.Now,
		tokens:     map[int64]cachedToken{},
	}, nil
}

// Enabled reports whether the App API is configured. A nil client is disabled.
func (c *Client) Enabled() bool { return c != nil && c.privateKey != nil }

// signAppJWT mints the short-lived RS256 JWT GitHub requires for
// App-authenticated calls. iat is back-dated 60s to absorb clock skew and exp
// is capped at 9 minutes (GitHub's ceiling is 10).
func (c *Client) signAppJWT(now time.Time) (string, error) {
	claims := jwt.MapClaims{
		"iat": now.Add(-60 * time.Second).Unix(),
		"exp": now.Add(9 * time.Minute).Unix(),
		"iss": c.appID,
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signed, err := tok.SignedString(c.privateKey)
	if err != nil {
		return "", errors.New("sign App JWT failed")
	}
	return signed, nil
}

// installationToken returns a cached installation access token, minting a new
// one via POST /app/installations/{id}/access_tokens when the cache is empty or
// within the renew skew of expiry.
func (c *Client) installationToken(ctx context.Context, installationID int64) (string, error) {
	now := c.now()
	c.mu.Lock()
	if t, ok := c.tokens[installationID]; ok && now.Add(tokenRenewSkew).Before(t.expiry) {
		tok := t.token
		c.mu.Unlock()
		return tok, nil
	}
	c.mu.Unlock()

	appJWT, err := c.signAppJWT(now)
	if err != nil {
		return "", err
	}
	endpoint := fmt.Sprintf("%s/app/installations/%d/access_tokens", strings.TrimRight(c.apiBase, "/"), installationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+appJWT)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		return "", rateLimitFromResponse(resp, c.now())
	}
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		// Never echo the body — a token-mint failure body can contain sensitive
		// hints; the status code is enough to diagnose.
		return "", fmt.Errorf("github installation token: unexpected status %d", resp.StatusCode)
	}
	var parsed struct {
		Token     string `json:"token"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", errors.New("github installation token: malformed response")
	}
	if parsed.Token == "" {
		return "", errors.New("github installation token: empty token")
	}
	expiry := now.Add(time.Hour)
	if parsed.ExpiresAt != "" {
		if t, err := time.Parse(time.RFC3339, parsed.ExpiresAt); err == nil {
			expiry = t
		}
	}
	c.mu.Lock()
	c.tokens[installationID] = cachedToken{token: parsed.Token, expiry: expiry}
	c.mu.Unlock()
	return parsed.Token, nil
}

// graphQL runs a single GraphQL query as the given installation and returns the
// raw `data` object. GitHub returns HTTP 200 even for query-level errors, so we
// inspect the `errors` array too, mapping a RATE_LIMITED error type to a
// RateLimitError.
func (c *Client) graphQL(ctx context.Context, installationID int64, query string, variables map[string]any) (json.RawMessage, error) {
	token, err := c.installationToken(ctx, installationID)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return nil, err
	}
	endpoint := strings.TrimRight(c.apiBase, "/") + "/graphql"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		return nil, rateLimitFromResponse(resp, c.now())
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github graphql: unexpected status %d", resp.StatusCode)
	}
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, errors.New("github graphql: malformed response")
	}
	if len(envelope.Errors) > 0 {
		for _, e := range envelope.Errors {
			if e.Type == "RATE_LIMITED" {
				return nil, &RateLimitError{RetryAfter: time.Minute}
			}
		}
		// Surface the message but nothing else; GraphQL error messages do not
		// contain credentials.
		return nil, fmt.Errorf("github graphql error: %s", envelope.Errors[0].Message)
	}
	if len(envelope.Data) == 0 {
		return nil, errors.New("github graphql: empty data")
	}
	return envelope.Data, nil
}

// rateLimitFromResponse builds a RateLimitError from GitHub's throttling
// headers. Retry-After (seconds) wins; then X-RateLimit-Reset (unix seconds);
// otherwise a conservative 60s. The wait is clamped to [1s, 5m].
func rateLimitFromResponse(resp *http.Response, now time.Time) *RateLimitError {
	wait := time.Minute
	if v := resp.Header.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && secs >= 0 {
			wait = time.Duration(secs) * time.Second
		}
	} else if v := resp.Header.Get("X-RateLimit-Reset"); v != "" {
		if unix, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
			if d := time.Unix(unix, 0).Sub(now); d > 0 {
				wait = d
			}
		}
	}
	if wait < time.Second {
		wait = time.Second
	}
	if wait > 5*time.Minute {
		wait = 5 * time.Minute
	}
	return &RateLimitError{RetryAfter: wait}
}
