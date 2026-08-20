package hztauth

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxResponseBytes = 1 << 20
const clientID = "multica"

var (
	ErrInvalidFlow      = errors.New("invalid HZT SSO flow")
	ErrExchangeRejected = errors.New("HZT SSO exchange rejected")
)

type Config struct {
	PublicURL    string
	InternalURL  string
	FrontendURL  string
	ClientSecret string
	FlowSecret   string
	Timeout      time.Duration
}

type Client struct {
	cfg        Config
	httpClient *http.Client
}

type Role struct {
	Slug string `json:"slug"`
}

type Identity struct {
	ID          string  `json:"id"`
	Username    string  `json:"username"`
	Email       *string `json:"email"`
	DisplayName string  `json:"displayName"`
	Role        string  `json:"role"`
	Roles       []Role  `json:"roles"`
}

type flowPayload struct {
	State    string `json:"state"`
	Verifier string `json:"verifier"`
	Next     string `json:"next,omitempty"`
	Expires  int64  `json:"expires"`
}

type Flow struct {
	State     string
	Verifier  string
	Challenge string
	Cookie    string
	Next      string
}

type tokenResponse struct {
	TokenType string   `json:"token_type"`
	ExpiresIn int      `json:"expires_in"`
	User      Identity `json:"user"`
}

func New(cfg Config) (*Client, error) {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	if strings.TrimSpace(cfg.InternalURL) == "" {
		cfg.InternalURL = cfg.PublicURL
	}
	for name, raw := range map[string]string{
		"public URL": cfg.PublicURL, "internal URL": cfg.InternalURL, "frontend URL": cfg.FrontendURL,
		"client secret": cfg.ClientSecret, "flow secret": cfg.FlowSecret,
	} {
		if strings.TrimSpace(raw) == "" {
			return nil, fmt.Errorf("HZT SSO %s is required", name)
		}
	}
	for name, raw := range map[string]string{"public URL": cfg.PublicURL, "internal URL": cfg.InternalURL, "frontend URL": cfg.FrontendURL} {
		parsed, err := url.Parse(raw)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
			return nil, fmt.Errorf("HZT SSO %s must be an absolute HTTP URL", name)
		}
	}
	if len(cfg.ClientSecret) < 32 || len(cfg.FlowSecret) < 32 {
		return nil, errors.New("HZT SSO client and flow secrets must contain at least 32 characters")
	}
	return &Client{cfg: cfg, httpClient: &http.Client{Timeout: cfg.Timeout}}, nil
}

func (c *Client) redirectURI() string {
	return strings.TrimRight(c.cfg.FrontendURL, "/") + "/auth/hzt/callback"
}

func randomBase64URL(bytesCount int) (string, error) {
	value := make([]byte, bytesCount)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func pkceChallenge(verifier string) string {
	digest := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func (c *Client) sign(payload string) string {
	mac := hmac.New(sha256.New, []byte(c.cfg.FlowSecret))
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (c *Client) NewFlow(next string, now time.Time) (Flow, error) {
	state, err := randomBase64URL(32)
	if err != nil {
		return Flow{}, err
	}
	verifier, err := randomBase64URL(48)
	if err != nil {
		return Flow{}, err
	}
	payloadBytes, err := json.Marshal(flowPayload{State: state, Verifier: verifier, Next: next, Expires: now.Add(10 * time.Minute).Unix()})
	if err != nil {
		return Flow{}, err
	}
	payload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	return Flow{
		State: state, Verifier: verifier, Challenge: pkceChallenge(verifier),
		Cookie: payload + "." + c.sign(payload), Next: next,
	}, nil
}

func (c *Client) ParseFlow(cookieValue, state string, now time.Time) (Flow, error) {
	payload, signature, ok := strings.Cut(cookieValue, ".")
	if !ok || !hmac.Equal([]byte(c.sign(payload)), []byte(signature)) {
		return Flow{}, ErrInvalidFlow
	}
	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return Flow{}, ErrInvalidFlow
	}
	var stored flowPayload
	if err := json.Unmarshal(decoded, &stored); err != nil || stored.Expires < now.Unix() || stored.State == "" || stored.Verifier == "" {
		return Flow{}, ErrInvalidFlow
	}
	if !hmac.Equal([]byte(stored.State), []byte(state)) {
		return Flow{}, ErrInvalidFlow
	}
	return Flow{State: stored.State, Verifier: stored.Verifier, Challenge: pkceChallenge(stored.Verifier), Cookie: cookieValue, Next: stored.Next}, nil
}

func (c *Client) AuthorizeURL(flow Flow) string {
	target := strings.TrimRight(c.cfg.PublicURL, "/") + "/api/sso/authorize"
	values := url.Values{
		"client_id":             {clientID},
		"redirect_uri":          {c.redirectURI()},
		"state":                 {flow.State},
		"code_challenge":        {flow.Challenge},
		"code_challenge_method": {"S256"},
	}
	return target + "?" + values.Encode()
}

func (c *Client) Exchange(ctx context.Context, code, verifier string) (Identity, error) {
	body, err := json.Marshal(map[string]string{
		"grant_type": "authorization_code", "client_id": clientID,
		"client_secret": c.cfg.ClientSecret, "code": code,
		"redirect_uri": c.redirectURI(), "code_verifier": verifier,
	})
	if err != nil {
		return Identity{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.cfg.InternalURL, "/")+"/api/sso/token", bytes.NewReader(body))
	if err != nil {
		return Identity{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Identity{}, fmt.Errorf("exchange HZT authorization code: %w", err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return Identity{}, fmt.Errorf("read HZT token response: %w", err)
	}
	if len(responseBody) > maxResponseBytes {
		return Identity{}, errors.New("HZT token response is too large")
	}
	if resp.StatusCode != http.StatusOK {
		return Identity{}, fmt.Errorf("%w: status %d", ErrExchangeRejected, resp.StatusCode)
	}
	var token tokenResponse
	if err := json.Unmarshal(responseBody, &token); err != nil {
		return Identity{}, fmt.Errorf("decode HZT token response: %w", err)
	}
	if token.TokenType != "Bearer" || token.User.ID == "" || token.User.Username == "" || token.User.Role == "" {
		return Identity{}, errors.New("HZT token response is incomplete")
	}
	return token.User, nil
}
