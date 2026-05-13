// Package persona is the Go SDK for the firtal-persona service.
//
// Apps and agent runtimes import this package, configure it with the persona
// endpoint and a bearer token, and call Check() / GetSecret() / RegisterActor.
//
//	client, err := persona.New(persona.Config{
//	    Endpoint: "https://persona.firtal.ts.net",
//	    Token:    os.Getenv("PERSONA_TOKEN"),
//	})
//	allowed, err := client.Check(ctx, "read", "rest:firtal-data-registry:GET /datasets")
//
// The SDK has zero runtime dependencies beyond the Go standard library. It
// caches positive answers for 5 minutes (SPOF mitigation per JEH-388) and
// never caches denies.
package persona

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Config configures a persona Client.
type Config struct {
	// Endpoint is the persona base URL, e.g. https://persona.firtal.ts.net.
	Endpoint string
	// Token is the bearer token persona issued for this actor.
	Token string
	// HTTP overrides the http.Client. Optional.
	HTTP *http.Client
	// CacheTTL is how long positive answers stay cached. Defaults to 5min.
	CacheTTL time.Duration
}

// Client talks to a persona service.
type Client struct {
	endpoint string
	token    string
	http     *http.Client

	cacheMu  sync.Mutex
	cache    map[string]cacheEntry
	cacheTTL time.Duration
}

// New constructs a persona client. Returns an error if the config is
// missing required fields.
func New(cfg Config) (*Client, error) {
	if cfg.Endpoint == "" {
		return nil, errors.New("persona: endpoint required")
	}
	if cfg.Token == "" {
		return nil, errors.New("persona: token required")
	}
	c := &Client{
		endpoint: strings.TrimRight(cfg.Endpoint, "/"),
		token:    cfg.Token,
		http:     cfg.HTTP,
		cache:    make(map[string]cacheEntry),
		cacheTTL: cfg.CacheTTL,
	}
	if c.http == nil {
		c.http = &http.Client{Timeout: 5 * time.Second}
	}
	if c.cacheTTL == 0 {
		c.cacheTTL = 5 * time.Minute
	}
	return c, nil
}

// AnonymousActorID is persona's well-known ID for the singleton
// Anonymous actor (JEH-1076). Re-exported so service callers can attach
// scoped grants to it via the actor-grants API without first having to
// look up the actor by name.
const AnonymousActorID = "00000000-0000-0000-0000-0000000a0000"

// CheckResult mirrors persona-API's response.
//
// NeedsApproval (JEH-1078) is the third decision state. When set,
// Allowed is false and Pending carries the metadata callers need to
// wait on a human approver. SDK consumers that ignore the new fields
// see Allowed=false and fail closed — which is the right default for
// any state they can't reason about.
//
// MaskedFields (JEH-1079) is the list of field-paths the caller must
// redact from the resource payload before rendering. Only meaningful
// when Allowed=true.
type CheckResult struct {
	Allowed       bool
	DecisionID    string
	Reason        string
	NeedsApproval bool
	Pending       *PendingApproval
	MaskedFields  []string
}

// PendingApproval is the payload returned alongside NeedsApproval=true.
// Callers (cerebro-persona-hook is the primary user) poll
// /v1/approvals/{RequestID} until the status changes.
type PendingApproval struct {
	RequestID    string       `json:"request_id"`
	ExpiresAt    time.Time    `json:"expires_at"`
	ApproverPool ApproverPool `json:"approver_pool"`
	GrantID      string       `json:"grant_id"`
}

// ApproverPool mirrors store.ApproverPool: who may resolve a pending
// request. Users matches by actor ID; Names matches by actor display
// name (the MVP stand-in for "role"). Groups will be added when the
// platform's group primitive lands.
type ApproverPool struct {
	Users []string `json:"users,omitempty"`
	Names []string `json:"names,omitempty"`
}

// ApprovalStatus is the lifecycle position of a pending request, sent
// on the wire by the /v1/approvals/{id} endpoint.
type ApprovalStatus string

