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

// RequestRecorder is the small interface the client uses to instrument every
// outbound request without taking a hard dependency on the metrics package.
// A nil recorder is safely no-op'd.
type RequestRecorder interface {
	RecordCloudRuntimeRequest(op, status string, durationSeconds float64)
}

type Config struct {
	BaseURL    string
	Timeout    time.Duration
	HTTPClient *http.Client
	// Recorder, when non-nil, receives one observation per Do() call with
	// the inferred op, status bucket, and elapsed time. Production wires
	// this to the BusinessMetrics collector; tests leave it nil.
	Recorder RequestRecorder
}

type Request struct {
	Method    string
	Path      string
	Query     url.Values
	Body      []byte
	UserID    string
	RequestID string

	// Op is the high-level operation label fed to the request metric (e.g.
	// "provision", "terminate", "status", "gateway"). Empty defaults to a
	// path-derived bucket — useful for ad-hoc proxy calls that don't have
	// an obvious symbolic name.
	Op string

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
	recorder   RequestRecorder
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
		recorder:   cfg.Recorder,
	}
}

// SetRecorder lets callers attach a metrics recorder after construction —
// useful for handler.New which builds the cloudruntime client before main.go
// has decided whether the metrics listener is enabled.
func (c *Client) SetRecorder(r RequestRecorder) {
	if c == nil {
		return
	}
	c.recorder = r
}

func (c *Client) Enabled() bool {
	return c != nil && c.baseURL != ""
}

func (c *Client) Do(ctx context.Context, req Request) (*Response, error) {
	if c == nil || c.baseURL == "" {
		return nil, ErrDisabled
	}

	op := inferCloudRuntimeOp(req.Op, req.Method, req.Path)
	start := time.Now()
	resp, err := c.doInner(ctx, req)
	if c.recorder != nil {
		status := requestStatusBucket(resp, err)
		c.recorder.RecordCloudRuntimeRequest(op, status, time.Since(start).Seconds())
	}
	return resp, err
}

func (c *Client) doInner(ctx context.Context, req Request) (*Response, error) {
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

// inferCloudRuntimeOp returns the symbolic op label for the request metric.
// Callers may pin Request.Op explicitly; otherwise the bucket is derived
// from the path so existing call sites don't need to change.
func inferCloudRuntimeOp(op, method, path string) string {
	op = strings.ToLower(strings.TrimSpace(op))
	if op != "" {
		return op
	}
	switch {
	case strings.Contains(path, "/billing"):
		return "billing"
	case strings.Contains(path, "/gateway") || strings.Contains(path, "/proxy"):
		return "gateway"
	case strings.Contains(path, "/exec"):
		return "gateway"
	case strings.Contains(path, "/start") || strings.Contains(path, "/provision") || strings.Contains(path, "/nodes/create"):
		return "provision"
	case strings.Contains(path, "/stop") || strings.Contains(path, "/terminate") || strings.Contains(path, "/reboot"):
		return "terminate"
	case strings.Contains(path, "/status") || strings.Contains(path, "/health") || strings.Contains(path, "/ready"):
		return "status"
	case strings.Contains(path, "/nodes"):
		switch strings.ToUpper(strings.TrimSpace(method)) {
		case "POST":
			return "provision"
		case "DELETE":
			return "terminate"
		default:
			return "status"
		}
	}
	return "fleet"
}

// requestStatusBucket maps the (response, error) pair into the fixed metric
// status enum {ok, 4xx, 5xx, timeout, error}.
func requestStatusBucket(resp *Response, err error) string {
	if err != nil {
		// context.DeadlineExceeded surfaces as net.Error.Timeout(); we
		// keep the substring check loose so wrapped timeouts still bucket.
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline exceeded") {
			return "timeout"
		}
		return "error"
	}
	if resp == nil {
		return "error"
	}
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 400:
		return "ok"
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		return "4xx"
	case resp.StatusCode >= 500 && resp.StatusCode < 600:
		return "5xx"
	}
	return "error"
}
