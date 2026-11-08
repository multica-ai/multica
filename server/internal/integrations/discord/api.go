package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// This file is the minimal Discord REST surface the adapter needs: GET
// /users/@me (install-time validation and bot identity) and POST
// /channels/{id}/messages (outbound). It is a thin hand-rolled client over
// net/http — the REST API is plain JSON-over-HTTPS, so no SDK dependency is
// warranted. The Gateway (inbound) lives in gateway.go.

// defaultAPIBase is the production REST API root. Tests point base at an
// httptest server.
const defaultAPIBase = "https://discord.com/api/v10"

// requestError deliberately omits the request URL from Error(): transport
// errors can echo the URL, and Discord puts the bot token in the Authorization
// header (not the URL), but keeping the URL out of the message is the same
// credential-hygiene rule the Telegram client follows.
type requestError struct {
	method string
	cause  error
}

func (e *requestError) Error() string { return fmt.Sprintf("discord: %s request failed", e.method) }

func (e *requestError) Unwrap() error { return e.cause }

// apiError is a non-2xx Discord response.
type apiError struct {
	Status int
	Code   int // Discord's JSON error code
	Body   string
	// RetryAfter is Discord's mandated backoff on a 429.
	RetryAfter time.Duration
}

func (e *apiError) Error() string {
	return fmt.Sprintf("discord api: %d %s", e.Status, e.Body)
}

// isUnauthorized reports whether Discord rejected the bot token.
func isUnauthorized(err error) bool {
	var ae *apiError
	return errors.As(err, &ae) && ae.Status == http.StatusUnauthorized
}

// apiClient is one bot's REST client.
type apiClient struct {
	base  string // API root, no trailing slash
	token string
	http  *http.Client
}

func newAPIClient(base, token string, client *http.Client) *apiClient {
	if base == "" {
		base = defaultAPIBase
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &apiClient{base: strings.TrimRight(base, "/"), token: token, http: client}
}

// DiscordUser is the subset of a user object the adapter reads.
type DiscordUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Bot      bool   `json:"bot"`
}

// GetCurrentUser returns the bot user the token belongs to.
func (c *apiClient) GetCurrentUser(ctx context.Context) (DiscordUser, error) {
	var out DiscordUser
	if err := c.call(ctx, http.MethodGet, "/users/@me", nil, &out); err != nil {
		return DiscordUser{}, err
	}
	return out, nil
}

// SendMessage posts a plain-text message into a channel and returns its id.
// replyTo, when set, sends the message as a reply to that message id.
func (c *apiClient) SendMessage(ctx context.Context, channelID, content, replyTo string) (string, error) {
	if channelID == "" {
		return "", errors.New("discord: send message without channel id")
	}
	body := map[string]any{"content": content}
	if replyTo != "" {
		body["message_reference"] = map[string]any{"message_id": replyTo}
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := c.call(ctx, http.MethodPost, "/channels/"+channelID+"/messages", body, &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", errors.New("discord: send message returned no id")
	}
	return out.ID, nil
}

// call performs one REST request. A 2xx response is decoded into out when out
// is non-nil. Non-2xx responses become *apiError.
func (c *apiClient) call(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("discord: encode %s body: %w", path, err)
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return fmt.Errorf("discord: build %s request: %w", path, err)
	}
	req.Header.Set("Authorization", "Bot "+c.token)
	req.Header.Set("User-Agent", "DiscordBot (https://multica.ai, 1.0)")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return &requestError{method: method + " " + path, cause: err}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return &requestError{method: method + " " + path, cause: err}
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return newAPIError(resp, raw)
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("discord: decode %s response: %w", path, err)
	}
	return nil
}

// newAPIError builds the typed error for a non-2xx response, reading the 429
// backoff from the body (retry_after, seconds) or the Retry-After header.
func newAPIError(resp *http.Response, raw []byte) error {
	e := &apiError{Status: resp.StatusCode, Body: strings.TrimSpace(string(raw))}
	var payload struct {
		Code       int     `json:"code"`
		Message    string  `json:"message"`
		RetryAfter float64 `json:"retry_after"`
	}
	if json.Unmarshal(raw, &payload) == nil {
		e.Code = payload.Code
		if payload.Message != "" {
			e.Body = payload.Message
		}
		if payload.RetryAfter > 0 {
			e.RetryAfter = time.Duration(payload.RetryAfter * float64(time.Second))
		}
	}
	if e.RetryAfter == 0 {
		if secs, err := strconv.ParseFloat(resp.Header.Get("Retry-After"), 64); err == nil && secs > 0 {
			e.RetryAfter = time.Duration(secs * float64(time.Second))
		}
	}
	if e.Body == "" {
		e.Body = resp.Status
	}
	return e
}