const (
	ApprovalPending  ApprovalStatus = "pending"
	ApprovalApproved ApprovalStatus = "approved"
	ApprovalDenied   ApprovalStatus = "denied"
	ApprovalExpired  ApprovalStatus = "expired"
)

// ApprovalRequest is the wire shape returned by /v1/approvals endpoints.
type ApprovalRequest struct {
	ID             string         `json:"id"`
	RequesterID    string         `json:"requester_id"`
	RequesterName  string         `json:"requester_name,omitempty"`
	Action         string         `json:"action"`
	ResourceKind   string         `json:"resource_kind"`
	ResourceID     string         `json:"resource_id"`
	ResourceAttrs  map[string]any `json:"resource_attrs,omitempty"`
	ApproverPool   ApproverPool   `json:"approver_pool"`
	GrantID         string        `json:"grant_id"`
	GrantSource     string        `json:"grant_source"`
	DecisionID      string        `json:"decision_id"`
	Status          ApprovalStatus `json:"status"`
	DecidedAt       *time.Time    `json:"decided_at,omitempty"`
	DecidedByID     string        `json:"decided_by_id,omitempty"`
	DecidedByName   string        `json:"decided_by_name,omitempty"`
	DecisionReason  string        `json:"decision_reason,omitempty"`
	CreatedAt       time.Time     `json:"created_at"`
	ExpiresAt       time.Time     `json:"expires_at"`
	DurationMS      int64         `json:"duration_ms,omitempty"`
}

// Check asks persona "may I do action on resource?". Returns Allowed=false
// for any deny, including network errors after the cache TTL has expired
// (fail-closed). Positive answers are cached; denies are never cached.
func (c *Client) Check(ctx context.Context, action, resource string) (*CheckResult, error) {
	key := action + "|" + resource

	if hit := c.lookupCache(key); hit != nil {
		return hit, nil
	}

	body, err := json.Marshal(map[string]string{"action": action, "resource": resource})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/v1/check", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		// Network error: fall back to stale cache. Per JEH-388 SPOF
		// mitigation we extend cache TTL on persona unreachable, so a
		// previous Allowed=true within 5min still answers Allowed=true.
		if hit := c.lookupCacheStale(key); hit != nil {
			return hit, nil
		}
		return &CheckResult{Allowed: false, Reason: "persona unreachable"}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		var apiErr struct {
			Error      string `json:"error"`
			Message    string `json:"message"`
			DecisionID string `json:"decision_id"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&apiErr)
		return &CheckResult{
			Allowed:    false,
			DecisionID: apiErr.DecisionID,
			Reason:     apiErr.Message,
		}, nil
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("persona: status %d: %s", resp.StatusCode, string(raw))
	}

	var out struct {
		Allowed         bool             `json:"allowed"`
		DecisionID      string           `json:"decision_id"`
		Reason          string           `json:"reason"`
		NeedsApproval   bool             `json:"needs_approval,omitempty"`
		PendingApproval *PendingApproval `json:"pending_approval,omitempty"`
		MaskedFields    []string         `json:"masked_fields,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}

	result := &CheckResult{
		Allowed:       out.Allowed,
		DecisionID:    out.DecisionID,
		Reason:        out.Reason,
		NeedsApproval: out.NeedsApproval,
		Pending:       out.PendingApproval,
		MaskedFields:  out.MaskedFields,
	}
	// Never cache a needs_approval — the next call must re-create a
	// fresh request so we don't accidentally re-use an approver's earlier
	// decision (the reentrancy acceptance criterion in JEH-1078).
	//
	// JEH-1079: also never cache results that carry masked_fields — the
	// answer is tied to a particular grant's classification ceiling, and
	// stale cache could under-redact a future caller.
	if result.Allowed && !result.NeedsApproval && len(result.MaskedFields) == 0 {
		c.storeCache(key, result)
	}
	return result, nil
}

// GetSecret fetches a secret from persona. Persona checks Cerbos first;
// callers receive the secret value or a 403 error.
//
// MVP scaffold: persona-API currently returns 501 for secrets while Phase 2
// (JEH-395) lands the Infisical wiring. SDK signature is stable.
func (c *Client) GetSecret(ctx context.Context, path string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"/v1/secret/"+path, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("persona: secret %s: status %d: %s", path, resp.StatusCode, string(raw))
	}
	var out struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.Value, nil
}

