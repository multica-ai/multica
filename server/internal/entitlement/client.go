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
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/singleflight"
)

const (
	defaultRequestTimeout = 3 * time.Second
	defaultMaxEntries     = 10_000
	defaultStaleGrace     = 15 * time.Minute
	defaultFailureRetry   = 5 * time.Second
	maxPolicyTTL          = 5 * time.Minute
	maxResponseBodySize   = 64 << 10
	// minForcedRefreshAge bounds RefreshGate: a policy received this recently
	// is already current, so repeated rejections cost no more Cloud requests
	// than one per workspace in this window.
	minForcedRefreshAge = 5 * time.Second
)

var (
	ErrInvalidConfig = errors.New("entitlement: invalid configuration")
	ErrInvalidPolicy = errors.New("entitlement: invalid policy response")
)

// Config has no environment-loading side effects. A zero Config is disabled;
// setting BaseURL connects the client to the internal Cloud policy endpoint.
type Config struct {
	BaseURL    string
	HTTPClient *http.Client
	Observer   Observer
	Logger     *slog.Logger
}

// Client implements Provider with a bounded workspace cache and one in-flight
// refresh per workspace. It has no goroutines or background lifecycle.
type Client struct {
	enabled    bool
	baseURL    *url.URL
	timeout    time.Duration
	staleGrace time.Duration
	httpClient *http.Client
	observer   Observer
	logger     *slog.Logger
	now        func() time.Time
	cache      *policyCache
	refreshes  singleflight.Group
}

var (
	_ Provider        = (*Client)(nil)
	_ Refresher       = (*Client)(nil)
	_ FeatureProvider = (*Client)(nil)
)

func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return &Client{observer: cfg.Observer, now: time.Now}, nil
	}

	rawBaseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	baseURL, err := url.Parse(rawBaseURL)
	if err != nil || (baseURL.Scheme != "http" && baseURL.Scheme != "https") || baseURL.Host == "" ||
		baseURL.User != nil || baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return nil, fmt.Errorf("%w: base URL must be an absolute URL without credentials, query, or fragment", ErrInvalidConfig)
	}
	httpClient := &http.Client{}
	if cfg.HTTPClient != nil {
		clone := *cfg.HTTPClient
		httpClient = &clone
	}
	// Internal Cloud calls must not follow a redirect to another origin.
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &Client{
		enabled:    true,
		baseURL:    baseURL,
		timeout:    defaultRequestTimeout,
		staleGrace: defaultStaleGrace,
		httpClient: httpClient,
		observer:   cfg.Observer,
		logger:     logger,
		now:        time.Now,
		cache:      newPolicyCache(defaultMaxEntries),
	}, nil
}

func (c *Client) Enabled() bool { return c != nil && c.enabled }

func (c *Client) Gate(ctx context.Context, workspaceID uuid.UUID, name GateName) Decision {
	if !c.Enabled() {
		return c.recordDecision(name, offDecision(ReasonDisabled))
	}
	if workspaceID == uuid.Nil {
		return c.recordDecision(name, offDecision(ReasonInvalidWorkspace))
	}
	if !name.valid() {
		return c.recordDecision(name, offDecision(ReasonUnknownGate))
	}
	entry, reason, stale, ok := c.policy(ctx, workspaceID)
	if !ok {
		return c.recordDecision(name, offDecision(reason))
	}
	return c.recordDecision(name, decisionFromEntry(entry, name, reason, stale))
}

// RefreshGate fetches the workspace's policy again unless the cached one
// arrived within minForcedRefreshAge, then resolves the gate from it. When
// that fetch fails it answers exactly like Gate, from the cache or fail-open.
func (c *Client) RefreshGate(ctx context.Context, workspaceID uuid.UUID, name GateName) Decision {
	if !c.Enabled() || workspaceID == uuid.Nil || !name.valid() {
		return c.Gate(ctx, workspaceID, name)
	}
	entry, refreshed, err := c.refreshShared(ctx, workspaceID, "force:"+workspaceID.String(),
		func(cached cacheEntry, now time.Time) bool {
			return cached.hasPolicy && now.Before(cached.freshUntil) && now.Sub(cached.receivedAt) < minForcedRefreshAge
		})
	if err != nil {
		return c.Gate(ctx, workspaceID, name)
	}
	reason := ReasonCacheFresh
	if refreshed {
		reason = ReasonRefreshed
	}
	return c.recordDecision(name, decisionFromEntry(entry, name, reason, false))
}

