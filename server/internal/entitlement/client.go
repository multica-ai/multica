package entitlement

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/singleflight"
)

const (
	defaultRequestTimeout = 3 * time.Second
	maxRequestTimeout     = 3 * time.Second
	defaultMaxEntries     = 10_000
	defaultStaleGrace     = 15 * time.Minute
	defaultFailureRetry   = 5 * time.Second
	maxPolicyTTL          = 5 * time.Minute
	maxResponseBodySize   = 64 << 10
	minServiceTokenBytes  = 32
)

var (
	ErrInvalidConfig = errors.New("entitlement: invalid configuration")
	ErrInvalidPolicy = errors.New("entitlement: invalid policy response")
)

// Config has no environment-loading side effects. A zero Config is disabled,
// so self-hosted deployments and existing server construction stay unchanged.
// A future SaaS-only wiring change must explicitly set Enabled and provide the
// independent service credential issued for the Cloud policy endpoint.
type Config struct {
	Enabled      bool
	BaseURL      string
	ServiceToken string
	Timeout      time.Duration
	MaxEntries   int
	StaleGrace   time.Duration
	HTTPClient   *http.Client
	Observer     Observer
	Logger       *slog.Logger
}

// Client implements Provider with a bounded workspace cache and one in-flight
// refresh per workspace. It has no goroutines or background lifecycle.
type Client struct {
	enabled      bool
	baseURL      *url.URL
	serviceToken string
	timeout      time.Duration
	staleGrace   time.Duration
	httpClient   *http.Client
	observer     Observer
	logger       *slog.Logger
	now          func() time.Time
	cache        *policyCache
	refreshes    singleflight.Group
	emergencyOff atomic.Bool
}

var _ Provider = (*Client)(nil)

func New(cfg Config) (*Client, error) {
	if !cfg.Enabled {
		return &Client{observer: cfg.Observer, now: time.Now}, nil
	}

	rawBaseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	baseURL, err := url.Parse(rawBaseURL)
	if err != nil || (baseURL.Scheme != "http" && baseURL.Scheme != "https") || baseURL.Host == "" ||
		baseURL.User != nil || baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return nil, fmt.Errorf("%w: base URL must be an absolute URL without credentials, query, or fragment", ErrInvalidConfig)
	}
	if cfg.ServiceToken != strings.TrimSpace(cfg.ServiceToken) || strings.ContainsAny(cfg.ServiceToken, " \t\r\n") || len(cfg.ServiceToken) < minServiceTokenBytes {
		return nil, fmt.Errorf("%w: service token must contain at least %d non-whitespace bytes", ErrInvalidConfig, minServiceTokenBytes)
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = defaultRequestTimeout
	}
	if timeout < 0 || timeout > maxRequestTimeout {
		return nil, fmt.Errorf("%w: timeout must be positive and at most %s", ErrInvalidConfig, maxRequestTimeout)
	}
	maxEntries := cfg.MaxEntries
	if maxEntries == 0 {
		maxEntries = defaultMaxEntries
	}
	if maxEntries < 0 {
		return nil, fmt.Errorf("%w: max entries must be positive", ErrInvalidConfig)
	}
	staleGrace := cfg.StaleGrace
	if staleGrace == 0 {
		staleGrace = defaultStaleGrace
	}
	if staleGrace < 0 {
		return nil, fmt.Errorf("%w: stale grace must not be negative", ErrInvalidConfig)
	}
	httpClient := &http.Client{}
	if cfg.HTTPClient != nil {
		clone := *cfg.HTTPClient
		httpClient = &clone
	}
	// Never forward the independent service credential through an HTTP redirect.
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &Client{
		enabled:      true,
		baseURL:      baseURL,
		serviceToken: cfg.ServiceToken,
		timeout:      timeout,
		staleGrace:   staleGrace,
		httpClient:   httpClient,
		observer:     cfg.Observer,
		logger:       logger,
		now:          time.Now,
		cache:        newPolicyCache(maxEntries),
	}, nil
}

func (c *Client) Enabled() bool { return c != nil && c.enabled }

