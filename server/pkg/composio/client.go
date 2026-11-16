package composio

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

// DefaultBaseURL is the canonical Composio v3.1 REST root.
const DefaultBaseURL = "https://backend.composio.dev/api/v3.1"

// DefaultUserAgent is sent on every request unless overridden via [Options.UserAgent].
const DefaultUserAgent = "multica-composio-go/0.1"

// DefaultTimeout is the per-request timeout applied to the underlying
// resty client when [Options.Timeout] is zero.
const DefaultTimeout = 30 * time.Second

// Options configures a [Client]. Only APIKey is required.
type Options struct {
	// APIKey is the Composio project API key, sent as the `x-api-key` header.
	APIKey string

	// BaseURL overrides the API root. Mostly useful for tests against a
	// httptest.Server. Defaults to [DefaultBaseURL].
	BaseURL string

	// UserAgent overrides the User-Agent header. Defaults to [DefaultUserAgent].
	UserAgent string

	// Timeout is the per-request timeout. Zero means [DefaultTimeout].
	// A negative value disables the timeout entirely.
	Timeout time.Duration

	// HTTPClient lets callers inject a custom *http.Client (for example with
	// a corporate transport, custom CookieJar, redirect policy, or
	// observability instrumentation). When non-nil it is adopted in full
	// via resty.NewWithClient — the caller's Transport, Jar, CheckRedirect,
	// and built-in Timeout all carry through. If both this client's
	// Timeout and [Options.Timeout] are set, [Options.Timeout] wins.
	HTTPClient *http.Client

	// RetryCount is the number of retries resty performs on transient
	// failures. Zero means no retries (callers can layer their own).
	RetryCount int

	// RetryWaitTime is the base delay between retries when RetryCount > 0.
	RetryWaitTime time.Duration
}

// Client is the Composio REST client.
//
// It is safe for concurrent use by multiple goroutines.
type Client struct {
	rc        *resty.Client
	baseURL   string
	apiKey    string
	userAgent string
}

// NewClient constructs a Client from [Options]. It returns an error when the
// options are obviously broken (empty API key, malformed base URL).
func NewClient(opts Options) (*Client, error) {
	if strings.TrimSpace(opts.APIKey) == "" {
		return nil, errors.New("composio: APIKey is required")
	}

	baseURL := opts.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if _, err := url.Parse(baseURL); err != nil {
		return nil, fmt.Errorf("composio: invalid BaseURL %q: %w", baseURL, err)
	}
	baseURL = strings.TrimRight(baseURL, "/")

	ua := opts.UserAgent
	if ua == "" {
		ua = DefaultUserAgent
	}

	timeout := opts.Timeout
	switch {
	case timeout == 0:
		timeout = DefaultTimeout
	case timeout < 0:
		timeout = 0 // resty treats 0 as "no timeout"
	}

	rc := newRestyClient(opts.HTTPClient).
		SetBaseURL(baseURL).
		SetHeader("Content-Type", "application/json").
		SetHeader("Accept", "application/json").
		SetHeader("User-Agent", ua).
		SetHeader("x-api-key", opts.APIKey).
		SetTimeout(timeout)

	if opts.RetryCount > 0 {
		rc = rc.SetRetryCount(opts.RetryCount)
		if opts.RetryWaitTime > 0 {
			rc = rc.SetRetryWaitTime(opts.RetryWaitTime)
		}
	}

	return &Client{
		rc:        rc,
		baseURL:   baseURL,
		apiKey:    opts.APIKey,
		userAgent: ua,
	}, nil
}

// newRestyClient constructs a resty.Client honoring an injected *http.Client
// in full when one is provided. resty.NewWithClient adopts the caller's
// http.Client wholesale — so the caller's Transport, Jar, CheckRedirect,
// and Timeout all carry through — which matches the documented contract
// of [Options.HTTPClient].
func newRestyClient(hc *http.Client) *resty.Client {
	if hc != nil {
		return resty.NewWithClient(hc)
	}
	return resty.New()
}

// BaseURL returns the resolved API root after defaulting.
func (c *Client) BaseURL() string { return c.baseURL }

// APIKeyHeader returns the header pair callers should attach to MCP
// streaming clients or any other Composio request made outside the SDK.
//
// Returning a copy keeps the internal map immutable.
func (c *Client) APIKeyHeader() map[string]string {
	return map[string]string{"x-api-key": c.apiKey}
}

// newRequest returns a resty.Request bound to the given context.
// All endpoint methods funnel through this helper.
func (c *Client) newRequest(ctx context.Context) *resty.Request {
	return c.rc.R().SetContext(ctx)
}

// do executes a request and unmarshals a successful body into out.
// On non-2xx it returns a *APIError populated from the response body.
//
// out may be nil if the caller does not care about the body
// (e.g. DELETE / 204).
func (c *Client) do(req *resty.Request, method, path string, out any) error {
	if out != nil {
		req = req.SetResult(out)
	}
	resp, err := req.Execute(method, path)
	if err != nil {
		return fmt.Errorf("composio: %s %s: %w", method, path, err)
	}
	if resp.IsError() {
		return parseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}
