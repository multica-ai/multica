// Package memoryhub implements the Multica HTTP client for the MemoryHub /
// MemoryProxy instances. It owns the wire calls only; secret handling,
// binding policy, and claim-gate orchestration live in
// server/internal/service.
//
// The client never logs request or response bodies, never persists any
// credential, and classifies every error into a deterministic business code
// (401/402/403/404/409/422/429/5xx/timeout).
package memoryhub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// ErrorCode is a stable business error classification.
type ErrorCode string

const (
	ErrorUnauthorized     ErrorCode = "unauthorized"
	ErrorPaymentRequired  ErrorCode = "payment_required"
	ErrorForbidden        ErrorCode = "forbidden"
	ErrorNotFound         ErrorCode = "not_found"
	ErrorConflict         ErrorCode = "conflict"
	ErrorUnprocessable    ErrorCode = "unprocessable"
	ErrorRateLimited      ErrorCode = "rate_limited"
	ErrorUpstream         ErrorCode = "upstream_error"
	ErrorTimeout          ErrorCode = "timeout"
	ErrorNetwork          ErrorCode = "network"
	ErrorMalformedResponse ErrorCode = "malformed_response"
)

// Error is a classified MemoryHub client error.
type Error struct {
	Code    ErrorCode
	Status  int
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("memoryhub: %s (http %d): %s", e.Code, e.Status, e.Message)
}

// Unauthorized / Forbidden helpers used by the service layer to decide
// "blocked, do not retry".
func IsAuthFailure(err error) bool {
	var mhErr *Error
	if !errors.As(err, &mhErr) {
		return false
	}
	return mhErr.Code == ErrorUnauthorized || mhErr.Code == ErrorPaymentRequired || mhErr.Code == ErrorForbidden
}

// IsRetryable reports whether the failure may be retried with backoff.
func IsRetryable(err error) bool {
	var mhErr *Error
	if !errors.As(err, &mhErr) {
		return true // unknown transport failure: retry
	}
	switch mhErr.Code {
	case ErrorUpstream, ErrorTimeout, ErrorNetwork, ErrorRateLimited:
		return true
	default:
		return false
	}
}

// Options configures the client.
type Options struct {
	// BaseURL is the memory-core or memory-proxy origin (no trailing slash).
	BaseURL string
	// ServiceID is the x-tdai-service-id / space id used in request paths
	// such as /claude-code/{spaceId}/v1/messages.
	ServiceID string
	// UserKey is the owner-provided x-tdai-user-key. It is carried in the
	// request header only; it is never logged or persisted.
	UserKey string
	// HTTPClient may be overridden in tests; default is a 15s-timeout client.
	HTTPClient *http.Client
	// Logger may be supplied; the client logs only redacted facts (status,
	// code), never bodies or keys.
	Logger interface{ Logf(format string, args ...any) }
}

// Client is the MemoryHub HTTP client.
type Client struct {
	baseURL   string
	serviceID string
	userKey   string
	http      *http.Client
	logf      func(format string, args ...any)
}

// New builds a Client.
func New(opts Options) *Client {
	hc := opts.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	logf := func(string, ...any) {}
	if opts.Logger != nil {
		logf = opts.Logger.Logf
	}
	return &Client{
		baseURL:   opts.BaseURL,
		serviceID: opts.ServiceID,
		userKey:   opts.UserKey,
		http:      hc,
		logf:      logf,
	}
}

// Health probes /health and returns the raw health JSON plus status.
func (c *Client) Health(ctx context.Context) (json.RawMessage, error) {
	return c.do(ctx, http.MethodGet, "/health", nil, nil)
}

// VerifyUserKey verifies the configured user key via
// POST /v3/meta/auth/verify. A non-2xx response is classified.
func (c *Client) VerifyUserKey(ctx context.Context) error {
	_, err := c.do(ctx, http.MethodPost, "/v3/meta/auth/verify", nil, nil)
	return err
}

// FindOrCreateTeam finds-or-creates a remote team.
func (c *Client) FindOrCreateTeam(ctx context.Context, req FindOrCreateRequest) (*RemoteRef, error) {
	return c.findOrCreate(ctx, "/v3/meta/team", req)
}

// FindOrCreateAgent finds-or-creates a remote agent under a team.
func (c *Client) FindOrCreateAgent(ctx context.Context, req FindOrCreateRequest) (*RemoteRef, error) {
	return c.findOrCreate(ctx, "/v3/meta/agent", req)
}

