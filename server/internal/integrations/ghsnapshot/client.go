// Package ghsnapshot fetches a GitHub pull request's CI + mergeability state
// from the GitHub API and treats that response as the single source of truth
// for the PR card (MUL-5265, Plan C). Webhooks and page visits only trigger a
// refresh; nothing here infers state incrementally from webhook payloads.
//
// The package has three layers:
//   - Client (this file): GitHub App auth — App JWT → installation access
//     token (cached per installation, refreshed early) → REST calls.
//   - snapshot.go: PR / Checks / Statuses queries, pagination, and
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
	"golang.org/x/sync/singleflight"
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
	// sf collapses concurrent token mints for the same installation into a
	// single HTTP call, so the N workers of one installation don't each mint a
	// token on a cold cache or a simultaneous renew.
	sf singleflight.Group
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
// within the renew skew of expiry. Concurrent callers for the same installation
// are collapsed by singleflight, so a cold cache under N workers mints once.
func (c *Client) installationToken(ctx context.Context, installationID int64) (string, error) {
	if tok, ok := c.cachedToken(installationID); ok {
		return tok, nil
	}
	v, err, _ := c.sf.Do(strconv.FormatInt(installationID, 10), func() (any, error) {
		// A concurrent caller may have minted while we waited for the flight.
		if tok, ok := c.cachedToken(installationID); ok {
			return tok, nil
		}
		return c.mintInstallationToken(ctx, installationID)
	})
	if err != nil {
		return "", err
	}
	return v.(string), nil
}

// cachedToken returns a cached token that is still outside the renew skew.
func (c *Client) cachedToken(installationID int64) (string, bool) {
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if t, ok := c.tokens[installationID]; ok && now.Add(tokenRenewSkew).Before(t.expiry) {
		return t.token, true
	}
	return "", false
}

// mintInstallationToken exchanges the App JWT for a fresh installation token and
// caches it. Called under singleflight, so only one runs per installation.
func (c *Client) mintInstallationToken(ctx context.Context, installationID int64) (string, error) {
	now := c.now()
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
	if isRateLimited(resp, body) {
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

// restGet authenticates against a fixed API path. Response bodies and tokens
// never appear in errors; permission failures must not pause an installation.
func (c *Client) restGet(ctx context.Context, installationID int64, path string) (json.RawMessage, error) {
	token, err := c.installationToken(ctx, installationID)
	if err != nil {
		return nil, err
	}
	endpoint := strings.TrimRight(c.apiBase, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	const maxBody = 8 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, errors.New("github REST: failed to read response")
	}
	if isRateLimited(resp, body) {
		return nil, rateLimitFromResponse(resp, c.now())
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github REST: unexpected status %d", resp.StatusCode)
	}
	if len(body) > maxBody {
		return nil, errors.New("github REST: response exceeds size limit")
	}
	return body, nil
}

func isRateLimited(resp *http.Response, body []byte) bool {
	if resp.StatusCode == http.StatusTooManyRequests {
		return true
	}
	if resp.StatusCode != http.StatusForbidden {
		return false
	}
	if resp.Header.Get("X-RateLimit-Remaining") == "0" || resp.Header.Get("Retry-After") != "" {
		return true
	}
	var failure struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &failure) != nil {
		return false
	}
	message := strings.ToLower(failure.Message)
	return strings.Contains(message, "secondary rate limit") || strings.Contains(message, "abuse detection")
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