// Feature resolves one paid feature from the same cached policy as Gate, so
// reading both costs at most one Cloud request per workspace.
func (c *Client) Feature(ctx context.Context, workspaceID uuid.UUID, name FeatureName) FeatureDecision {
	if !c.Enabled() {
		return c.recordFeatureDecision(name, FeatureDecision{Action: ActionOff, Reason: ReasonDisabled})
	}
	if workspaceID == uuid.Nil {
		return c.recordFeatureDecision(name, FeatureDecision{Action: ActionOff, Reason: ReasonInvalidWorkspace})
	}
	if !name.Valid() {
		return c.recordFeatureDecision(name, FeatureDecision{Action: ActionOff, Reason: ReasonUnknownGate})
	}
	entry, reason, stale, ok := c.policy(ctx, workspaceID)
	if !ok {
		return c.recordFeatureDecision(name, FeatureDecision{Action: ActionOff, Reason: reason})
	}
	return c.recordFeatureDecision(name, featureDecisionFromEntry(entry, name, reason, stale))
}

// policy returns the workspace's usable cached or refreshed policy. ok=false
// means there is none and reason says why every gate and feature is off.
// stale=true means the policy may no longer enforce.
func (c *Client) policy(ctx context.Context, workspaceID uuid.UUID) (entry cacheEntry, reason Reason, stale, ok bool) {
	now := c.now()
	cached, cachedOK := c.cache.get(workspaceID)
	if cachedOK && cached.hasPolicy && now.Before(cached.freshUntil) {
		c.recordCache("hit")
		return cached, ReasonCacheFresh, false, true
	}
	if cachedOK && now.Before(cached.retryAfter) {
		c.recordCache("retry_suppressed")
		if cached.hasPolicy && now.Before(cached.staleUntil) {
			return cached, ReasonStale, true, true
		}
		return cacheEntry{}, ReasonUnavailable, false, false
	}
	if cachedOK && cached.hasPolicy {
		c.recordCache("expired")
	} else {
		c.recordCache("miss")
	}

	refreshedEntry, refreshed, err := c.refresh(ctx, workspaceID)
	if err == nil {
		if !refreshed {
			return refreshedEntry, ReasonCacheFresh, false, true
		}
		return refreshedEntry, ReasonRefreshed, false, true
	}

	// A stale policy is safe only after enforcement is downgraded to observe.
	// Re-read the cache because another caller may have refreshed it while this
	// call was waiting in singleflight.
	now = c.now()
	if cached, ok := c.cache.get(workspaceID); ok && cached.hasPolicy && now.Before(cached.staleUntil) {
		return cached, ReasonStale, true, true
	}
	reason = ReasonUnavailable
	if errors.Is(err, ErrInvalidPolicy) {
		reason = ReasonInvalidPolicy
	}
	if errors.Is(err, errVersionRegression) {
		reason = ReasonVersionRegression
	}
	return cacheEntry{}, reason, false, false
}

type refreshResult struct {
	entry     cacheEntry
	refreshed bool
}

func (c *Client) refresh(ctx context.Context, workspaceID uuid.UUID) (cacheEntry, bool, error) {
	return c.refreshShared(ctx, workspaceID, workspaceID.String(), func(cached cacheEntry, now time.Time) bool {
		return cached.hasPolicy && now.Before(cached.freshUntil)
	})
}

