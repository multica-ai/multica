// Package gitlab is a small client library for the GitLab REST API v4.
// Tokens are passed per call (never stored on the Client) so the caller can use
// per-user or per-workspace tokens interchangeably.
package gitlab

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const DefaultBaseURL = "https://gitlab.com"

// Client performs HTTP calls to a GitLab instance.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient constructs a Client with a given base URL and http.Client.
// Pass http.DefaultClient (or a timeout-bounded one) for production use.
func NewClient(baseURL string, hc *http.Client) *Client {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &Client{baseURL: baseURL, http: hc}
}

func (c *Client) get(ctx context.Context, token, path string, out any) error {
	return c.do(ctx, http.MethodGet, token, path, nil, out)
}

func (c *Client) do(ctx context.Context, method, token, path string, body, out any) error {
	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("gitlab: marshal body: %w", err)
		}
		reqBody = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+"/api/v4"+path, reqBody)
	if err != nil {
		return fmt.Errorf("gitlab: build request: %w", err)
	}
	req.Header.Set("PRIVATE-TOKEN", token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("gitlab: http do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out == nil {
			io.Copy(io.Discard, resp.Body)
			return nil
		}
		return json.NewDecoder(resp.Body).Decode(out)
	}

	// Non-2xx: classify.
	respBody, _ := io.ReadAll(resp.Body)
	var parsed struct {
		Message any `json:"message"`
	}
	_ = json.Unmarshal(respBody, &parsed)
	msg := formatGitlabMessage(parsed.Message)
	if msg == "" {
		msg = string(respBody)
	}

	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return fmt.Errorf("%w: %s", ErrUnauthorized, msg)
	case http.StatusForbidden:
		return fmt.Errorf("%w: %s", ErrForbidden, msg)
	case http.StatusNotFound:
		return fmt.Errorf("%w: %s", ErrNotFound, msg)
	default:
		return &APIError{StatusCode: resp.StatusCode, Message: msg}
	}
}

// formatGitlabMessage normalizes the `message` field of GitLab REST error
// responses to a human-readable string. The field is returned as either a
// plain string or an array of strings (for validation errors); we join the
// latter with "; ". Unknown shapes or nil yield "".
func formatGitlabMessage(m any) string {
	switch v := m.(type) {
	case nil:
		return ""
	case string:
		return v
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			parts = append(parts, fmt.Sprintf("%v", item))
		}
		return strings.Join(parts, "; ")
	default:
		return ""
	}
}
