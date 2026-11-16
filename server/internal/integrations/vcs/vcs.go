// Package vcs is the provider abstraction for token-based Git providers that
// Multica mirrors pull requests and CI status from: Forgejo, Gitea (Forgejo's
// upstream, wire-identical), and GitLab. GitHub is intentionally NOT a vcs
// provider — its App/installation model and check_suite CI differ enough that
// it keeps its own handler (server/internal/handler/github.go).
//
// Each provider only contributes the parts that actually differ between
// providers: how a webhook is authenticated, how its event/payload shapes map to
// the normalized PR/CI structs below, and how a token is validated. The shared
// storage, issue auto-link / auto-close, and broadcast logic live once in the
// handler layer and consume the normalized types.
package vcs

import (
	"context"
	"errors"
	"net/http"
)

// Kind identifies a provider. The string values are persisted on
// vcs_connection.provider and used as the registry key.
type Kind string

const (
	KindForgejo Kind = "forgejo"
	KindGitea   Kind = "gitea"
	KindGitLab  Kind = "gitlab"
)

// Valid reports whether k is a known provider kind.
func (k Kind) Valid() bool {
	switch k {
	case KindForgejo, KindGitea, KindGitLab:
		return true
	}
	return false
}

// ErrUnauthorized is returned by ValidateToken when the instance rejects the
// token (HTTP 401/403). Callers surface it as a connect-time validation
// failure distinct from transport/instance errors.
var ErrUnauthorized = errors.New("vcs: token unauthorized")

// EventKind is the normalized webhook event category. Anything a provider does
// not model maps to EventOther and is acknowledged but ignored.
type EventKind int

const (
	EventOther EventKind = iota
	EventPullRequest
	EventCIStatus
)

// PullRequestEvent is the provider-agnostic shape of a pull/merge request
// webhook. State is already normalized to one of open/closed/merged/draft, so
// the handler never re-derives it. GitLab "merge requests" map onto the same
// struct.
type PullRequestEvent struct {
	// Action is the raw provider action (e.g. "opened", "closed", "merge").
	// The handler only needs to know whether it is terminal; see Terminal.
	Action          string
	RepoOwner       string
	RepoName        string
	Number          int32
	Title           string
	Body            string
	State           string // open | closed | merged | draft
	HTMLURL         string
	Branch          string
	HeadSHA         string
	AuthorLogin     string
	AuthorAvatarURL string
	Additions       int32
	Deletions       int32
	ChangedFiles    int32
	MergedAt        string // RFC3339 or empty
	ClosedAt        string
	CreatedAt       string
	UpdatedAt       string
}

// Terminal reports whether this event is the PR's merge/close event, after
// which the close-intent decision must be frozen. Providers spell the terminal
// action differently (Forgejo "closed"/"merged", GitLab "merge"/"close"), so
// the set is matched here rather than in the handler.
func (e PullRequestEvent) Terminal() bool {
	switch e.Action {
	case "closed", "merged", "merge", "close":
		return true
	}
	return false
}

// CIStatusEvent is the provider-agnostic shape of a commit-status / pipeline
// webhook. State is normalized to passed/failed/pending so the aggregation
// query is provider-independent.
type CIStatusEvent struct {
	SHA         string
	Context     string // status check / pipeline name; "" is allowed
	State       string // passed | failed | pending
	TargetURL   string
	Description string
	// UpdatedAt is the provider's own event timestamp (RFC3339 or empty). It
	// feeds the commit-status monotonic guard so an out-of-order redelivery
	// can't regress a status; empty means "unknown", and the handler falls back
	// to ingestion time.
	UpdatedAt string
}

// Account is the minimal identity returned by ValidateToken.
type Account struct {
	Login string
}

// Provider is the per-provider adapter. Implementations are stateless and cheap
// to construct; the registry holds one instance per kind.
type Provider interface {
	Kind() Kind
	// EventKind classifies an inbound webhook from its headers.
	EventKind(h http.Header) EventKind
	// VerifySignature authenticates the raw body against the connection's
	// stored secret. Forgejo/Gitea use HMAC-SHA256 (X-Gitea-Signature);
	// GitLab uses a plaintext token compare (X-Gitlab-Token).
	VerifySignature(secret string, h http.Header, body []byte) bool
	// ParsePullRequest decodes a pull/merge request webhook body.
	ParsePullRequest(body []byte) (PullRequestEvent, error)
	// ParseCIStatus decodes a commit-status / pipeline webhook body.
	ParseCIStatus(body []byte) (CIStatusEvent, error)
	// ValidateToken confirms the token works against instanceURL and returns
	// the authenticated account. Maps a 401/403 to ErrUnauthorized.
	ValidateToken(ctx context.Context, instanceURL, token string) (Account, error)
}

// registry maps a Kind to its Provider. Populated by package init in the
// adapter files (forgejo.go, gitlab.go).
var registry = map[Kind]Provider{}

func register(p Provider) { registry[p.Kind()] = p }

// For returns the provider for kind, or (nil, false) if unknown.
func For(kind string) (Provider, bool) {
	p, ok := registry[Kind(kind)]
	return p, ok
}
