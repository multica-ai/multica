package webhooks

import (
	"net/http"
)

// PR2 ships four stub adapters so the registry shape is fully visible
// and the feature-flag-on smoke test has something to exercise.
//
// GitHubStub will be REPLACED (not extended) by PR3's real adapter —
// the stub keeps Normalize returning ErrUnsupportedEvent on every
// call so a misconfigured staging that flips the flag too early
// before PR3 lands cannot drive a cascade by accident.
//
// LinearStub / SlackStub / GitLabStub are placeholders for future
// generic-event-router source adapters (E1 capability). They are
// registered but always return ErrUnsupportedEvent so callers see a
// clean 204 No Content rather than a 404 — operators wiring up a
// new vendor see the route is alive while the adapter is being
// built.

// stubSource is the shared implementation for the four placeholder
// adapters. Each named adapter wraps it with its public Name() so
// router.go's logs and the registry key carry the right vendor
// label.
type stubSource struct {
	name string
}

func (s stubSource) Name() string                          { return s.name }
func (s stubSource) SignatureHeader() string               { return "" } // opt out of HMAC; replaced in PR3 for github
func (s stubSource) Secrets() (current, previous string)   { return "", "" }
func (s stubSource) Normalize(*http.Request) (*TriggerEvent, error) {
	return nil, ErrUnsupportedEvent
}

// GitHubStub is the GitHub source adapter placeholder. PR3 replaces
// it with a real implementation that subscribes to workflow_run,
// check_run, pull_request, and pull_request_review events; pins the
// payload schema version; emits HMAC-SHA256 signatures against
// `X-Hub-Signature-256` using `MULTICA_GITHUB_WEBHOOK_SECRET_CURRENT`
// and `_PREVIOUS` env vars.
//
// Until PR3 ships, GitHubStub also opts out of HMAC and returns
// ErrUnsupportedEvent. Practical consequence: even if PR8's
// feature-flag rollout is accidentally triggered before PR3 merges,
// no live GitHub event can drive a cascade — the worst case is a
// stream of 204s in the access log.
func GitHubStub() Source  { return stubSource{name: "github"} }

// LinearStub, SlackStub, GitLabStub: future E1 source adapters. Each
// is a TODO until a real integration is planned in its own ticket.
// Registering them now means the route shape, log surface, and
// metric labels are stable from day one — operators see
// /webhooks/linear etc. resolve to 204 No Content instead of 404,
// which is the cleanest signal of "registered, not implemented".
func LinearStub() Source { return stubSource{name: "linear"} }
func SlackStub() Source  { return stubSource{name: "slack"} }
func GitLabStub() Source { return stubSource{name: "gitlab"} }