// refreshShared fetches the policy once per key for all concurrent callers.
// reuse decides whether the cached entry is current enough to skip the fetch.
// Forced refreshes use their own key so a normal caller already inside the
// shared call cannot hand them back the entry they meant to replace.
func (c *Client) refreshShared(
	ctx context.Context,
	workspaceID uuid.UUID,
	key string,
	reuse func(cached cacheEntry, now time.Time) bool,
) (cacheEntry, bool, error) {
	// A refresh is shared by all callers for this workspace, so its lifetime
	// cannot belong to whichever request happens to enter singleflight first.
	// Keep request-scoped values, but let the independent fetch timeout bound it.
	refreshCtx := context.WithoutCancel(ctx)
	value, err, _ := c.refreshes.Do(key, func() (any, error) {
		now := c.now()
		if cached, ok := c.cache.get(workspaceID); ok {
			if reuse(cached, now) {
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
			receivedAt: receivedAt,
			freshUntil: receivedAt.Add(ttl),
			staleUntil: receivedAt.Add(ttl).Add(c.staleGrace),
		}
		stored, err := c.cache.put(workspaceID, entry, receivedAt)
		if err != nil {
			c.cache.markFailure(workspaceID, c.now().Add(defaultFailureRetry))
			if c.observer != nil {
				c.observer.RecordEntitlementVersionRegression()
			}
			c.recordRefresh("version_regression", time.Since(started))
			c.logger.WarnContext(refreshCtx, "entitlement subscription version regressed",
				"workspace_id", workspaceID.String())
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
	// Features stays raw so one malformed feature, or a features value of the
	// wrong JSON type, cannot fail the decode of the whole policy and with it
	// the metered gates. normalizeFeatures parses it entry by entry.
	Features json.RawMessage `json:"features"`
	// undecodableGates names gates whose value had the wrong JSON shape.
	undecodableGates map[string]bool
}

// UnmarshalJSON decodes each gate on its own. A gate of the wrong JSON shape
// is marked undecodable instead of failing the whole response, so an optional
// gate falls back alone and cannot take autopilot_runs down with it.
func (p *wirePolicy) UnmarshalJSON(data []byte) error {
	type plain wirePolicy
	var envelope struct {
		plain
		// Shallower than plain.Gates, so it receives the "gates" key.
		Gates map[string]json.RawMessage `json:"gates"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return err
	}
	*p = wirePolicy(envelope.plain)
	p.Gates = make(map[string]wireGate, len(envelope.Gates))
	for name, raw := range envelope.Gates {
		var gate wireGate
		if err := json.Unmarshal(raw, &gate); err != nil {
			if p.undecodableGates == nil {
				p.undecodableGates = make(map[string]bool)
			}
			p.undecodableGates[name] = true
			continue
		}
		p.Gates[name] = gate
	}
	return nil
}

type wireGate struct {
	Action        string                  `json:"action"`
	Limit         *int                    `json:"limit"`
	PeriodStart   *time.Time              `json:"period_start"`
	PeriodEnd     *time.Time              `json:"period_end"`
	ResetAt       *time.Time              `json:"reset_at"`
	Notifications *wireNotificationPolicy `json:"notifications"`
}

type wireNotificationPolicy struct {
	OnRejection string `json:"on_rejection"`
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
	// Unknown gates are ignored for additive compatibility.
	gates := make(map[GateName]Gate, 3)
	wireAutopilot, ok := wire.Gates[string(GateAutopilotRuns)]
	if !ok {
		return fetchedPolicy{}, ErrInvalidPolicy
	}
	autopilot, err := normalizeGate(GateAutopilotRuns, wireAutopilot)
	if err != nil {
		return fetchedPolicy{}, err
	}
	gates[GateAutopilotRuns] = autopilot
	// Gates added after autopilot_runs are optional and read on their own,
	// like features: a missing or malformed one is off for that gate alone, so
	// Cloud and Multica can deploy in either order and a bad value cannot turn
	// the autopilot quota off.
	gateReasons := make(map[GateName]Reason)
	for _, name := range []GateName{GateMembers, GateStorageBytes} {
		if wire.undecodableGates[string(name)] {
			gates[name] = Gate{Action: ActionOff}
			gateReasons[name] = ReasonGateInvalid
			continue
		}
		wireGate, ok := wire.Gates[string(name)]
		if !ok {
			gates[name] = Gate{Action: ActionOff}
			gateReasons[name] = ReasonGateMissing
			continue
		}
		gate, err := normalizeGate(name, wireGate)
		if err != nil {
			gates[name] = Gate{Action: ActionOff}
			gateReasons[name] = ReasonGateInvalid
			continue
		}
		gates[name] = gate
	}
	return fetchedPolicy{
		snapshot: policySnapshot{
			policyRevision:      wire.PolicyRevision,
			subscriptionVersion: wire.SubscriptionVersion,
			cloudValidUntil:     wire.ValidUntil,
			gates:               gates,
			gateReasons:         gateReasons,
			features:            normalizeFeatures(wire.Features),
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
		// observe carries the same parameters as enforce so consumers can tell
		// what would have been blocked without rejecting anything.
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
	gate.Notifications = normalizeNotificationPolicy(wire.Notifications)
	return gate, nil
}

// normalizeFeatures reads every known paid feature independently. A missing,
// unknown or malformed feature falls back to off for that feature alone; it
// never invalidates the policy, so the metered gates keep their instructions.
// Features this consumer does not know are ignored.
func normalizeFeatures(raw json.RawMessage) map[FeatureName]featureEntry {
	out := make(map[FeatureName]featureEntry, len(Features()))
	var entries map[string]json.RawMessage
	objectOK := len(raw) > 0 && json.Unmarshal(raw, &entries) == nil && entries != nil
	for _, name := range Features() {
		if !objectOK {
			reason := ReasonFeatureMissing
			if len(raw) > 0 && string(raw) != "null" {
				reason = ReasonFeatureInvalid
			}
			out[name] = featureEntry{action: ActionOff, reason: reason}
			continue
		}
		entry, ok := entries[string(name)]
		if !ok {
			out[name] = featureEntry{action: ActionOff, reason: ReasonFeatureMissing}
			continue
		}
		out[name] = normalizeFeature(entry)
	}
	return out
}

func normalizeFeature(raw json.RawMessage) featureEntry {
	var wire struct {
		Action json.RawMessage `json:"action"`
	}
	var action string
	if json.Unmarshal(raw, &wire) != nil || json.Unmarshal(wire.Action, &action) != nil {
		return featureEntry{action: ActionOff, reason: ReasonFeatureInvalid}
	}
	switch Action(action) {
	case ActionOff, ActionObserve, ActionEnforce:
		return featureEntry{action: Action(action)}
	default:
		return featureEntry{action: ActionOff, reason: ReasonFeatureInvalid}
	}
}

// Notification instructions are additive presentation policy. A malformed
// notification object must never invalidate the enforcement gate and fail open
// the underlying quota, so it is dropped independently.
func normalizeNotificationPolicy(wire *wireNotificationPolicy) *NotificationPolicy {
	if wire == nil || wire.OnRejection != NotificationFirstRejectionPerPeriod {
		return nil
	}
	return &NotificationPolicy{
		OnRejection: wire.OnRejection,
	}
}

func decisionFromEntry(entry cacheEntry, name GateName, reason Reason, stale bool) Decision {
	gate := cloneGate(entry.policy.gates[name])
	if fallback, ok := entry.policy.gateReasons[name]; ok {
		reason = fallback
	}
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

func featureDecisionFromEntry(entry cacheEntry, name FeatureName, reason Reason, stale bool) FeatureDecision {
	feature, ok := entry.policy.features[name]
	if !ok {
		feature = featureEntry{action: ActionOff, reason: ReasonFeatureMissing}
	}
	if feature.reason != "" {
		reason = feature.reason
	}
	action := feature.action
	downgraded := stale && action == ActionEnforce
	if downgraded {
		action = ActionObserve
	}
	return FeatureDecision{
		Action:              action,
		Reason:              reason,
		Downgraded:          downgraded,
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

// Feature decisions share the decision metric under a "feature:" prefix; the
// label set stays bounded by the known feature list.
func (c *Client) recordFeatureDecision(name FeatureName, decision FeatureDecision) FeatureDecision {
	if c != nil && c.observer != nil {
		label := "feature:unknown"
		if name.Valid() {
			label = "feature:" + string(name)
		}
		c.observer.RecordEntitlementDecision(label, string(decision.Action), string(decision.Reason))
	}
	return decision
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
