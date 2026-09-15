// Package qoderruntime bridges Multica's daemon task protocol to Qoder's managed sessions API.
package qoderruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type api struct {
	base, token, workspace string
	client                 *http.Client
}
type apiError struct{ Status int }

func (e *apiError) Error() string { return fmt.Sprintf("remote API returned HTTP %d", e.Status) }

func newAPI(base, token, workspace string) (*api, error) {
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
	return &api{base: strings.TrimRight(base, "/"), token: token, workspace: workspace, client: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
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
	if a.workspace != "" {
		req.Header.Set("X-Workspace-ID", a.workspace)
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
		return &apiError{resp.StatusCode}
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
