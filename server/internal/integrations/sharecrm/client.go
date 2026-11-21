package sharecrm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// API error codes from ShareCRM IM Gateway (bot-api.md §9).
const (
	codeOK              = 0
	codeTokenInvalid    = 40100
	codeTokenExpired    = 40101
	codeBotNotConnected = 50001
)

var (
	errUnauthorized   = errors.New("sharecrm: unauthorized (token invalid or expired)")
	errBotNotConnected = errors.New("sharecrm: bot not connected (SSE offline)")
)

const (
	authTokenPath  = "/im-gateway/auth/token"
	sendMessagePath = "/im-gateway/qixin/message/send"
	tokenSafetyMargin = 5 * time.Minute
	tokenMintTimeout  = 10 * time.Second
)

// Client caches access tokens and posts outbound messages. One instance is
// shared across installations; the cache is keyed by appId. Safe for concurrent
// use. Per-installation gateway base URLs are passed per call.
type Client struct {
	httpClient *http.Client
	now        func() time.Time

	mu      sync.Mutex
	tokens  map[string]cachedToken
	minting singleflight.Group
}

type cachedToken struct {
	value     string
	expiresAt time.Time
	baseURL   string
}

// NewClient builds the shared outbound client.
func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		httpClient: httpClient,
		now:        time.Now,
		tokens:     map[string]cachedToken{},
	}
}

type gatewayEnvelope struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

type authTokenData struct {
	AccessToken string `json:"accessToken"`
	ExpiresIn   int64  `json:"expiresIn"`
	TokenType   string `json:"tokenType"`
}

type sendMessageData struct {
	MessageID string `json:"message_id"`
}

// FetchAccessToken mints a token from appId/appSecret. Used by BYO install
// validation and by the token cache.
func FetchAccessToken(ctx context.Context, httpClient *http.Client, baseURL, appID, appSecret string) (string, int64, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = DefaultGatewayBaseURL
	}
	body, err := json.Marshal(map[string]string{
		"appId":     appID,
		"appSecret": appSecret,
	})
	if err != nil {
		return "", 0, fmt.Errorf("sharecrm: marshal auth request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+authTokenPath, bytes.NewReader(body))
	if err != nil {
		return "", 0, fmt.Errorf("sharecrm: build auth request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("sharecrm: auth request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", 0, fmt.Errorf("sharecrm: read auth response: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return "", 0, errUnauthorized
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", 0, fmt.Errorf("sharecrm: auth http %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	var env gatewayEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return "", 0, fmt.Errorf("sharecrm: decode auth envelope: %w", err)
	}
	if env.Code != codeOK {
		return "", 0, fmt.Errorf("sharecrm: auth code=%d msg=%q", env.Code, env.Msg)
	}
	var data authTokenData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		return "", 0, fmt.Errorf("sharecrm: decode auth data: %w", err)
	}
	if data.AccessToken == "" {
		return "", 0, errors.New("sharecrm: auth response missing accessToken")
	}
	return data.AccessToken, data.ExpiresIn, nil
}

func (c *Client) accessToken(ctx context.Context, baseURL, appID, appSecret string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = DefaultGatewayBaseURL
	}
	if t, ok := c.cachedToken(appID, base); ok {
		return t, nil
	}
	v, err, _ := c.minting.Do(appID+"@"+base, func() (any, error) {
		if t, ok := c.cachedToken(appID, base); ok {
			return t, nil
		}
		mintCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), tokenMintTimeout)
		defer cancel()
		token, expiresIn, err := FetchAccessToken(mintCtx, c.httpClient, base, appID, appSecret)
		if err != nil {
			return "", err
		}
		ttl := time.Duration(expiresIn) * time.Second
		if ttl <= 0 {
			ttl = 2 * time.Hour
		}
		if ttl > tokenSafetyMargin {
			ttl -= tokenSafetyMargin
		}
		c.mu.Lock()
		c.tokens[appID] = cachedToken{
			value:     token,
			expiresAt: c.now().Add(ttl),
			baseURL:   base,
		}
		c.mu.Unlock()
		return token, nil
	})
	if err != nil {
		return "", err
	}
	return v.(string), nil
}

func (c *Client) cachedToken(appID, baseURL string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	t, ok := c.tokens[appID]
	if !ok || t.baseURL != baseURL || c.now().After(t.expiresAt) || t.value == "" {
		return "", false
	}
	return t.value, true
}

func (c *Client) invalidate(appID string) {
	c.mu.Lock()
	delete(c.tokens, appID)
	c.mu.Unlock()
}

// SendMessage posts a text reply into chatID. replyMessageID is optional.
func (c *Client) SendMessage(ctx context.Context, baseURL, appID, appSecret, chatID, text string, replyMessageID *int64) (string, error) {
	if strings.TrimSpace(chatID) == "" {
		return "", errors.New("sharecrm: chat_id is required")
	}
	if strings.TrimSpace(text) == "" {
		return "", nil
	}
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = DefaultGatewayBaseURL
	}
	payload := map[string]any{
		"chat_id": chatID,
		"text":    text,
	}
	if replyMessageID != nil && *replyMessageID > 0 {
		payload["reply_message_id"] = *replyMessageID
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("sharecrm: marshal send request: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		token, err := c.accessToken(ctx, base, appID, appSecret)
		if err != nil {
			return "", fmt.Errorf("sharecrm: access token: %w", err)
		}
		msgID, err := c.postSend(ctx, base, token, body)
		if err == nil {
			return msgID, nil
		}
		lastErr = err
		if errors.Is(err, errUnauthorized) && attempt == 0 {
			c.invalidate(appID)
			continue
		}
		return "", err
	}
	return "", lastErr
}

func (c *Client) postSend(ctx context.Context, base, token string, body []byte) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+sendMessagePath, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("sharecrm: build send request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("sharecrm: send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("sharecrm: read send response: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return "", errUnauthorized
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("sharecrm: send http %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	var env gatewayEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return "", fmt.Errorf("sharecrm: decode send envelope: %w", err)
	}
	switch env.Code {
	case codeOK:
		var data sendMessageData
		if len(env.Data) > 0 {
			_ = json.Unmarshal(env.Data, &data)
		}
		return data.MessageID, nil
	case codeTokenInvalid, codeTokenExpired:
		return "", errUnauthorized
	case codeBotNotConnected:
		return "", errBotNotConnected
	default:
		return "", fmt.Errorf("sharecrm: send code=%d msg=%q", env.Code, env.Msg)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
