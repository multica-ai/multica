// Package claudemanagedruntime bridges Multica's daemon task protocol to the Claude Managed Agents sessions API.
package claudemanagedruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultAnthropicURL is the Claude Managed Agents API base.
const DefaultAnthropicURL = "https://api.anthropic.com/v1"

type api struct {
	base, token string
	headers     map[string]string
	client      *http.Client
}
type apiError struct {
	Status int
	// RetryAfter is the server's Retry-After hint; zero when absent or unparsable.
	RetryAfter time.Duration
	// WorkspaceKeyRequired means Anthropic asked for anthropic-workspace-id,
	// i.e. the key is not workspace-scoped.
	WorkspaceKeyRequired bool
}

func (e *apiError) Error() string {
	if e.WorkspaceKeyRequired {
		return fmt.Sprintf("remote API returned HTTP %d: a workspace-scoped Anthropic API key is required", e.Status)
	}
	return fmt.Sprintf("remote API returned HTTP %d", e.Status)
}

// requiresWorkspaceHeader inspects an error body without ever returning it.
func requiresWorkspaceHeader(body io.Reader) bool {
	var envelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	b, _ := io.ReadAll(io.LimitReader(body, 64<<10))
	return json.Unmarshal(b, &envelope) == nil && strings.Contains(strings.ToLower(envelope.Error.Message), "anthropic-workspace-id")
}

const defaultRetryDelay = 30 * time.Second

// retryDelay honours a remote Retry-After hint and otherwise backs off by a fixed delay.
func retryDelay(err error) time.Duration {
	var ae *apiError
	if errors.As(err, &ae) && ae.RetryAfter > 0 {
		return ae.RetryAfter
	}
	return defaultRetryDelay
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
		return 0
	}
	if at, err := http.ParseTime(value); err == nil && at.After(now) {
		return at.Sub(now)
	}
	return 0
}

// newAPI builds the Multica client; a non-empty workspace is sent as X-Workspace-ID.
func newAPI(base, token, workspace string) (*api, error) {
	headers := map[string]string{}
	if workspace != "" {
		headers["X-Workspace-ID"] = workspace
	}
	return newClient(base, token, headers)
}

// newAnthropicAPI builds the Claude Managed Agents client. It requires a
// workspace-scoped API key; anthropic-workspace-id is deliberately unsupported.
func newAnthropicAPI(base, token string) (*api, error) {
	return newClient(base, token, map[string]string{"anthropic-version": "2023-06-01", "anthropic-beta": "managed-agents-2026-04-01"})
}

func newClient(base, token string, headers map[string]string) (*api, error) {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("invalid API base URL")
	}
	if u.Scheme == "http" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1" {
		return nil, fmt.Errorf("API base URL must use HTTPS except on loopback")
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("API token is required")
	}
	return &api{base: strings.TrimRight(base, "/"), token: token, headers: headers, client: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

// Errors deliberately omit remote bodies and URLs, which may echo credentials or task content.
func (a *api) call(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.base+path, body)
	if err != nil {
		return fmt.Errorf("build API request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	for name, value := range a.headers {
		req.Header.Set(name, value)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("remote API transport failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &apiError{
			Status:               resp.StatusCode,
			RetryAfter:           parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()),
			WorkspaceKeyRequired: resp.StatusCode >= 400 && resp.StatusCode < 500 && requiresWorkspaceHeader(resp.Body),
		}
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20+1))
	if err != nil {
		return fmt.Errorf("read API response failed")
	}
	if len(b) > 8<<20 {
		return fmt.Errorf("API response exceeds 8 MiB")
	}
	if out != nil {
		if err := json.Unmarshal(b, out); err != nil {
			return fmt.Errorf("invalid API JSON response")
		}
	}
	return nil
}
