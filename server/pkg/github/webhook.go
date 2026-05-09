package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

// Errors returned by signature verification. Distinguished so the
// webhook handler can metric them separately — a missing header is a
// misconfigured sender; a mismatch is potentially malicious.
var (
	ErrMissingSignature = errors.New("github webhook: missing signature header")
	ErrSignatureInvalid = errors.New("github webhook: signature mismatch")
)

// VerifySignature checks that signatureHeader (the X-Hub-Signature-256
// value) matches the HMAC-SHA256 of body using secret. The header is
// formatted as "sha256=<hex>" — we reject anything else upfront so a
// caller can't trick us into accepting an unsupported algorithm.
//
// The actual compare uses hmac.Equal — constant-time per stdlib — so a
// signature mismatch can't be timed.
func VerifySignature(body []byte, signatureHeader, secret string) error {
	if signatureHeader == "" {
		return ErrMissingSignature
	}
	if !strings.HasPrefix(signatureHeader, "sha256=") {
		return ErrSignatureInvalid
	}
	signed := strings.TrimPrefix(signatureHeader, "sha256=")
	provided, err := hex.DecodeString(signed)
	if err != nil {
		return ErrSignatureInvalid
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := mac.Sum(nil)
	if !hmac.Equal(provided, expected) {
		return ErrSignatureInvalid
	}
	return nil
}

// ComputeSignature returns the "sha256=<hex>" value the GitHub webhook
// sender would attach for body+secret. Useful for tests that drive the
// receiver, and for diagnostics ("here's what the secret WOULD produce
// for this payload").
func ComputeSignature(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// WebhookHeaders captures the bits of the request envelope downstream
// processing cares about. Decoded once by the handler so the dispatch
// logic doesn't reach back into *http.Request.
type WebhookHeaders struct {
	DeliveryID string
	EventType  string
}

// PullRequestEvent is the slice of GitHub's `pull_request` event payload
// we persist. Field names follow GitHub's naming so json.Unmarshal can
// hit the wire shape directly.
type PullRequestEvent struct {
	Action      string      `json:"action"`
	Number      int         `json:"number"`
	PullRequest PullRequest `json:"pull_request"`
	Repository  Repository  `json:"repository"`
	Sender      User        `json:"sender"`
}

// PullRequestReviewEvent fires on pull_request_review.submitted/edited/
// dismissed. The review block carries the state we project into our
// pull_request_review table.
type PullRequestReviewEvent struct {
	Action      string      `json:"action"`
	PullRequest PullRequest `json:"pull_request"`
	Review      Review      `json:"review"`
	Repository  Repository  `json:"repository"`
}

// Review mirrors the wire shape of the review block. Used by both the
// pull_request_review webhook payload AND the response of POST
// /repos/.../pulls/{n}/reviews (Phase 6.5 in-app review submission).
//
// Why share a type? Both are GitHub's same Review object — the webhook
// just delivers it via webhook plumbing and SubmitReview returns it
// directly. SubmittedAt is time.Time because both webhook and REST
// emit it in RFC3339; net/http auto-decodes via time.Time's
// UnmarshalJSON. HTMLURL is populated by REST but absent on some
// webhook payload variants — that's fine since it's an optional
// pointer-to-renderable in the chip toast.
type Review struct {
	ID          int64     `json:"id"`
	HTMLURL     string    `json:"html_url"`
	State       string    `json:"state"` // "approved" | "changes_requested" | "commented" | "dismissed" | "APPROVED" | ...
	Body        string    `json:"body"`
	SubmittedAt time.Time `json:"submitted_at"`
	User        User      `json:"user"`
}

// CheckRunEvent fires on check_run.completed (we ignore the lifecycle
// states; until completed there's no conclusion to roll up).
type CheckRunEvent struct {
	Action     string     `json:"action"`
	CheckRun   CheckRun   `json:"check_run"`
	Repository Repository `json:"repository"`
}

// CheckRun wire shape. PullRequests is GitHub's join — a check_run
// belongs to a head_sha; the repo's open PRs whose head_sha matches are
// nested inline so we can derive (pull_request, head_sha) without an
// extra API call.
type CheckRun struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	HeadSHA      string    `json:"head_sha"`
	Status       string    `json:"status"`     // "queued" | "in_progress" | "completed"
	Conclusion   string    `json:"conclusion"` // "" until status=completed
	DetailsURL   string    `json:"details_url"`
	HTMLURL      string    `json:"html_url"`
	StartedAt    time.Time `json:"started_at"`
	CompletedAt  time.Time `json:"completed_at"`
	PullRequests []struct {
		Number int `json:"number"`
		Head   struct {
			SHA string `json:"sha"`
		} `json:"head"`
	} `json:"pull_requests"`
}

// StatusEvent fires for the legacy combined-status API. Important for
// older repos that haven't migrated to check_runs.
type StatusEvent struct {
	SHA        string     `json:"sha"`
	Name       string     `json:"name"` // repo name, e.g. "owner/repo"
	State      string     `json:"state"`
	Context    string     `json:"context"`
	TargetURL  string     `json:"target_url"`
	Repository Repository `json:"repository"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// DeploymentEvent fires when a deployment is created. We map this to a
// new `deploy` row in `pending` state.
type DeploymentEvent struct {
	Action     string     `json:"action"`
	Deployment Deployment `json:"deployment"`
	Repository Repository `json:"repository"`
}

// Deployment wire shape.
type Deployment struct {
	ID          int64     `json:"id"`
	SHA         string    `json:"sha"`
	Ref         string    `json:"ref"`
	Environment string    `json:"environment"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

// DeploymentStatusEvent fires for status transitions — success, failure,
// in_progress. Carries the parent Deployment for sha/env disambiguation.
type DeploymentStatusEvent struct {
	Action           string           `json:"action"`
	DeploymentStatus DeploymentStatus `json:"deployment_status"`
	Deployment       Deployment       `json:"deployment"`
	Repository       Repository       `json:"repository"`
}

type DeploymentStatus struct {
	ID          int64     `json:"id"`
	State       string    `json:"state"` // "success" | "failure" | "in_progress" | "queued" | "error" | "inactive"
	LogURL      string    `json:"log_url"`
	TargetURL   string    `json:"target_url"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// PushEvent fires on a `git push`. We listen for merges to the default
// branch so the per-repo PR list refreshes without waiting on the
// 5-minute reconciler.
type PushEvent struct {
	Ref        string     `json:"ref"` // e.g. "refs/heads/main"
	Before     string     `json:"before"`
	After      string     `json:"after"`
	Repository Repository `json:"repository"`
}

// Repository is the slice of GitHub's repo object we extract URLs from.
// HTMLURL is what the user pasted as a github_repo project_resource;
// matching by URL is the join key for "which workspace's webhook is
// this".
type Repository struct {
	ID            int64  `json:"id"`
	FullName      string `json:"full_name"`
	HTMLURL       string `json:"html_url"`
	DefaultBranch string `json:"default_branch"`
}

// User wire shape. Reused by review.user and pull_request.user.
type User struct {
	Login     string `json:"login"`
	AvatarURL string `json:"avatar_url"`
}
