// Package forgejo is the Forgejo (self-hosted Git forge) integration: a thin
// token-authenticated REST client plus webhook helpers. Forgejo speaks the
// Gitea API (/api/v1) and signs webhooks with HMAC-SHA256 over the raw body
// in the X-Gitea-Signature header.
//
// Unlike GitHub (server/internal/handler/github.go), there is no App or
// installation model: each workspace connection carries its own instance URL
// and access token, stored encrypted (MULTICA_FORGEJO_SECRET_KEY).
package forgejo

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrUnauthorized is returned when the access token is rejected by the
// instance (HTTP 401/403). Callers surface this as a connect-time validation
// failure distinct from transport/instance errors.
var ErrUnauthorized = errors.New("forgejo: token unauthorized")

// Client is a token-authenticated REST client bound to one Forgejo instance.
// It holds no state beyond the base URL, token, and an http.Client, so it is
// cheap to construct per request.
type Client struct {
	baseURL string // instance URL, no trailing slash, no /api/v1 suffix
	token   string
	http    *http.Client
}

// NewClient builds a client for instanceURL authenticating as token. The
// instanceURL is normalized (trailing slash trimmed); the /api/v1 prefix is
// added per call.
func NewClient(instanceURL, token string) *Client {
	return &Client{
		baseURL: NormalizeInstanceURL(instanceURL),
		token:   token,
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

// NormalizeInstanceURL trims whitespace and any trailing slash so the stored
// instance_url and webhook URLs are stable regardless of how the user typed
// it. It does not validate the scheme — the connect handler does that.
func NormalizeInstanceURL(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}

// User is the subset of GET /api/v1/user we consume: enough to confirm the
// token works and learn the authenticated login for display.
type User struct {
	Login string `json:"login"`
}

// CurrentUser validates the token and returns the authenticated account. A
// 401/403 maps to ErrUnauthorized; other non-2xx responses surface as a
// generic error so the caller can distinguish "bad token" from "instance
// unreachable / misconfigured".
func (c *Client) CurrentUser(ctx context.Context) (User, error) {
	endpoint := c.baseURL + "/api/v1/user"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return User{}, fmt.Errorf("forgejo: build request: %w", err)
	}
	req.Header.Set("Authorization", "token "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return User{}, fmt.Errorf("forgejo: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return User{}, ErrUnauthorized
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return User{}, fmt.Errorf("forgejo: GET /user: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var u User
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return User{}, fmt.Errorf("forgejo: decode user: %w", err)
	}
	if u.Login == "" {
		return User{}, errors.New("forgejo: user response missing login")
	}
	return u, nil
}

// VerifyWebhookSignature reports whether sigHeader (the hex digest from
// X-Gitea-Signature) is a valid HMAC-SHA256 of body under secret. Forgejo,
// like Gitea, sends a bare hex digest with no "sha256=" prefix (that prefix
// is GitHub's X-Hub-Signature-256 convention). Comparison is constant-time.
func VerifyWebhookSignature(secret string, sigHeader string, body []byte) bool {
	sigHeader = strings.TrimSpace(sigHeader)
	// Tolerate a "sha256=" prefix in case the payload was configured against
	// the GitHub-style header; Forgejo can populate both.
	sigHeader = strings.TrimPrefix(sigHeader, "sha256=")
	want, err := hex.DecodeString(sigHeader)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), want)
}