// SetEmergencyDisabled is a local, server-only down switch. It can suppress a
// Cloud action immediately, but can never promote off or observe to enforce.
func (c *Client) SetEmergencyDisabled(disabled bool) {
	if c != nil {
		c.emergencyOff.Store(disabled)
	}
}

func (c *Client) Gate(ctx context.Context, workspaceID uuid.UUID, name GateName) Decision {
	if !c.Enabled() {
		return c.recordDecision(name, offDecision(ReasonDisabled))
	}
	if c.emergencyOff.Load() {
		return c.recordDecision(name, offDecision(ReasonEmergencyDisabled))
	}
	if workspaceID == uuid.Nil {
		return c.recordDecision(name, offDecision(ReasonInvalidWorkspace))
	}
	if !name.valid() {
		return c.recordDecision(name, offDecision(ReasonUnknownGate))
	}

	now := c.now()
	cached, cachedOK := c.cache.get(workspaceID)
	if cachedOK && cached.hasPolicy && now.Before(cached.freshUntil) {
		c.recordCache("hit")
		return c.recordDecision(name, decisionFromEntry(cached, name, ReasonCacheFresh, false))
	}
	if cachedOK && now.Before(cached.retryAfter) {
		c.recordCache("retry_suppressed")
		if cached.hasPolicy && now.Before(cached.staleUntil) {
			return c.recordDecision(name, decisionFromEntry(cached, name, ReasonStale, true))
		}
		return c.recordDecision(name, offDecision(ReasonUnavailable))
	}
	if cachedOK && cached.hasPolicy {
		c.recordCache("expired")
	} else {
		c.recordCache("miss")
	}

	entry, refreshed, err := c.refresh(ctx, workspaceID)
	if err == nil {
		if c.emergencyOff.Load() {
			return c.recordDecision(name, offDecision(ReasonEmergencyDisabled))
		}
		reason := ReasonRefreshed
		if !refreshed {
			reason = ReasonCacheFresh
		}
		return c.recordDecision(name, decisionFromEntry(entry, name, reason, false))
	}

	if c.emergencyOff.Load() {
		return c.recordDecision(name, offDecision(ReasonEmergencyDisabled))
	}
	// A stale policy is safe only after enforcement is downgraded to observe.
	// Re-read the cache because another caller may have refreshed it while this
	// call was waiting in singleflight.
	now = c.now()
	if cached, ok := c.cache.get(workspaceID); ok && cached.hasPolicy && now.Before(cached.staleUntil) {
		return c.recordDecision(name, decisionFromEntry(cached, name, ReasonStale, true))
	}
	reason := ReasonUnavailable
	if errors.Is(err, ErrInvalidPolicy) {
		reason = ReasonInvalidPolicy
	}
	if errors.Is(err, errVersionRegression) {
		reason = ReasonVersionRegression
	}
	return c.recordDecision(name, offDecision(reason))
}

type refreshResult struct {
	entry     cacheEntry
	refreshed bool
}

