// Package daemon: request-scoped secret resolution for per-visitor identity
// passthrough (WS-11 P1).
//
// A task may carry a RequestSecretRef — an opaque handle (never the secret
// itself) that the server minted at claim time into its in-process
// requestsecret.Store, bound to the task and the authenticated originator. The
// daemon runs in a different process, so it cannot read that store directly: at
// child-launch time it exchanges the handle for the short-lived secret over the
// DAEMON-authenticated server endpoint (RedeemRequestSecret, authorized by the
// daemon's own token, NOT the task's mat_ token) and stashes the plaintext in
// the per-task in-memory broker. It is served from there to the single
// whitelisted query child over the daemon's 127.0.0.1 loopback control server.
//
// The plaintext secret is deliberately NEVER placed in agentEnv, the Codex
// shell-env allowlist, the prompt, task / comment bodies, normal logs, or any
// file. Seeding agentEnv would leak it to the whole Agent runtime and every
// shell/tool it launches; the broker + loopback fetch keeps it scoped to the
// one child that presents the task token.
//
// Fail-closed contract: when a task declares a RequestSecretRef but the handle
// cannot be redeemed (server rejects it, transport fails, or the server
// returns an empty secret) the resolver returns an error and the task refuses
// to start. A task with no RequestSecretRef is unaffected (returns nil) so
// existing owner-scoped tasks keep their current behavior.
package daemon

import (
	"context"
	"errors"
	"strings"
)

// requestSecretChildEnvKey is the single controlled child-env variable the
// whitelisted query launcher injects the fetched secret under (matching
// identity_shim.py's highest-priority source var, AIME_USER_CLOUD_JWT). NOTE:
// the daemon no longer writes this into agentEnv — it is set transiently by the
// launcher on the query child only, from the value fetched over the broker. The
// constant lives here so the daemon and the launcher agree on the name.
const requestSecretChildEnvKey = "AIME_USER_CLOUD_JWT"

// requestSecretRedeemer redeems an opaque handle for its short-lived secret
// using the DAEMON token, binding it to the given task id server-side.
// Implemented by *Client; an interface so the resolver is unit-testable without
// a live server.
type requestSecretRedeemer interface {
	RedeemRequestSecret(ctx context.Context, taskID, handle string) (string, error)
}

// requestSecretSink stashes a redeemed secret for later loopback fetch by the
// whitelisted child. Implemented by *requestSecretBroker.
type requestSecretSink interface {
	Put(taskID, token, secret string)
	Discard(taskID string)
}

// resolveRequestSecretIntoBroker resolves task.RequestSecretRef by redeeming
// the handle over the daemon-authenticated server endpoint and stashing the
// plaintext in the broker keyed by taskID, fetchable only by a caller
// presenting fetchToken (the task's own mat_ token, which the launched child
// receives via MULTICA_TOKEN). It returns:
//
//   - nil        when the task carries no RequestSecretRef (no-op);
//   - nil        after a live secret is stashed in the broker;
//   - non-nil    fail-closed, when a handle is declared but cannot be safely
//     redeemed (nil redeemer/sink, missing token, invalid ref, or server
//     rejection).
//
// The secret is never returned to the caller, never logged, never stored back
// onto the Task, and never written to agentEnv or disk — it exists only inside
// the broker until the whitelisted child takes it over loopback.
func resolveRequestSecretIntoBroker(ctx context.Context, redeemer requestSecretRedeemer, sink requestSecretSink, taskID, fetchToken, ref string) error {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil
	}
	if redeemer == nil {
		return errors.New("request secret ref present but no redeemer configured")
	}
	if sink == nil {
		return errors.New("request secret ref present but no broker configured")
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return errors.New("request secret ref present but task id missing")
	}
	fetchToken = strings.TrimSpace(fetchToken)
	if fetchToken == "" {
		return errors.New("request secret ref present but fetch token missing")
	}
	// The reference is an opaque handle. Reject anything with separators / NUL /
	// traversal or unbounded length before it leaves the process — defense in
	// depth even though the server validates the binding authoritatively.
	if err := validateRequestSecretRef(ref); err != nil {
		return err
	}
	secret, err := redeemer.RedeemRequestSecret(ctx, taskID, ref)
	if err != nil {
		// Do not echo the handle value or the secret. The server already
		// collapses reject reasons to one code; keep the daemon-side message
		// equally opaque.
		return errors.New("resolve request secret ref: handle not redeemable")
	}
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return errors.New("resolve request secret ref: empty secret")
	}
	// Ensure no stale entry survives if this is a re-launch, then stash fresh.
	sink.Discard(taskID)
	sink.Put(taskID, fetchToken, secret)
	return nil
}

// validateRequestSecretRef enforces that a reference is a single opaque handle
// segment: non-empty, no path separators, no traversal, no NUL, bounded length.
func validateRequestSecretRef(ref string) error {
	if ref == "" {
		return errors.New("request secret ref is empty")
	}
	if len(ref) > 256 {
		return errors.New("request secret ref too long")
	}
	if strings.ContainsAny(ref, "/\\\x00") {
		return errors.New("request secret ref contains path separators")
	}
	if ref == "." || ref == ".." || strings.Contains(ref, "..") {
		return errors.New("request secret ref contains traversal")
	}
	return nil
}
