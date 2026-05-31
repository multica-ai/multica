package cloudruntime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultTimeout      = 35 * time.Second
	maxResponseBodySize = 1 << 20
)

var (
	ErrDisabled       = errors.New("cloud runtime fleet URL is not configured")
	ErrInvalidBaseURL = errors.New("cloud runtime fleet URL is invalid")
)

type Config struct {
	BaseURL    string
	Timeout    time.Duration
	HTTPClient *http.Client
}

type Request struct {
	Method    string
	Path      string
	Query     url.Values
	Body      []byte
	UserID    string
	RequestID string

	// Headers carries arbitrary outbound headers that the caller wants
	// forwarded verbatim. They are applied AFTER the client's defaults
	// (Accept, Content-Type, X-User-ID, X-Request-ID) so a caller
	// supplying any of those overrides them — useful when proxying a
	// request whose Content-Type is not application/json or whose
	// signed body must not be touched (e.g. the Stripe webhook
	// passthrough preserving Stripe-Signature alongside the raw body).
	//
	// Nil / empty is the common case; existing call sites can ignore
	// this field.
	Headers http.Header
}

type Response struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(cfg Config) *Client {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	}
	return &Client{
		baseURL:    strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		httpClient: httpClient,
	}
}

func (c *Client) Enabled() bool {
	return c != nil && c.baseURL != ""
}

func (c *Client) Do(ctx context.Context, req Request) (*Response, error) {
	if c == nil || c.baseURL == "" {
		return nil, ErrDisabled
	}

	base, err := url.Parse(c.baseURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return nil, fmt.Errorf("%w: %s", ErrInvalidBaseURL, c.baseURL)
	}
	if !strings.HasPrefix(req.Path, "/") {
		return nil, fmt.Errorf("cloud runtime path must start with /: %s", req.Path)
	}

	u := *base
	u.Path = strings.TrimRight(base.Path, "/") + req.Path
	u.RawQuery = req.Query.Encode()

	var body io.Reader
	if len(req.Body) > 0 {
		body = bytes.NewReader(req.Body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, u.String(), body)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Accept", "application/json")
	if len(req.Body) > 0 {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	// Caller-supplied headers go before the X-User-ID / X-Request-ID
	// stamps, since those are derived from authenticated context and
	// must not be overridable by the caller. Defaults (Accept,
	// Content-Type) ARE overridable — a webhook passthrough may need
	// to preserve a non-JSON Content-Type.
	for k, vs := range req.Headers {
		// Skip the headers we stamp authoritatively below. Canonicalize
		// once per key — http.CanonicalHeaderKey allocates on its
		// fast path so calling it twice per iteration would double
		// the per-request header overhead for no reason.
		canon := http.CanonicalHeaderKey(k)
		if canon == "X-User-Id" || canon == "X-Request-Id" {
			continue
		}
		httpReq.Header.Del(k)
		for _, v := range vs {
			httpReq.Header.Add(k, v)
		}
	}
	if req.UserID != "" {
		httpReq.Header.Set("X-User-ID", req.UserID)
	}
	if req.RequestID != "" {
		httpReq.Header.Set("X-Request-ID", req.RequestID)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodySize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxResponseBodySize {
		return nil, fmt.Errorf("cloud runtime response exceeds %d bytes", maxResponseBodySize)
	}
	return &Response{
		StatusCode: resp.StatusCode,
		Header:     resp.Header.Clone(),
		Body:       data,
	}, nil
}
