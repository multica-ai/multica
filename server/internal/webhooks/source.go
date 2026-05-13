package webhooks

import (
	"errors"
	"net/http"
)

// ErrSchemaMismatch is returned by Source.Normalize when the payload
// shape does not match what the adapter expects (e.g. GitHub bumped the
// workflow_run event to a v4 schema and the adapter is still pinned to
// v3). The router translates this to HTTP 400 — loud and explicit, per
// the constraint "Schema mismatch — явный fail, не молчаливый парсинг"
// in the plan.
var ErrSchemaMismatch = errors.New("webhooks: payload schema mismatch")

// ErrUnsupportedEvent is returned by Source.Normalize when the payload
// is structurally valid but represents an event we deliberately don't
// react to (e.g. workflow_run with conclusion=success — the cascade
// only cares about failures, but we don't want to crash on success
// payloads either). The router translates this to HTTP 204 — accepted
// but no work to do — distinguishing it from a real failure.
var ErrUnsupportedEvent = errors.New("webhooks: unsupported event")

// Source is the per-vendor adapter contract. Every webhook source
// (GitHub, Linear, Slack, GitLab, …) implements this interface and
// registers a single instance with the Router. PR2 ships stubs for the
// four planned sources; PR3 replaces GitHubSource with the real
// implementation.
type Source interface {
	// Name returns the source identifier used in the URL path:
	// /webhooks/<name>. Lowercase, no slashes. Must be unique across
	// the Router registry — duplicate registration panics at startup
	// (cheap fail-loud).
	Name() string

	// SignatureHeader returns the HTTP header that carries the HMAC
	// signature for this source. Different vendors use different
	// conventions:
	//   github: X-Hub-Signature-256 (value `sha256=<hex>`)
	//   linear: Linear-Signature
	//   slack:  X-Slack-Signature   (value `v0=<hex>` with timestamp guard)
	//   gitlab: X-Gitlab-Token      (plain shared secret, not HMAC — adapter
	//                                handles the difference internally;
	//                                returns empty here to opt out of the
	//                                generic HMAC middleware)
	// An empty return string opts out of generic HMAC verification —
	// the adapter is then responsible for authenticating the request
	// itself inside Normalize. PR2 stub adapters return empty; PR3
	// GitHub adapter returns "X-Hub-Signature-256".
	SignatureHeader() string

	// Secrets returns the current and previous shared secrets used to
	// verify the HMAC. The router accepts a signature computed with
	// either, enabling the 24h zero-downtime rotation procedure in the
	// plan: operator installs new secret as `current`, leaves old as
	// `previous`, waits 24h for in-flight retries to drain, then drops
	// `previous`.
	//
	// `previous` may be empty when no rotation is in flight. `current`
	// must be non-empty when SignatureHeader is non-empty — startup
	// validation in Router.Register fails loud if a source claims to
	// use HMAC but has no current secret configured.
	Secrets() (current, previous string)

	// Normalize parses the raw HTTP request body — which the router
	// passes through unchanged after HMAC verification — into a
	// TriggerEvent. Returns:
	//   - (*TriggerEvent, nil): event accepted, ready for downstream.
	//   - (nil, ErrUnsupportedEvent): structurally valid but not a
	//                                 cascade trigger (success, ping, etc.).
	//   - (nil, ErrSchemaMismatch): payload shape doesn't match the
	//                               pinned schema version. Logged + 400.
	//   - (nil, other error):        unexpected parse failure; 500.
	//
	// The request body is fully readable here — the router uses
	// httputil.NewChunkedReader-style buffering so HMAC can hash the
	// raw bytes while still letting the adapter decode them. See
	// router.go for the body-replay mechanism.
	Normalize(r *http.Request) (*TriggerEvent, error)
}