func (c *Client) refresh(ctx context.Context, workspaceID uuid.UUID) (cacheEntry, bool, error) {
	// A refresh is shared by all callers for this workspace, so its lifetime
	// cannot belong to whichever request happens to enter singleflight first.
	// Keep request-scoped values, but let the independent fetch timeout bound it.
	refreshCtx := context.WithoutCancel(ctx)
	value, err, _ := c.refreshes.Do(workspaceID.String(), func() (any, error) {
		now := c.now()
		if cached, ok := c.cache.get(workspaceID); ok {
			if cached.hasPolicy && now.Before(cached.freshUntil) {
				return refreshResult{entry: cached}, nil
			}
			if now.Before(cached.retryAfter) {
				return nil, &fetchError{outcome: "retry_suppressed"}
			}
		}

		started := time.Now()
		policy, err := c.fetch(refreshCtx, workspaceID)
		if err != nil {
			c.cache.markFailure(workspaceID, c.now().Add(defaultFailureRetry))
			c.recordRefresh(refreshOutcome(err), time.Since(started))
			c.logger.DebugContext(refreshCtx, "entitlement policy refresh failed",
				"workspace_id", workspaceID.String(), "outcome", refreshOutcome(err))
			return nil, err
		}
		receivedAt := c.now()
		ttl := time.Duration(policy.validForSeconds) * time.Second
		entry := cacheEntry{
			hasPolicy:  true,
			policy:     policy.snapshot,
			freshUntil: receivedAt.Add(ttl),
			staleUntil: receivedAt.Add(ttl).Add(c.staleGrace),
		}
		stored, err := c.cache.put(workspaceID, entry, receivedAt)
		if err != nil {
			c.cache.markFailure(workspaceID, c.now().Add(defaultFailureRetry))
			var regression *revisionError
			source := "unknown"
			if errors.As(err, &regression) {
				source = regression.source
				if c.observer != nil {
					c.observer.RecordEntitlementVersionRegression(source)
				}
			}
			c.recordRefresh("version_regression", time.Since(started))
			c.logger.WarnContext(refreshCtx, "entitlement policy revision regressed",
				"workspace_id", workspaceID.String(), "source", source)
			return nil, err
		}
		c.recordRefresh("ok", time.Since(started))
		return refreshResult{entry: stored, refreshed: true}, nil
	})
	if err != nil {
		return cacheEntry{}, false, err
	}
	result := value.(refreshResult)
	return result.entry, result.refreshed, nil
}

type wirePolicy struct {
	SchemaVersion       int                 `json:"schema_version"`
	PolicyRevision      int64               `json:"policy_revision"`
	SubscriptionVersion int64               `json:"subscription_version"`
	ValidUntil          time.Time           `json:"valid_until"`
	ValidForSeconds     int64               `json:"valid_for_seconds"`
	Gates               map[string]wireGate `json:"gates"`
}

type wireGate struct {
	Action      string     `json:"action"`
	Limit       *int       `json:"limit"`
	PeriodStart *time.Time `json:"period_start"`
	PeriodEnd   *time.Time `json:"period_end"`
	ResetAt     *time.Time `json:"reset_at"`
}

type fetchedPolicy struct {
	snapshot        policySnapshot
	validForSeconds int64
}

func (c *Client) fetch(ctx context.Context, workspaceID uuid.UUID) (fetchedPolicy, error) {
	u := *c.baseURL
	u.Path = strings.TrimRight(c.baseURL.Path, "/") + "/api/v1/internal/entitlement-policies/" + workspaceID.String()
	u.RawQuery = ""
	u.Fragment = ""

	requestCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, u.String(), nil)
	if err != nil {
		return fetchedPolicy{}, &fetchError{outcome: "request"}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.serviceToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(requestCtx.Err(), context.DeadlineExceeded) {
			return fetchedPolicy{}, &fetchError{outcome: "timeout"}
		}
		return fetchedPolicy{}, &fetchError{outcome: "network"}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fetchedPolicy{}, &fetchError{outcome: statusOutcome(resp.StatusCode)}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodySize+1))
	if err != nil {
		return fetchedPolicy{}, &fetchError{outcome: "read"}
	}
	if len(body) > maxResponseBodySize {
		return fetchedPolicy{}, fmt.Errorf("%w: response exceeds %d bytes", ErrInvalidPolicy, maxResponseBodySize)
	}
	var wire wirePolicy
	if err := json.Unmarshal(body, &wire); err != nil {
		return fetchedPolicy{}, fmt.Errorf("%w: malformed JSON", ErrInvalidPolicy)
	}
	return normalizePolicy(wire)
}

