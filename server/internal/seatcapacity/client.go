// Package seatcapacity is the product-side executor for Multica Cloud's
// pre-purchased workspace-seat protocol.
package seatcapacity

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

	"github.com/google/uuid"
)

const (
	defaultTimeout       = 3 * time.Second
	maxResponseBodySize  = 64 << 10
	rateLimitScopeHeader = "X-Multica-RateLimit-Scope"

	RateLimitScopeGlobal    = "global"
	RateLimitScopeWorkspace = "workspace"
)

var ErrInvalidConfig = errors.New("seat capacity: invalid configuration")

type Config struct {
	BaseURL    string
	HTTPClient *http.Client
}

type Capacity struct {
	PurchasedSeats int   `json:"purchased_seats"`
	UsedSeats      int   `json:"used_seats"`
	ReservedSeats  int   `json:"reserved_seats"`
	Version        int64 `json:"version"`
}

type Operation struct {
	Token       uuid.UUID  `json:"token"`
	WorkspaceID uuid.UUID  `json:"workspace_id"`
	Kind        string     `json:"kind"`
	SubjectID   uuid.UUID  `json:"subject_id"`
	State       string     `json:"state"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

type Decision struct {
	Managed   bool       `json:"managed"`
	Allowed   bool       `json:"allowed"`
	Reason    string     `json:"reason,omitempty"`
	Operation *Operation `json:"operation,omitempty"`
	Capacity  *Capacity  `json:"capacity,omitempty"`
}

type Executor interface {
	// RecoveryAvailable reports whether this executor may settle durable
	// product-side intents. Implementations and decorators must forward this
	// capability explicitly, so an unavailable executor cannot be hidden by a
	// wrapper and accidentally start the recovery worker.
	RecoveryAvailable() bool
	ReserveInvitation(context.Context, uuid.UUID, uuid.UUID, time.Time) (Decision, error)
	ClaimShareJoin(context.Context, uuid.UUID, uuid.UUID) (Decision, error)
	Consume(context.Context, uuid.UUID, uuid.UUID) (Decision, error)
	Confirm(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (Decision, error)
	Release(context.Context, uuid.UUID, uuid.UUID) (Decision, error)
	ReleaseMember(context.Context, uuid.UUID, uuid.UUID) (Decision, error)
	GetOperation(context.Context, uuid.UUID, uuid.UUID) (Decision, error)
}

type unavailableExecutor struct{ err error }

// NewUnavailable preserves fail-closed behavior when a Cloud-connected
// deployment has invalid connection configuration.
func NewUnavailable(err error) Executor { return &unavailableExecutor{err: err} }

// CanRunWorker reports whether executor can safely settle durable intents.
func CanRunWorker(executor Executor) bool {
	return executor != nil && executor.RecoveryAvailable()
}

func (*unavailableExecutor) RecoveryAvailable() bool { return false }

func (u *unavailableExecutor) fail() (Decision, error) {
	return Decision{}, fmt.Errorf("seat capacity executor unavailable: %w", u.err)
}
func (u *unavailableExecutor) ReserveInvitation(context.Context, uuid.UUID, uuid.UUID, time.Time) (Decision, error) {
	return u.fail()
}
func (u *unavailableExecutor) ClaimShareJoin(context.Context, uuid.UUID, uuid.UUID) (Decision, error) {
	return u.fail()
}
func (u *unavailableExecutor) Consume(context.Context, uuid.UUID, uuid.UUID) (Decision, error) {
	return u.fail()
}
func (u *unavailableExecutor) Confirm(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (Decision, error) {
	return u.fail()
}
func (u *unavailableExecutor) Release(context.Context, uuid.UUID, uuid.UUID) (Decision, error) {
	return u.fail()
}
func (u *unavailableExecutor) ReleaseMember(context.Context, uuid.UUID, uuid.UUID) (Decision, error) {
	return u.fail()
}
func (u *unavailableExecutor) GetOperation(context.Context, uuid.UUID, uuid.UUID) (Decision, error) {
	return u.fail()
}

type Client struct {
	baseURL    *url.URL
	httpClient *http.Client
}

var _ Executor = (*Client)(nil)

func (*Client) RecoveryAvailable() bool { return true }

func New(cfg Config) (Executor, error) {
	rawURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if rawURL == "" {
		return nil, nil
	}
	baseURL, err := url.Parse(rawURL)
	if err != nil || (baseURL.Scheme != "http" && baseURL.Scheme != "https") || baseURL.Host == "" ||
		baseURL.User != nil || baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return nil, fmt.Errorf("%w: base URL must be absolute and contain no credentials, query, or fragment", ErrInvalidConfig)
	}
	httpClient := &http.Client{}
	if cfg.HTTPClient != nil {
		clone := *cfg.HTTPClient
		httpClient = &clone
	}
	// Internal Cloud calls must not follow a redirect to another origin.
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{
		baseURL:    baseURL,
		httpClient: httpClient,
	}, nil
}

func (c *Client) ReserveInvitation(ctx context.Context, workspaceID, invitationID uuid.UUID, expiresAt time.Time) (Decision, error) {
	return c.post(ctx, workspaceID, "reserve", map[string]any{
		"token": invitationID, "kind": "invitation", "subject_id": invitationID, "expires_at": expiresAt,
	})
}

func (c *Client) ClaimShareJoin(ctx context.Context, workspaceID, intentID uuid.UUID) (Decision, error) {
	return c.post(ctx, workspaceID, "claim", map[string]any{
		"token": intentID, "kind": "share_join", "subject_id": intentID,
	})
}

func (c *Client) Consume(ctx context.Context, workspaceID, token uuid.UUID) (Decision, error) {
	return c.post(ctx, workspaceID, "consume", map[string]any{"token": token})
}

func (c *Client) Confirm(ctx context.Context, workspaceID, token, memberID uuid.UUID) (Decision, error) {
	return c.post(ctx, workspaceID, "confirm", map[string]any{"token": token, "member_id": memberID})
}

func (c *Client) Release(ctx context.Context, workspaceID, token uuid.UUID) (Decision, error) {
	return c.post(ctx, workspaceID, "release", map[string]any{"token": token})
}

func (c *Client) ReleaseMember(ctx context.Context, workspaceID, memberID uuid.UUID) (Decision, error) {
	return c.post(ctx, workspaceID, "release-member", map[string]any{"member_id": memberID})
}

func (c *Client) GetOperation(ctx context.Context, workspaceID, token uuid.UUID) (Decision, error) {
	return c.do(ctx, http.MethodGet, workspaceID, "operations/"+token.String(), nil)
}

func (c *Client) post(ctx context.Context, workspaceID uuid.UUID, action string, value any) (Decision, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return Decision{}, err
	}
	return c.do(ctx, http.MethodPost, workspaceID, action, body)
}

func (c *Client) do(ctx context.Context, method string, workspaceID uuid.UUID, suffix string, body []byte) (Decision, error) {
	if workspaceID == uuid.Nil {
		return Decision{}, fmt.Errorf("seat capacity: workspace ID is required")
	}
	u := *c.baseURL
	u.Path = strings.TrimRight(c.baseURL.Path, "/") + "/api/v1/internal/subscriptions/" + workspaceID.String() + "/capacity/" + suffix

	requestCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()
	var reader io.Reader
	if len(body) > 0 {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(requestCtx, method, u.String(), reader)
	if err != nil {
		return Decision{}, err
	}
	req.Header.Set("Accept", "application/json")
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Decision{}, fmt.Errorf("seat capacity request failed: %w", err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodySize+1))
	if err != nil {
		return Decision{}, fmt.Errorf("seat capacity response read failed: %w", err)
	}
	if len(payload) > maxResponseBodySize {
		return Decision{}, fmt.Errorf("seat capacity response exceeded %d bytes", maxResponseBodySize)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var remote struct {
			Error string `json:"error"`
			Code  string `json:"code"`
		}
		_ = json.Unmarshal(payload, &remote)
		return Decision{}, &HTTPError{
			StatusCode:     resp.StatusCode,
			Code:           remote.Code,
			Message:        remote.Error,
			RetryAfter:     retryAfterDuration(resp.Header.Get("Retry-After")),
			RateLimitScope: normalizedRateLimitScope(resp.Header.Get(rateLimitScopeHeader)),
		}
	}
	var out Decision
	if err := json.Unmarshal(payload, &out); err != nil {
		return Decision{}, fmt.Errorf("seat capacity response decode failed: %w", err)
	}
	return out, nil
}

type HTTPError struct {
	StatusCode     int
	Code           string
	Message        string
	RetryAfter     time.Duration
	RateLimitScope string
}

func (e *HTTPError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("seat capacity request returned %d (%s)", e.StatusCode, e.Code)
	}
	return fmt.Sprintf("seat capacity request returned %d", e.StatusCode)
}

func IsNotFound(err error) bool {
	var remote *HTTPError
	return errors.As(err, &remote) && remote.StatusCode == http.StatusNotFound
}

func IsCapacityOvercommitted(err error) bool {
	var remote *HTTPError
	return errors.As(err, &remote) && remote.StatusCode == http.StatusConflict && remote.Code == "capacity_overcommitted"
}

func IsRateLimited(err error) bool {
	var remote *HTTPError
	// A proxy, ingress, or WAF may generate the 429 before the request reaches
	// Cloud and therefore cannot attach Cloud's JSON error code. HTTP 429 is
	// sufficient to preserve the retryable semantics and Retry-After value.
	return errors.As(err, &remote) && remote.StatusCode == http.StatusTooManyRequests
}

func RateLimitRetryAfter(err error) time.Duration {
	var remote *HTTPError
	if !errors.As(err, &remote) || !IsRateLimited(remote) {
		return 0
	}
	return remote.RetryAfter
}

// RateLimitScopeOf returns a trusted Cloud scope hint. Proxy-generated 429s
// normally have no scope and remain conservatively global to the caller.
func RateLimitScopeOf(err error) string {
	var remote *HTTPError
	if !errors.As(err, &remote) || !IsRateLimited(remote) {
		return ""
	}
	return normalizedRateLimitScope(remote.RateLimitScope)
}

func normalizedRateLimitScope(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case RateLimitScopeGlobal:
		return RateLimitScopeGlobal
	case RateLimitScopeWorkspace:
		return RateLimitScopeWorkspace
	default:
		return ""
	}
}

func retryAfterDuration(value string) time.Duration {
	return retryAfterDurationAt(value, time.Now())
}

func retryAfterDurationAt(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err == nil {
		if seconds < 1 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}
	retryAt, err := http.ParseTime(value)
	if err != nil {
		return 0
	}
	delay := retryAt.Sub(now)
	if delay <= 0 {
		return 0
	}
	return delay
}
