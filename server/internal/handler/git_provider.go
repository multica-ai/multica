package handler

import (
	"net/http"
	"net/url"
)

// GitProvider abstracts a git-hosting backend (GitHub today; Gitea in Phase B)
// behind the webhook mirror pipeline. The pipeline itself — PR upsert, issue
// auto-link, close-intent gating, check-suite replay, merge→done advance — is
// provider-neutral and lives in the handler. A provider owns only the vendor
// edges: signature verification, event decoding, PR-state mapping, and the
// instance identity stamped on mirrored rows.
type GitProvider interface {
	// Name is the stable provider key persisted on mirrored rows ("github").
	Name() string
	// InstanceBaseURL is the provider instance's web base URL
	// ("https://github.com"). Its host stamps mirrored rows so the same
	// owner/name/number on different instances stay distinct.
	InstanceBaseURL() string
	// VerifySignature reports whether the webhook body carries a valid
	// signature for this provider's configured secret.
	VerifySignature(headers http.Header, body []byte) bool
	// ParseEvent decodes a raw webhook delivery into a neutral GitEvent.
	// A body that fails to decode for a known event kind returns an error;
	// an unmodeled event kind returns GitEventUnknown with a nil error.
	ParseEvent(headers http.Header, body []byte) (GitEvent, error)
	// DerivePRState maps the provider's raw PR fields to a mirror state
	// (one of open / draft / closed / merged).
	DerivePRState(state string, draft, merged bool) string
}

// providerBaseHost is the value stamped on the base_host column: the host of
// the provider's instance URL, falling back to the provider name when the URL
// has no parseable host.
func providerBaseHost(p GitProvider) string {
	u, err := url.Parse(p.InstanceBaseURL())
	if err != nil || u.Host == "" {
		return p.Name()
	}
	return u.Host
}

// GitEventKind classifies a decoded webhook delivery.
type GitEventKind string

const (
	GitEventPing         GitEventKind = "ping"
	GitEventInstallation GitEventKind = "installation"
	GitEventPullRequest  GitEventKind = "pull_request"
	GitEventCheckSuite   GitEventKind = "check_suite"
	GitEventUnknown      GitEventKind = "unknown"
)

// GitEvent is the neutral shape the mirror pipeline consumes. Exactly one of
// the payload pointers is set for the Installation / PullRequest / CheckSuite
// kinds; Ping and Unknown carry none.
type GitEvent struct {
	Kind         GitEventKind
	Installation *GitInstallationEvent
	PullRequest  *GitPullRequestEvent
	CheckSuite   *GitCheckSuiteEvent
}

// GitInstallationEvent is the neutral projection of an installation lifecycle
// webhook. AccountLogin is empty when the payload omitted it; the handler
// treats an empty login as "no account metadata to apply".
type GitInstallationEvent struct {
	Action           string
	InstallationID   int64
	AccountLogin     string
	AccountType      string
	AccountAvatarURL *string
}

// GitPullRequestEvent is the neutral projection of a pull_request webhook.
// BaseRefChanged is precomputed by the provider from the raw payload's change
// set; the mirror pipeline uses it to decide when a stale mergeable verdict
// must be blanked.
type GitPullRequestEvent struct {
	Action          string
	InstallationID  int64
	RepoOwner       string
	RepoName        string
	Number          int32
	HTMLURL         string
	Title           string
	Body            string
	State           string
	Draft           bool
	Merged          bool
	MergedAt        string
	ClosedAt        string
	CreatedAt       string
	UpdatedAt       string
	MergeableState  string
	BaseRefChanged  bool
	Additions       int32
	Deletions       int32
	ChangedFiles    int32
	HeadRef         string
	HeadSHA         string
	AuthorLogin     string
	AuthorAvatarURL string
}

// GitCheckSuiteEvent is the neutral projection of a check_suite webhook. A
// suite may reference several PRs by number (same head SHA across base
// branches), so PRNumbers is a slice.
type GitCheckSuiteEvent struct {
	Action         string
	InstallationID int64
	RepoOwner      string
	RepoName       string
	SuiteID        int64
	HeadSHA        string
	Status         string
	Conclusion     string
	UpdatedAt      string
	AppID          int64
	PRNumbers      []int32
}