// FindOrCreateTask finds-or-creates a remote task under an agent.
func (c *Client) FindOrCreateTask(ctx context.Context, req FindOrCreateRequest) (*RemoteRef, error) {
	return c.findOrCreate(ctx, "/v3/meta/task", req)
}

func (c *Client) findOrCreate(ctx context.Context, path string, req FindOrCreateRequest) (*RemoteRef, error) {
	var resp RemoteRef
	if _, err := c.do(ctx, http.MethodPost, path, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// FindRemote looks up an existing remote object by kind + remote id.
func (c *Client) FindRemote(ctx context.Context, kind, remoteID string) (*RemoteRef, error) {
	q := url.Values{}
	q.Set("kind", kind)
	if remoteID != "" {
		q.Set("remote_id", remoteID)
	}
	var resp RemoteRef
	if _, err := c.do(ctx, http.MethodGet, "/v3/meta/remote?"+q.Encode(), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// DeleteRemote deletes a remote object by id.
func (c *Client) DeleteRemote(ctx context.Context, remoteID string) error {
	body, _ := json.Marshal(map[string]string{"remote_id": remoteID})
	_, err := c.do(ctx, http.MethodDelete, "/v3/meta/remote", body, nil)
	return err
}

// do performs the request, classifies errors, and decodes a 2xx body when
// out is non-nil. Bodies are never logged.
func (c *Client) do(ctx context.Context, method, path string, in, out any) (json.RawMessage, error) {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return nil, fmt.Errorf("memoryhub: marshal request: %w", err)
		}
		body = bytes.NewReader(b)
	}

	u, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("memoryhub: parse base url: %w", err)
	}
	u.Path = joinPath(u.Path, path)

	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, fmt.Errorf("memoryhub: new request: %w", err)
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	// The MemoryHub instances identify the space via this header for the
	// proxy's /claude-code/{spaceId} shapes and via the service id for core.
	if c.serviceID != "" {
		req.Header.Set("x-tdai-service-id", c.serviceID)
	}
	if c.userKey != "" {
		req.Header.Set("x-tdai-user-key", c.userKey)
	}

	start := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, &Error{Code: ErrorTimeout, Status: 0, Message: "request timeout"}
		}
		c.logf("memoryhub: %s %s network error after %s", method, path, time.Since(start).Round(time.Millisecond))
		return nil, &Error{Code: ErrorNetwork, Status: 0, Message: err.Error()}
	}
	defer resp.Body.Close()

	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if readErr != nil {
		return nil, &Error{Code: ErrorNetwork, Status: resp.StatusCode, Message: readErr.Error()}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, classifyError(resp.StatusCode, raw)
	}

	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return nil, &Error{Code: ErrorMalformedResponse, Status: resp.StatusCode, Message: "malformed success body"}
		}
	}
	return raw, nil
}

func classifyError(status int, body []byte) error {
	// Extract only the error code field name; never the body text or values.
	msg := "upstream error"
	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil && envelope.Error.Code != "" {
		msg = envelope.Error.Code
	}
	switch status {
	case http.StatusUnauthorized:
		return &Error{Code: ErrorUnauthorized, Status: status, Message: msg}
	case http.StatusPaymentRequired:
		return &Error{Code: ErrorPaymentRequired, Status: status, Message: msg}
	case http.StatusForbidden:
		return &Error{Code: ErrorForbidden, Status: status, Message: msg}
	case http.StatusNotFound:
		return &Error{Code: ErrorNotFound, Status: status, Message: msg}
	case http.StatusConflict:
		return &Error{Code: ErrorConflict, Status: status, Message: msg}
	case http.StatusUnprocessableEntity:
		return &Error{Code: ErrorUnprocessable, Status: status, Message: msg}
	case http.StatusTooManyRequests:
		return &Error{Code: ErrorRateLimited, Status: status, Message: msg}
	default:
		return &Error{Code: ErrorUpstream, Status: status, Message: msg}
	}
}

func joinPath(base, p string) string {
	if base == "" {
		return p
	}
	return base + p
}

// FindOrCreateRequest is the find-or-create body.
type FindOrCreateRequest struct {
	Kind      string `json:"kind,omitempty"`
	Name      string `json:"name,omitempty"`
	TeamID    string `json:"team_id,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`
	RemoteID  string `json:"remote_id,omitempty"`
}

// RemoteRef is the remote identity returned by find-or-create / find.
type RemoteRef struct {
	TeamID string `json:"team_id,omitempty"`
	AgentID string `json:"agent_id,omitempty"`
	TaskID string `json:"task_id,omitempty"`
	Name   string `json:"name,omitempty"`
}
