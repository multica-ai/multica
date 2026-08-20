package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
	"github.com/multica-ai/multica/server/pkg/remotemcp"
)

// The hook engine: the one place Multica calls OUT to a plugin's own server.
//
// Everything before this ran the other way — a sandboxed surface asked the host
// and the host acted on the signed-in user's session, so no request ever left
// our infrastructure on a plugin's say-so. A hook does leave, which is why the
// checks here are about the destination rather than the caller: the host must
// be one the manifest declared through a `net:` scope, it must resolve to a
// public address at dial time, and the body must be signed so the receiver can
// tell our call from anyone else's.

const (
	// Signature scheme version, carried in the header so the scheme can change
	// without every handler guessing which one it is looking at.
	hookSignatureVersion = "v1"
	// How far a request timestamp may be from our clock. Bounds replay: a
	// captured request is only reusable inside this window, and a handler that
	// also remembers signatures can close it entirely.
	hookTimestampTolerance = 5 * time.Minute
	// Fallback when a manifest omits timeout_ms.
	hookDefaultTimeout = 10 * time.Second
	// Response bodies are read for the caller, so they are capped. A hook that
	// answers with a gigabyte should fail, not consume the host.
	hookMaxResponseBytes = 1 << 20

	// Event retry schedule. Three attempts total, then the call is abandoned.
	hookEventAttempts = 3
	hookEventBackoff  = 2 * time.Second

	// Circuit breaker. After this many failures inside the window, event
	// delivery for the hook stops until the window rolls off — an endpoint that
	// has been down for a minute does not need one request per event to notice.
	hookBreakerThreshold = 5
	hookBreakerWindow    = 5 * time.Minute

	// Per-hook rate limit, counted in attempts.
	hookRateLimit  = 120
	hookRateWindow = time.Minute
)

// HookResult is what a completed hook call returns to whoever invoked it.
type HookResult struct {
	Status   string          `json:"status"`
	Output   json.RawMessage `json:"output,omitempty"`
	Error    string          `json:"error,omitempty"`
	Latency  int             `json:"latency_ms"`
	HookKey  string          `json:"hook_key"`
	Trigger  string          `json:"trigger"`
	Attempts int             `json:"attempts"`
}

// HookActor is who the resulting writes belong to.
//
// This is the whole identity model for hooks, and it follows the trigger rather
// than the plugin: a ui/manual hook runs because a person pressed something, so
// its writes stay that person's (marked via_plugin_id); an event hook has no
// person behind it and must not borrow the last one who happened to touch the
// issue.
type HookActor struct {
	Type string
	ID   pgtype.UUID
}

// HookInvocation is one call the engine has been asked to make.
type HookInvocation struct {
	Installation db.PluginInstallation
	Hook         plugincontract.Hook
	Trigger      string
	// EventType is set only for the event trigger, and names what happened.
	EventType string
	Actor     HookActor
	// IssueID is the issue this call is about, when there is one. It narrows the
	// callback token so a handler answering about one issue cannot use the same
	// grant to reach across the workspace.
	IssueID pgtype.UUID
	Input   any
}

// hookRequestBody is the wire format. Stable and small on purpose: a hook
// handler written against v1 should not have to care what else the host learns
// how to send later.
type hookRequestBody struct {
	Version      int    `json:"version"`
	HookKey      string `json:"hook_key"`
	Trigger      string `json:"trigger"`
	EventType    string `json:"event_type,omitempty"`
	WorkspaceID  string `json:"workspace_id"`
	Installation string `json:"installation_id"`
	// IssueID is the issue this call is about, as resolved and permission-checked
	// by the host. Sent because the alternative is every handler reading it out
	// of client-supplied `input` — unvalidated, and absent entirely for the event
	// trigger, where no client was involved at all.
	IssueID string           `json:"issue_id,omitempty"`
	Actor   hookRequestActor `json:"actor"`
	Input   json.RawMessage  `json:"input,omitempty"`
	// Config is the installation's non-secret configuration, the values an
	// administrator typed into the host-rendered form. Sent because the handler
	// has no other way to read them — the Action API deliberately has no config
	// endpoint — and a plugin forced to keep its own second copy would make the
	// manifest's config block decorative.
	//
	// Secret-typed fields are NEVER included. They are the plugin's credentials
	// for ITS OWN services; it already has them, and putting them on the wire
	// would hand every value to whoever holds the endpoint.
	Config map[string]any `json:"config,omitempty"`
	// CallbackToken lets the handler call the Action API back for the few
	// minutes it is valid. Narrower than the installation's own token and tied
	// to this one call, so a handler that leaks it leaks a few minutes of the
	// scopes it was already using, not standing access.
	CallbackToken string `json:"callback_token,omitempty"`
	CallbackURL   string `json:"callback_url,omitempty"`
}

