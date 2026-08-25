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
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	defaultTimeout      = 3 * time.Second
	maxTimeout          = 5 * time.Second
	maxResponseBodySize = 64 << 10
	minServiceTokenSize = 32
)

var ErrInvalidConfig = errors.New("seat capacity: invalid configuration")

type Config struct {
	Enabled      bool
	BaseURL      string
	ServiceToken string
	Timeout      time.Duration
	HTTPClient   *http.Client
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
	Enabled() bool
	ReserveInvitation(context.Context, uuid.UUID, uuid.UUID, time.Time) (Decision, error)
	ClaimShareJoin(context.Context, uuid.UUID, uuid.UUID) (Decision, error)
	Consume(context.Context, uuid.UUID, uuid.UUID) (Decision, error)
	Confirm(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (Decision, error)
	Release(context.Context, uuid.UUID, uuid.UUID) (Decision, error)
	ReleaseMember(context.Context, uuid.UUID, uuid.UUID) (Decision, error)
	GetOperation(context.Context, uuid.UUID, uuid.UUID) (Decision, error)
}

type unavailableExecutor struct{ err error }

// NewUnavailable preserves fail-closed behavior when an operator explicitly
// enabled capacity enforcement with invalid configuration.
func NewUnavailable(err error) Executor { return &unavailableExecutor{err: err} }

func (u *unavailableExecutor) Enabled() bool { return true }
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
	enabled      bool
	baseURL      *url.URL
	serviceToken string
	timeout      time.Duration
	httpClient   *http.Client
}

var _ Executor = (*Client)(nil)

func New(cfg Config) (*Client, error) {
	if !cfg.Enabled {
		return &Client{}, nil
	}
	rawURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	baseURL, err := url.Parse(rawURL)
	if err != nil || (baseURL.Scheme != "http" && baseURL.Scheme != "https") || baseURL.Host == "" ||
		baseURL.User != nil || baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return nil, fmt.Errorf("%w: base URL must be absolute and contain no credentials, query, or fragment", ErrInvalidConfig)
	}
	if cfg.ServiceToken != strings.TrimSpace(cfg.ServiceToken) || strings.ContainsAny(cfg.ServiceToken, " \t\r\n") || len(cfg.ServiceToken) < minServiceTokenSize {
		return nil, fmt.Errorf("%w: service token must contain at least %d non-whitespace bytes", ErrInvalidConfig, minServiceTokenSize)
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	if timeout < 0 || timeout > maxTimeout {
		return nil, fmt.Errorf("%w: timeout must be positive and at most %s", ErrInvalidConfig, maxTimeout)
	}
	httpClient := &http.Client{}
	if cfg.HTTPClient != nil {
		clone := *cfg.HTTPClient
		httpClient = &clone
	}
	// The machine credential must never cross an HTTP redirect boundary.
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{
		enabled:      true,
		baseURL:      baseURL,
		serviceToken: cfg.ServiceToken,
		timeout:      timeout,
		httpClient:   httpClient,
	}, nil
}

func (c *Client) Enabled() bool { return c != nil && c.enabled }

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
	if !c.Enabled() {
		return Decision{Allowed: true}, nil
	}
	if workspaceID == uuid.Nil {
		return Decision{}, fmt.Errorf("seat capacity: workspace ID is required")
	}
	u := *c.baseURL
	u.Path = strings.TrimRight(c.baseURL.Path, "/") + "/api/v1/internal/subscriptions/" + workspaceID.String() + "/capacity/" + suffix

	requestCtx, cancel := context.WithTimeout(ctx, c.timeout)
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
	req.Header.Set("Authorization", "Bearer "+c.serviceToken)
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
		return Decision{}, &HTTPError{StatusCode: resp.StatusCode, Code: remote.Code, Message: remote.Error}
	}
	var out Decision
	if err := json.Unmarshal(payload, &out); err != nil {
		return Decision{}, fmt.Errorf("seat capacity response decode failed: %w", err)
	}
	return out, nil
}

type HTTPError struct {
	StatusCode int
	Code       string
	Message    string
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