func normalizePolicy(wire wirePolicy) (fetchedPolicy, error) {
	if wire.SchemaVersion != SchemaVersion || wire.PolicyRevision <= 0 || wire.SubscriptionVersion < 0 ||
		wire.ValidUntil.IsZero() || wire.ValidForSeconds <= 0 || wire.ValidForSeconds > int64(maxPolicyTTL/time.Second) {
		return fetchedPolicy{}, ErrInvalidPolicy
	}
	gates := make(map[GateName]Gate, 2)
	for _, name := range []GateName{GateIssueWindow, GateAutopilotRuns} {
		wireGate, ok := wire.Gates[string(name)]
		if !ok {
			return fetchedPolicy{}, ErrInvalidPolicy
		}
		gate, err := normalizeGate(name, wireGate)
		if err != nil {
			return fetchedPolicy{}, err
		}
		gates[name] = gate
	}
	return fetchedPolicy{
		snapshot: policySnapshot{
			policyRevision:      wire.PolicyRevision,
			subscriptionVersion: wire.SubscriptionVersion,
			cloudValidUntil:     wire.ValidUntil,
			gates:               gates,
		},
		validForSeconds: wire.ValidForSeconds,
	}, nil
}

func normalizeGate(name GateName, wire wireGate) (Gate, error) {
	action := Action(wire.Action)
	switch action {
	case ActionOff:
		return Gate{Action: ActionOff}, nil
	case ActionObserve, ActionEnforce:
	default:
		return Gate{}, ErrInvalidPolicy
	}
	if wire.Limit == nil || *wire.Limit < 0 {
		return Gate{}, ErrInvalidPolicy
	}
	periodFields := 0
	for _, value := range []*time.Time{wire.PeriodStart, wire.PeriodEnd, wire.ResetAt} {
		if value != nil {
			periodFields++
		}
	}
	if periodFields != 0 && periodFields != 3 {
		return Gate{}, ErrInvalidPolicy
	}
	if name == GateAutopilotRuns && periodFields != 3 {
		return Gate{}, ErrInvalidPolicy
	}
	if periodFields == 3 && (!wire.PeriodStart.Before(*wire.PeriodEnd) || !wire.PeriodStart.Before(*wire.ResetAt)) {
		return Gate{}, ErrInvalidPolicy
	}
	limit := *wire.Limit
	gate := Gate{Action: action, Limit: &limit}
	if periodFields == 3 {
		start, end, reset := *wire.PeriodStart, *wire.PeriodEnd, *wire.ResetAt
		gate.PeriodStart, gate.PeriodEnd, gate.ResetAt = &start, &end, &reset
	}
	return gate, nil
}

func decisionFromEntry(entry cacheEntry, name GateName, reason Reason, stale bool) Decision {
	gate := cloneGate(entry.policy.gates[name])
	if stale && gate.Action == ActionEnforce {
		gate.Action = ActionObserve
	}
	return Decision{
		Gate:                gate,
		Reason:              reason,
		PolicyRevision:      entry.policy.policyRevision,
		SubscriptionVersion: entry.policy.subscriptionVersion,
		CloudValidUntil:     entry.policy.cloudValidUntil,
	}
}

type fetchError struct {
	outcome string
}

func (e *fetchError) Error() string { return "entitlement policy fetch failed: " + e.outcome }

func statusOutcome(status int) string {
	switch {
	case status == http.StatusUnauthorized:
		return "unauthorized"
	case status == http.StatusNotFound:
		return "not_found"
	case status >= 500 && status < 600:
		return "5xx"
	case status >= 400 && status < 500:
		return "4xx"
	default:
		return "status"
	}
}

func refreshOutcome(err error) string {
	var fetchErr *fetchError
	if errors.As(err, &fetchErr) {
		return fetchErr.outcome
	}
	if errors.Is(err, ErrInvalidPolicy) {
		return "invalid_policy"
	}
	return "error"
}

func (c *Client) recordCache(outcome string) {
	if c != nil && c.observer != nil {
		c.observer.RecordEntitlementCache(outcome)
	}
}

func (c *Client) recordRefresh(outcome string, duration time.Duration) {
	if c != nil && c.observer != nil {
		c.observer.RecordEntitlementRefresh(outcome, duration.Seconds())
	}
}

func (c *Client) recordDecision(name GateName, decision Decision) Decision {
	if c != nil && c.observer != nil {
		gate := "unknown"
		if name.valid() {
			gate = string(name)
		}
		c.observer.RecordEntitlementDecision(gate, string(decision.Gate.Action), string(decision.Reason))
	}
	return cloneDecision(decision)
}
