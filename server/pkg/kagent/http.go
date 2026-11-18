package kagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HTTPConfig supplies a qualified endpoint and caller identity. Token is evaluated
// per request so short-lived credentials can rotate. It must not log credentials.
// Client may supply a custom TLS root or authenticated RoundTripper.
type HTTPConfig struct {
	// AllowInsecureLoopback permits plaintext HTTP only to localhost or a literal
	// loopback IP for local development. HTTPS is required otherwise.
	AllowInsecureLoopback bool
	Endpoint              string
	Token                 func(context.Context) (string, error)
	Client                *http.Client
}

type wireClient struct {
	endpoint string
	token    func(context.Context) (string, error)
	client   *http.Client
}

func newWire(c HTTPConfig) (*wireClient, error) {
	u, err := url.Parse(c.Endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("kagent: invalid endpoint")
	}
	if u.Scheme == "http" {
		host := u.Hostname()
		ip := net.ParseIP(host)
		if !c.AllowInsecureLoopback || !(strings.EqualFold(host, "localhost") || (ip != nil && ip.IsLoopback())) {
			return nil, fmt.Errorf("kagent: HTTPS required (HTTP requires explicit loopback development opt-in)")
		}
	}
	client := http.Client{Timeout: 30 * time.Second}
	if c.Client != nil {
		client = *c.Client
	}
	if client.Timeout == 0 {
		client.Timeout = 30 * time.Second
	}
	// A redirect must never move a mutation or credential to another endpoint.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &wireClient{strings.TrimRight(c.Endpoint, "/"), c.Token, &client}, nil
}

type HTTPError struct{ Status int }

func (e *HTTPError) Error() string { return fmt.Sprintf("kagent: HTTP %d", e.Status) }

func (c *wireClient) do(ctx context.Context, method, path, contentType string, body, out any) error {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("kagent: encode request")
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint+path, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("kagent: build request")
	}
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if c.token != nil {
		token, err := c.token(ctx)
		if err != nil {
			return fmt.Errorf("kagent: obtain credential")
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("kagent: transport unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &HTTPError{resp.StatusCode}
	}
	if out == nil {
		return nil
	}
	const limit = 8 << 20
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || len(raw) > limit {
		return fmt.Errorf("kagent: incomplete or oversized response")
	}
	if uniqueJSON(raw) != nil {
		return fmt.Errorf("kagent: malformed response")
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("kagent: malformed response")
	}
	return nil
}
