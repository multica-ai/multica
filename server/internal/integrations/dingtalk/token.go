package dingtalk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// This file is the outbound DingTalk Open-API client: an access_token cache plus
// a JSON POST helper. DingTalk's access_token expires (~2h), unlike Slack's
// static bot token, so it is cached in-process keyed by AppKey and refreshed
// before expiry — the same shape as Feishu's tenant_access_token cache.

// tokenSafetyMargin is subtracted from DingTalk's expireIn so a token is
// refreshed before it actually expires, absorbing clock skew and in-flight use.
const tokenSafetyMargin = 5 * time.Minute

// Client caches access_tokens and posts robot messages. One instance is shared
// across installations; the cache is keyed by AppKey so each installation's
// token is independent. Safe for concurrent use.
type Client struct {
	httpClient *http.Client
	apiBase    string
	now        func() time.Time

	mu     sync.Mutex
	tokens map[string]cachedToken

	// minting collapses concurrent cache misses for the same AppKey into a
	// single in-flight token request.
	minting singleflight.Group
}

type cachedToken struct {
	value     string
	expiresAt time.Time
}

// NewClient builds the outbound client. apiBase defaults to the DingTalk
// Open-API host; tests point it at an httptest server.
func NewClient(httpClient *http.Client, apiBase string) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if apiBase == "" {
		apiBase = defaultAPIBase
	}
	return &Client{
		httpClient: httpClient,
		apiBase:    strings.TrimRight(apiBase, "/"),
		now:        time.Now,
		tokens:     map[string]cachedToken{},
	}
}

// accessToken returns a usable access_token for (appKey, appSecret), minting and
// caching one when the cache is empty or stale.
func (c *Client) accessToken(ctx context.Context, appKey, appSecret string) (string, error) {
	if t, ok := c.cachedToken(appKey); ok {
		return t, nil
	}

	// Collapse concurrent misses for the same AppKey into one mint: a burst of
	// outbound sends (ack + reply) sharing an expired token would otherwise each
	// fire a redundant token request and race to overwrite the cache. DingTalk
	// rate-limits token issuance, so the fan-in also avoids tripping that limit.
	v, err, _ := c.minting.Do(appKey, func() (any, error) {
		// A mint that finished while we queued behind the flight already
		// refreshed the cache; reuse it instead of fetching again.
		if t, ok := c.cachedToken(appKey); ok {
			return t, nil
		}
		token, expireIn, err := fetchAccessToken(ctx, c.httpClient, c.apiBase, appKey, appSecret)
		if err != nil {
			return "", err
		}
		ttl := time.Duration(expireIn) * time.Second
		if ttl < tokenSafetyMargin*2 {
			ttl = tokenSafetyMargin * 2
		}
		c.mu.Lock()
		c.tokens[appKey] = cachedToken{value: token, expiresAt: c.now().Add(ttl - tokenSafetyMargin)}
		c.mu.Unlock()
		return token, nil
	})
	if err != nil {
		return "", err
	}
	return v.(string), nil
}

// cachedToken returns the cached token for appKey if present and unexpired.
func (c *Client) cachedToken(appKey string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if t, ok := c.tokens[appKey]; ok && t.expiresAt.After(c.now()) {
		return t.value, true
	}
	return "", false
}

// invalidate drops the cached token for appKey so the next accessToken call
// refreshes. Used after the API reports an expired/invalid token (HTTP 401).
func (c *Client) invalidate(appKey string) {
	c.mu.Lock()
	delete(c.tokens, appKey)
	c.mu.Unlock()
}

// postJSON posts body to path with the access token header and decodes a 2xx
// response into out (out may be nil to ignore the body). It returns
// errUnauthorized on HTTP 401 so the caller can refresh the token and retry.
func (c *Client) postJSON(ctx context.Context, path, accessToken string, body, out any) error {
	reqBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("dingtalk: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiBase+path, bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("dingtalk: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-acs-dingtalk-access-token", accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("dingtalk: request %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode == http.StatusUnauthorized {
		return errUnauthorized
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr apiError
		_ = json.Unmarshal(respBody, &apiErr)
		if apiErr.Message != "" {
			return fmt.Errorf("dingtalk: %s: code=%q message=%q", path, apiErr.Code, apiErr.Message)
		}
		return fmt.Errorf("dingtalk: %s: http %d", path, resp.StatusCode)
	}
	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("dingtalk: decode %s response: %w", path, err)
		}
	}
	return nil
}