// RegisterActor creates a new actor in persona and returns the actor's id
// plus its initial bearer token. Only callable by bootstrap-admin or actors
// authorised to register others (e.g. cerebro daemon). Use
// RegisterActorWithAttributes to seed the operator-visible context (tools,
// provider, workdir...) at creation time.
func (c *Client) RegisterActor(ctx context.Context, name, kind, ownerID string) (id, token string, err error) {
	return c.RegisterActorWithAttributes(ctx, name, kind, ownerID, nil)
}

// RegisterActorWithAttributes is RegisterActor with the actor's attributes
// (E3 part 3) seeded at creation time. Persona's UI surfaces them so an
// operator can see what the actor's runtime supports before invoking any
// tools.
func (c *Client) RegisterActorWithAttributes(ctx context.Context, name, kind, ownerID string, attrs map[string]any) (id, token string, err error) {
	payload := map[string]any{
		"name":     name,
		"type":     kind,
		"owner_id": ownerID,
	}
	if len(attrs) > 0 {
		payload["attributes"] = attrs
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/v1/actors", bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", "", fmt.Errorf("persona: register actor: status %d: %s", resp.StatusCode, string(raw))
	}

	var out struct {
		Actor struct {
			ID string `json:"id"`
		} `json:"actor"`
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", "", err
	}
	return out.Actor.ID, out.Token, nil
}

// SandboxProfile is the shape persona returns from
// /v1/actors/{id}/sandbox-profile. Cerebro's daemon consumes this when
// constructing a Seatbelt profile for a spawned agent.
//
// AllowedWorkdirRoots (Phase C gap #2) lists the absolute path prefixes
// the actor's runtime is allowed to operate under. Empty = no
// restriction. Cerebro should mount only roots within this list when
// preparing the agent's exec environment; persona's /v1/check denies any
// tool call whose reported workdir is outside the whitelist.
type SandboxProfile struct {
	ActorID             string   `json:"actor_id"`
	ActorName           string   `json:"actor_name"`
	SandboxName         string   `json:"sandbox_name,omitempty"`
	WritablePaths       []string `json:"writable_paths"`
	AllowedHosts        []string `json:"allowed_hosts"`
	AllowedTools        []string `json:"allowed_tools"`
	AllowedMCPServers   []string `json:"allowed_mcp_servers"`
	AllowedWorkdirRoots []string `json:"allowed_workdir_roots"`
	// RequireInfisicalSecrets (Phase C gap #3b) tells Cerebro's daemon to
	// reject any spawn where a CustomEnv value is not an
	// `infisical://...` reference. False = mixed cleartext + refs OK.
	RequireInfisicalSecrets bool `json:"require_infisical_secrets"`
}