type hookRequestActor struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`
}

// FindHook returns the named hook from an installation's consented manifest.
//
// Read from the installation row, never from the manifest the source URL serves
// right now: the admin consented to a specific set of endpoints, and a plugin
// that later repoints its own hook at somewhere else must go through the
// upgrade flow to have that take effect.
func FindHook(installation db.PluginInstallation, hookKey string) (plugincontract.Hook, error) {
	manifest, err := ParseInstallationManifest(installation)
	if err != nil {
		return plugincontract.Hook{}, pluginErrf(PluginErrorInvalid, "plugin manifest is unreadable")
	}
	for _, hook := range manifest.Contributes.Hooks {
		if hook.Key == hookKey {
			return hook, nil
		}
	}
	return plugincontract.Hook{}, pluginErrf(PluginErrorNotFound, "this Plugin has no hook named %q", hookKey)
}

// HookAllowsTrigger reports whether the hook declared this trigger. A trigger
// the manifest did not list is not a call site the host may invent.
func HookAllowsTrigger(hook plugincontract.Hook, trigger string) bool {
	for _, declared := range hook.Triggers {
		if declared == trigger {
			return true
		}
	}
	return false
}

// InvokeHook performs one call and records it.
//
// Callers pick the blocking behaviour, not this function: ui/manual await the
// result because a person is watching, and the event dispatcher runs it on its
// own goroutine because the host request that produced the event must not wait
// for a third party.
func (s *PluginService) InvokeHook(ctx context.Context, invocation HookInvocation, attempt int) (HookResult, error) {
	hook := invocation.Hook
	result := HookResult{HookKey: hook.Key, Trigger: invocation.Trigger, Attempts: attempt}

	if !invocation.Installation.Enabled {
		return result, pluginErrf(PluginErrorForbidden, "this Plugin is disabled")
	}
	if !HookAllowsTrigger(hook, invocation.Trigger) {
		return result, pluginErrf(PluginErrorForbidden, "hook %q does not declare the %s trigger", hook.Key, invocation.Trigger)
	}
	if hook.Transport.Type != plugincontract.TransportHTTP {
		return result, pluginErrf(PluginErrorIncompatible, "hook transport %q is not supported yet", hook.Transport.Type)
	}
	if err := s.checkHookRate(ctx, invocation.Installation.ID, hook.Key); err != nil {
		return result, err
	}

	started := time.Now()
	output, callErr := s.callHookEndpoint(ctx, invocation)
	latency := int(time.Since(started).Milliseconds())

	result.Latency = latency
	status := "ok"
	message := ""
	if callErr != nil {
		status = hookFailureStatus(callErr)
		message = redactHookError(callErr)
		result.Error = message
	} else {
		result.Output = output
	}
	result.Status = status

	s.recordInvocation(ctx, invocation, status, attempt, latency, message)
	if callErr != nil {
		return result, callErr
	}
	return result, nil
}

// callHookEndpoint is the network half: validate the destination, sign, send.
func (s *PluginService) callHookEndpoint(ctx context.Context, invocation HookInvocation) (json.RawMessage, error) {
	installation := invocation.Installation
	granted := decodeScopes(installation.GrantedScopes)

	// The destination must be inside the consented `net:` set. Passing the
	// granted domains as the allowlist means the same string the admin approved
	// on the consent screen is what bounds the request — there is no second
	// list to fall out of sync.
	domains := plugincontract.NetDomains(granted)
	if len(domains) == 0 {
		return nil, pluginErrf(PluginErrorForbidden, "this Plugin was granted no net: scope, so it cannot call out")
	}

	// Two ways to reach an endpoint, and only the network guard differs between
	// them. The consent check does not: a destination outside the granted `net:`
	// set is refused on both paths, because that is what the admin approved and
	// no deployment setting may widen it.
	var endpoint *url.URL
	client := &http.Client{}
	if s.isDevOrigin(invocation.Hook.Transport.URL) {
		// The operator named this exact origin in MULTICA_PLUGIN_DEV_ORIGINS —
		// the same opt-in that lets a manifest be served from a local dev
		// server, for the same reason: an author building a hook has nowhere
		// public to point it yet.
		parsed, err := url.Parse(invocation.Hook.Transport.URL)
		if err != nil {
			return nil, &PluginError{Kind: PluginErrorInvalid, Message: "hook endpoint is not a valid URL", Err: err}
		}
		if !hostInNetScopes(parsed.Hostname(), domains) {
			return nil, pluginErrf(PluginErrorForbidden, "hook endpoint host is not covered by a net: scope")
		}
		endpoint = parsed
		if s.HookClient != nil {
			client = s.HookClient
		}
	} else {
		validated, err := remotemcp.ValidatePublicHTTPSEndpoint(ctx, invocation.Hook.Transport.URL, domains, nil)
		if err != nil {
			return nil, &PluginError{Kind: PluginErrorForbidden, Message: "hook endpoint is not allowed", Err: err}
		}
		endpoint = validated
		// NewSecureHTTPClient re-resolves at dial and refuses a non-public
		// address, so a hostname that passed validation and then flipped to a
		// private IP cannot be used to reach inside the network.
		client = remotemcp.NewSecureHTTPClient(endpoint)
	}

	body, err := s.buildHookBody(ctx, invocation)
	if err != nil {
		return nil, err
	}
	// The grant lives exactly as long as the call it was issued for. Without
	// this it would stay usable for the rest of its TTL after the handler has
	// already answered.
	if s.Callbacks != nil && body.CallbackToken != "" {
		defer s.Callbacks.Revoke(body.CallbackToken)
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, &PluginError{Kind: PluginErrorInvalid, Message: "encode hook request", Err: err}
	}

	timeout := hookDefaultTimeout
	if invocation.Hook.TimeoutMs > 0 {
		timeout = time.Duration(invocation.Hook.TimeoutMs) * time.Millisecond
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	request, err := http.NewRequestWithContext(callCtx, http.MethodPost, endpoint.String(), bytes.NewReader(encoded))
	if err != nil {
		return nil, &PluginError{Kind: PluginErrorInvalid, Message: "build hook request", Err: err}
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	signature, err := s.SignHookPayload(installation.ID, timestamp, encoded)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Multica-Timestamp", timestamp)
	request.Header.Set("X-Multica-Signature", hookSignatureVersion+"="+signature)
	request.Header.Set("X-Multica-Plugin-Installation", uuidString(installation.ID))
	request.Header.Set("User-Agent", "Multica-Hooks/1")

	response, err := client.Do(request)
	if err != nil {
		return nil, &PluginError{Kind: PluginErrorUnavailable, Message: "hook endpoint did not answer", Err: err}
	}
	defer func() { _ = response.Body.Close() }()

	payload, err := io.ReadAll(io.LimitReader(response.Body, hookMaxResponseBytes))
	if err != nil {
		return nil, &PluginError{Kind: PluginErrorUnavailable, Message: "read hook response", Err: err}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, pluginErrf(PluginErrorUnavailable, "hook endpoint returned %d", response.StatusCode)
	}
	if len(bytes.TrimSpace(payload)) == 0 {
		return nil, nil
	}
	if !json.Valid(payload) {
		return nil, pluginErrf(PluginErrorInvalid, "hook endpoint returned a non-JSON body")
	}
	return json.RawMessage(payload), nil
}

// nonSecretConfig reads the installation's stored configuration and drops
// anything the manifest declared as a secret.
//
// Filtered against the MANIFEST rather than against the stored shape: secrets
// live in their own table and should never appear in the config column at all,
// so this is the check that would catch it if one ever did.
func nonSecretConfig(installation db.PluginInstallation) map[string]any {
	if len(installation.Config) == 0 {
		return nil
	}
	values := map[string]any{}
	if err := json.Unmarshal(installation.Config, &values); err != nil {
		return nil
	}
	manifest, err := ParseInstallationManifest(installation)
	if err != nil {
		// Without a manifest there is no way to tell which keys are secret, so
		// send none of them.
		return nil
	}
	for key := range values {
		field, declared := manifest.Config.Field(key)
		if !declared || field.Type == plugincontract.ConfigSecret {
			delete(values, key)
		}
	}
	if len(values) == 0 {
		return nil
	}
	return values
}

func (s *PluginService) buildHookBody(ctx context.Context, invocation HookInvocation) (hookRequestBody, error) {
	body := hookRequestBody{
		Version:      1,
		HookKey:      invocation.Hook.Key,
		Trigger:      invocation.Trigger,
		EventType:    invocation.EventType,
		WorkspaceID:  uuidString(invocation.Installation.WorkspaceID),
		Installation: uuidString(invocation.Installation.ID),
		Actor:        hookRequestActor{Type: invocation.Actor.Type, ID: uuidString(invocation.Actor.ID)},
	}
	if invocation.IssueID.Valid {
		body.IssueID = uuidString(invocation.IssueID)
	}
	body.Config = nonSecretConfig(invocation.Installation)
	if invocation.Input != nil {
		encoded, err := json.Marshal(invocation.Input)
		if err != nil {
			return hookRequestBody{}, &PluginError{Kind: PluginErrorInvalid, Message: "encode hook input", Err: err}
		}
		body.Input = encoded
	}
	if s.Callbacks != nil {
		token, err := s.Callbacks.Issue(ctx, invocation)
		if err != nil {
			return hookRequestBody{}, err
		}
		body.CallbackToken = token
		body.CallbackURL = s.CallbackBaseURL
	}
	return body, nil
}

// SignHookPayload produces the hex HMAC a receiver checks.
//
// The signed string joins the timestamp and the body with a separator that
// cannot appear in the timestamp. Signing the body alone would let a captured
// request be replayed forever; signing a plain concatenation would let a
// crafted timestamp and body pair swap bytes between the two fields.
func (s *PluginService) SignHookPayload(installationID pgtype.UUID, timestamp string, body []byte) (string, error) {
	key, err := s.hookSigningKey(installationID)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// hookSigningKey derives this installation's signing secret from the deployment
// key.
//
// Derived rather than stored, and this is the asymmetry that decides it: the
// host must PRODUCE this value to sign with it, so a one-way hash is not an
// option, and storing a recoverable copy of it in every installation row would
// put a usable secret in reach of any database read. The install token goes the
// other way — the plugin produces it and the host only ever verifies — so that
// one is stored hashed. Same deployment key, opposite directions.
func (s *PluginService) hookSigningKey(installationID pgtype.UUID) ([]byte, error) {
	if len(s.DeploymentKey) == 0 {
		return nil, pluginErrf(PluginErrorUnavailable, "hooks are disabled: MULTICA_PLUGIN_SECRET_KEY is not configured")
	}
	mac := hmac.New(sha256.New, s.DeploymentKey)
	mac.Write([]byte("multica-plugin-hook-signature:v1:"))
	mac.Write([]byte(uuidString(installationID)))
	return mac.Sum(nil), nil
}

// HookSigningSecret is the same value in the form an author configures on their
// own server. Shown once at install time next to the install token.
func (s *PluginService) HookSigningSecret(installationID pgtype.UUID) (string, error) {
	key, err := s.hookSigningKey(installationID)
	if err != nil {
		return "", err
	}
	return "whsec_" + hex.EncodeToString(key), nil
}

// VerifyHookSignature is the receiver's half, exported so a plugin written in Go
// and our own tests check signatures the same way — a scheme only one side can
// implement is a scheme nobody can review.
func VerifyHookSignature(secretHex, timestamp string, body []byte, presented string, now time.Time) error {
	secretHex = strings.TrimPrefix(secretHex, "whsec_")
	key, err := hex.DecodeString(secretHex)
	if err != nil {
		return errors.New("signing secret is not valid hex")
	}
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return errors.New("timestamp is not an integer")
	}
	drift := now.Sub(time.Unix(seconds, 0))
	if drift < 0 {
		drift = -drift
	}
	if drift > hookTimestampTolerance {
		return errors.New("timestamp is outside the accepted window")
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	presented = strings.TrimPrefix(presented, hookSignatureVersion+"=")
	// Constant time: a byte-at-a-time comparison leaks how much of a guessed
	// signature was right, which is enough to find the rest.
	if subtle.ConstantTimeCompare([]byte(expected), []byte(presented)) != 1 {
		return errors.New("signature does not match")
	}
	return nil
}

// checkHookRate caps how often one hook may be called.
func (s *PluginService) checkHookRate(ctx context.Context, installationID pgtype.UUID, hookKey string) error {
	since := pgtype.Timestamptz{Time: time.Now().Add(-hookRateWindow), Valid: true}
	count, err := s.Queries.CountRecentPluginInvocations(ctx, db.CountRecentPluginInvocationsParams{
		InstallationID: installationID,
		HookKey:        hookKey,
		CreatedAt:      since,
	})
	if err != nil {
		// A telemetry read that fails must not take the feature down with it.
		return nil
	}
	if count >= hookRateLimit {
		return pluginErrf(PluginErrorQuota, "hook %q exceeded %d calls per minute", hookKey, hookRateLimit)
	}
	return nil
}

// HookBreakerOpen reports whether event delivery for this hook is currently
// suspended after repeated failures.
func (s *PluginService) HookBreakerOpen(ctx context.Context, installationID pgtype.UUID, hookKey string) bool {
	since := pgtype.Timestamptz{Time: time.Now().Add(-hookBreakerWindow), Valid: true}
	failures, err := s.Queries.CountRecentPluginFailures(ctx, db.CountRecentPluginFailuresParams{
		InstallationID: installationID,
		HookKey:        hookKey,
		CreatedAt:      since,
	})
	if err != nil {
		return false
	}
	return failures >= hookBreakerThreshold
}

func (s *PluginService) recordInvocation(ctx context.Context, invocation HookInvocation, status string, attempt, latency int, message string) {
	params := db.CreatePluginInvocationParams{
		InstallationID: invocation.Installation.ID,
		WorkspaceID:    invocation.Installation.WorkspaceID,
		HookKey:        invocation.Hook.Key,
		Trigger:        invocation.Trigger,
		Status:         status,
		Attempt:        int32(attempt),
		LatencyMs:      int32(latency),
	}
	if invocation.EventType != "" {
		params.EventType = pgtype.Text{String: invocation.EventType, Valid: true}
	}
	if message != "" {
		params.Error = pgtype.Text{String: truncate(message, 500), Valid: true}
	}
	// Best effort: telemetry must never fail the call it is describing.
	_, _ = s.Queries.CreatePluginInvocation(ctx, params)
}

func hookFailureStatus(err error) string {
	var pluginErr *PluginError
	if errors.As(err, &pluginErr) {
		switch pluginErr.Kind {
		case PluginErrorForbidden, PluginErrorQuota, PluginErrorIncompatible:
			return "refused"
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	return "failed"
}

// redactHookError keeps the host's own description and drops anything the
// remote end supplied. An endpoint that echoes its input could otherwise write
// issue content into a table that has no deletion path for it.
func redactHookError(err error) string {
	var pluginErr *PluginError
	if errors.As(err, &pluginErr) {
		return truncate(pluginErr.Message, 500)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "hook endpoint timed out"
	}
	return "hook call failed"
}

// hostInNetScopes is the exact-host check the consent screen promises. Never a
// suffix match: one scope string must mean the same thing here as it does in
// the surface CSP and on the authorization screen.
func hostInNetScopes(host string, domains []string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, domain := range domains {
		if host == strings.ToLower(domain) {
			return true
		}
	}
	return false
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