// GetSandboxProfile fetches the effective sandbox profile for an actor.
// Authorised by the calling principal having read on
// system:persona:sandbox-profile (the system:cerebro service-actor and
// bootstrap-admin do; nothing else does).
func (c *Client) GetSandboxProfile(ctx context.Context, actorID string) (*SandboxProfile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"/v1/actors/"+actorID+"/sandbox-profile", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("persona: sandbox profile: status %d: %s", resp.StatusCode, string(raw))
	}
	var out SandboxProfile
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CheckResource is the structured form of Check. It sends the resource as
// {kind, id, attributes} and lets callers ask a check on behalf of another
// actor (subjectActorID empty = ask about self). Used by cerebro-persona-hook
// so future argument-level rules can match without an API migration.
//
// Does NOT use the cache — every check carries fresh attributes that would
// poison the key.
func (c *Client) CheckResource(ctx context.Context, subjectActorID, action, kind, id string, attributes map[string]any) (*CheckResult, error) {
	body, _ := json.Marshal(map[string]any{
		"action":           action,
		"resource":         map[string]any{"kind": kind, "id": id, "attributes": attributes},
		"subject_actor_id": subjectActorID,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/v1/check", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return &CheckResult{Allowed: false, Reason: "persona unreachable"}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("persona: check on behalf: status %d: %s", resp.StatusCode, string(raw))
	}
	var out struct {
		Allowed         bool             `json:"allowed"`
		DecisionID      string           `json:"decision_id"`
		Reason          string           `json:"reason"`
		NeedsApproval   bool             `json:"needs_approval,omitempty"`
		PendingApproval *PendingApproval `json:"pending_approval,omitempty"`
		MaskedFields    []string         `json:"masked_fields,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &CheckResult{
		Allowed:       out.Allowed,
		DecisionID:    out.DecisionID,
		Reason:        out.Reason,
		NeedsApproval: out.NeedsApproval,
		Pending:       out.PendingApproval,
		MaskedFields:  out.MaskedFields,
	}, nil
}

// CheckResourceWithMasking is CheckResource with the JEH-1079
// classification attributes pre-assembled into the attributes map. Pass
// the resource's overall classification (empty = unclassified) and an
// optional map of field-paths to per-field classifications. The returned
// CheckResult's MaskedFields is the list of paths the caller must redact
// before rendering the resource to the actor.
//
// Cerebro callers should use this entry point rather than hand-rolling
// the attributes map so the contract stays in one place.
func (c *Client) CheckResourceWithMasking(
	ctx context.Context,
	subjectActorID, action, kind, id, classification string,
	fieldClassifications map[string]string,
	extraAttrs map[string]any,
) (*CheckResult, error) {
	attrs := make(map[string]any, len(extraAttrs)+2)
	for k, v := range extraAttrs {
		attrs[k] = v
	}
	if classification != "" {
		attrs["classification"] = classification
	}
	if len(fieldClassifications) > 0 {
		// Marshal to map[string]any so json.Marshal emits string values
		// even when the input was a typed map[string]string.
		fc := make(map[string]any, len(fieldClassifications))
		for k, v := range fieldClassifications {
			fc[k] = v
		}
		attrs["field_classifications"] = fc
	}
	return c.CheckResource(ctx, subjectActorID, action, kind, id, attrs)
}

// CheckAnonymous asks persona "may an anonymous visitor do action on
// resource?" without sending a bearer token. The caller passes the
// share-token / link id as anonymousContext so persona records the audit
// entry as "anonymous:<context>" — without it the entry would only say
// "anonymous" and the auditor would not know which public link the call
// came from.
//
// Returns Allowed=false when persona is unreachable (fail-closed). Does
// NOT use the cache because anonymous flows are short-lived and the
// (kind, id) tuple is the security boundary — a stale "yes" could survive
// a share-token revocation.
//
// AnonymousCheck is the public-link counterpart to CheckResource. The
// callsite must NOT pass user-supplied auth tokens: the request is sent
// with no Authorization header on purpose.
func CheckAnonymous(ctx context.Context, endpoint, action, kind, id, anonymousContext string, attributes map[string]any, httpClient *http.Client) (*CheckResult, error) {
	if endpoint == "" {
		return nil, errors.New("persona: endpoint required")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}
	body, _ := json.Marshal(map[string]any{
		"action":            action,
		"resource":          map[string]any{"kind": kind, "id": id, "attributes": attributes},
		"anonymous_context": anonymousContext,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(endpoint, "/")+"/v1/check", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	// NO Authorization header. Persona's MiddlewareAllowAnonymous
	// resolves the missing header to the singleton Anonymous actor.

	resp, err := httpClient.Do(req)
	if err != nil {
		return &CheckResult{Allowed: false, Reason: "persona unreachable"}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("persona: anonymous check: status %d: %s", resp.StatusCode, string(raw))
	}
	var out struct {
		Allowed    bool   `json:"allowed"`
		DecisionID string `json:"decision_id"`
		Reason     string `json:"reason"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &CheckResult{Allowed: out.Allowed, DecisionID: out.DecisionID, Reason: out.Reason}, nil
}

// AddAnonymousGrant adds a scoped grant to the singleton Anonymous actor.
// The caller's bearer token must have "update" on the Anonymous actor
// (bootstrap-admin and the cerebro service-actor both do). The grant
// applies to all unauthenticated /v1/check calls matching (action,
// resourceKind, resourcePattern). Wraps the standard actor-grants
// endpoint so cerebro doesn't need to know the Anonymous actor's UUID.
func (c *Client) AddAnonymousGrant(ctx context.Context, action, resourceKind, resourcePattern string) (grantID string, err error) {
	body, _ := json.Marshal(map[string]string{
		"action":           action,
		"resource_kind":    resourceKind,
		"resource_pattern": resourcePattern,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/v1/actors/"+AnonymousActorID+"/grants", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("persona: add anonymous grant: status %d: %s", resp.StatusCode, string(raw))
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.ID, nil
}

// DeleteAnonymousGrant removes a previously-minted anonymous grant by ID.
// Used by cerebro when a share-token is revoked so the anonymous actor
// no longer has the resource-scoped allow. Idempotent on the server
// (delete-the-absent returns 204).
func (c *Client) DeleteAnonymousGrant(ctx context.Context, grantID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.endpoint+"/v1/actors/"+AnonymousActorID+"/grants/"+grantID, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("persona: delete anonymous grant: status %d: %s", resp.StatusCode, string(raw))
	}
	return nil
}

// FindActorByName looks up an actor by exact name. Returns ("", nil) when
// no such actor exists. Used by Cerebro's auto-create-on-name flow.
func (c *Client) FindActorByName(ctx context.Context, name string) (id string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"/v1/actors", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("persona: list actors: status %d: %s", resp.StatusCode, string(raw))
	}
	var out struct {
		Actors []struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"actors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	for _, a := range out.Actors {
		if a.Name == name && a.Status != "deleted" {
			return a.ID, nil
		}
	}
	return "", nil
}

// GetOrCreateActor finds an actor by name, creating one if missing. Returns
// (id, token, err) where token is non-empty only when a new actor was just
// created — callers must persist it for future authenticated work.
func (c *Client) GetOrCreateActor(ctx context.Context, name, kind, ownerID string) (id, token string, err error) {
	return c.GetOrCreateActorWithAttributes(ctx, name, kind, ownerID, nil)
}

// GetOrCreateActorWithAttributes is GetOrCreateActor that also seeds (or
// refreshes via PutActorAttributes) the actor's runtime-context snapshot.
// The refresh is best-effort: a failure to push attributes does not block
// the spawn flow, since the actor itself is what gates tool calls.
func (c *Client) GetOrCreateActorWithAttributes(ctx context.Context, name, kind, ownerID string, attrs map[string]any) (id, token string, err error) {
	id, err = c.FindActorByName(ctx, name)
	if err != nil {
		return "", "", err
	}
	if id != "" {
		// Existing actor — refresh attributes so the live snapshot reflects
		// the current runtime. Best-effort: an attributes push that fails
		// authorisation must not break the spawn.
		if len(attrs) > 0 {
			_ = c.PutActorAttributes(ctx, id, attrs)
		}
		return id, "", nil
	}
	return c.RegisterActorWithAttributes(ctx, name, kind, ownerID, attrs)
}

// ListActorSummary is the minimal shape returned by ListActors. Persona's
// /v1/actors endpoint returns more fields, but the SDK only exposes what
// callers need for stale-actor cleanup and similar housekeeping.
type ListActorSummary struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// ListActors returns every non-deleted actor visible to the calling token.
// Used by the daemon to find stale actors that need cleanup after an agent
// rename — see DeleteActor for the companion soft-delete call.
func (c *Client) ListActors(ctx context.Context) ([]ListActorSummary, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"/v1/actors", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("persona: list actors: status %d: %s", resp.StatusCode, string(raw))
	}
	var out struct {
		Actors []ListActorSummary `json:"actors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Actors, nil
}

// DeleteActor soft-deletes the named actor in persona. Persona keeps the
// row for audit (see /v1/log/search) but the actor stops resolving in
// /v1/check. Used by the daemon to clean up stale actors after an agent
// rename so persona's actor-list doesn't accumulate one per old name.
func (c *Client) DeleteActor(ctx context.Context, actorID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.endpoint+"/v1/actors/"+actorID, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("persona: delete actor: status %d: %s", resp.StatusCode, string(raw))
	}
	return nil
}

// PutActorAttributes replaces the operator-visible context snapshot on an
// existing actor. See ActorView.Attributes in persona's API for the shape.
func (c *Client) PutActorAttributes(ctx context.Context, actorID string, attrs map[string]any) error {
	body, _ := json.Marshal(map[string]any{"attributes": attrs})
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.endpoint+"/v1/actors/"+actorID+"/attributes", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("persona: put actor attributes: status %d: %s", resp.StatusCode, string(raw))
	}
	return nil
}

// --- cache (positive-answer, 5min TTL) ---

type cacheEntry struct {
	result    CheckResult
	expiresAt time.Time
}

func (c *Client) lookupCache(key string) *CheckResult {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	hit, ok := c.cache[key]
	if !ok {
		return nil
	}
	if time.Now().After(hit.expiresAt) {
		delete(c.cache, key)
		return nil
	}
	out := hit.result
	return &out
}

func (c *Client) lookupCacheStale(key string) *CheckResult {
	// Same as lookupCache, but tolerant of expiry up to 2× TTL while persona
	// is unreachable. Beyond that we fail closed.
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	hit, ok := c.cache[key]
	if !ok {
		return nil
	}
	if time.Now().After(hit.expiresAt.Add(c.cacheTTL)) {
		delete(c.cache, key)
		return nil
	}
	out := hit.result
	return &out
}

func (c *Client) storeCache(key string, r *CheckResult) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	c.cache[key] = cacheEntry{
		result:    *r,
		expiresAt: time.Now().Add(c.cacheTTL),
	}
}

// --- approvals (JEH-1078) -------------------------------------------------

// GetApprovalRequest fetches a single approval request by ID. Used by
// cerebro-persona-hook to poll a pending request until its status
// flips. Visible to the requester, anyone in the approver pool, or
// actors with org-wide approval.read.
func (c *Client) GetApprovalRequest(ctx context.Context, id string) (*ApprovalRequest, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"/v1/approvals/"+id, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("persona: get approval: status %d: %s", resp.StatusCode, string(raw))
	}
	var out ApprovalRequest
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListApprovalRequests returns approvals visible to the caller.
// status=="" means "any". forMe=true filters to the caller's inbox
// (every request whose pool includes the caller) without requiring
// org-wide approval.read.
func (c *Client) ListApprovalRequests(ctx context.Context, status ApprovalStatus, forMe bool, limit int) ([]ApprovalRequest, error) {
	u := c.endpoint + "/v1/approvals"
	q := []string{}
	if status != "" {
		q = append(q, "status="+string(status))
	}
	if forMe {
		q = append(q, "for_me=true")
	}
	if limit > 0 {
		q = append(q, "limit="+strconv.Itoa(limit))
	}
	if len(q) > 0 {
		u = u + "?" + strings.Join(q, "&")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("persona: list approvals: status %d: %s", resp.StatusCode, string(raw))
	}
	var out struct {
		Requests []ApprovalRequest `json:"requests"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Requests, nil
}

// ApproveRequest resolves an approval request as "approved". The caller
// must belong to the request's approver pool (by user-id or name);
// persona returns 403 otherwise.
func (c *Client) ApproveRequest(ctx context.Context, id, reason string) (*ApprovalRequest, error) {
	return c.resolveApproval(ctx, id, "approve", reason)
}

// DenyRequest resolves an approval request as "denied".
func (c *Client) DenyRequest(ctx context.Context, id, reason string) (*ApprovalRequest, error) {
	return c.resolveApproval(ctx, id, "deny", reason)
}

func (c *Client) resolveApproval(ctx context.Context, id, verb, reason string) (*ApprovalRequest, error) {
	body, _ := json.Marshal(map[string]string{"reason": reason})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/v1/approvals/"+id+"/"+verb, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("persona: %s approval: status %d: %s", verb, resp.StatusCode, string(raw))
	}
	var out ApprovalRequest
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}
