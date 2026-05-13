// Package github implements the GitHub webhook source adapter for the
// cascade subsystem. Replaces the GitHubStub shipped in PR2.
//
// Subscribes to four event types — the union of events the PUL-102
// cascade reacts to:
//
//	workflow_run.completed  conclusion=failure → ci_failure
//	check_run.completed     conclusion=failure → ci_failure
//	pull_request.closed     merged=true        → pr_merged
//	pull_request.edited     title change only  → pr_title_edit (G4 fallback)
//	pull_request_review.submitted state=changes_requested → pr_review_change (E2)
//
// Success / pending / ignored variants of every event return
// ErrUnsupportedEvent → router responds 204.
//
// Schema version pin: the adapter rejects payloads that do not match
// the pinned shape (validated by required-field presence). The
// constraint "Schema mismatch — явный fail, не молчаливый парсинг"
// from the plan: a future GitHub schema change surfaces as a 400 +
// alert, not a silent miss.
package github

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/webhooks"
)

// SourceName is the registry key (and URL path segment). Exposed as a
// constant so the wiring in webhooks.MountFromEnv and tests reference
// the same string.
const SourceName = "github"

// SignatureHeaderName is the HTTP header GitHub uses for HMAC-SHA256
// signatures. The Verification scheme is implemented generically in
// webhooks.VerifyHMACSHA256.
const SignatureHeaderName = "X-Hub-Signature-256"

// eventTypeHeader is the GitHub event type. Always present on real
// deliveries; missing → schema mismatch.
const eventTypeHeader = "X-GitHub-Event"

// deliveryHeader is the GitHub delivery GUID. Always present; used as
// the seed for event_id (deterministic UUIDv5 → re-deliveries collide
// in cascade_retrigger).
const deliveryHeader = "X-GitHub-Delivery"

// deliveryNamespace is the UUIDv5 namespace for GitHub delivery IDs.
// Chosen once and pinned — never edit, or every existing
// cascade_retrigger.event_id becomes orphaned. The namespace is a
// random UUID I generated for this PR; tying it to a non-secret
// constant means dedup survives across multica restarts and
// horizontal scaling.
var deliveryNamespace = uuid.MustParse("a3b6f8e2-72c5-4b8b-9d1f-8d3b9c4f5a10")

// Config is the runtime config for the GitHub adapter. Loaded once
// at MountFromEnv time from env vars and held by the Source instance.
type Config struct {
	// SecretCurrent is the active HMAC secret. Required for HMAC
	// verification to succeed — register-time validation in
	// webhooks.Router panics if this is empty.
	SecretCurrent string

	// SecretPrevious is the rotated-out secret kept warm for up to
	// 24h so in-flight retries from GitHub still verify after a key
	// rotation. May be empty in steady state.
	SecretPrevious string
}

// Source implements webhooks.Source for GitHub.
type Source struct {
	cfg Config
}

// New returns a Source configured for the given secrets.
func New(cfg Config) *Source {
	return &Source{cfg: cfg}
}

// Name implements webhooks.Source.
func (*Source) Name() string { return SourceName }

// SignatureHeader implements webhooks.Source.
func (*Source) SignatureHeader() string { return SignatureHeaderName }

// Secrets implements webhooks.Source.
func (s *Source) Secrets() (string, string) {
	return s.cfg.SecretCurrent, s.cfg.SecretPrevious
}

// Normalize parses the incoming GitHub webhook and produces a
// TriggerEvent. Validates schema by requiring the fields the cascade
// pipeline reads. Anything missing → ErrSchemaMismatch. Success /
// ignored variants → ErrUnsupportedEvent.
func (s *Source) Normalize(r *http.Request) (*webhooks.TriggerEvent, error) {
	eventType := r.Header.Get(eventTypeHeader)
	if eventType == "" {
		return nil, fmt.Errorf("%w: missing %s header", webhooks.ErrSchemaMismatch, eventTypeHeader)
	}
	deliveryID := r.Header.Get(deliveryHeader)
	if deliveryID == "" {
		return nil, fmt.Errorf("%w: missing %s header", webhooks.ErrSchemaMismatch, deliveryHeader)
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	switch eventType {
	case "workflow_run":
		return s.normalizeWorkflowRun(body, deliveryID)
	case "check_run":
		return s.normalizeCheckRun(body, deliveryID)
	case "pull_request":
		return s.normalizePullRequest(body, deliveryID)
	case "pull_request_review":
		return s.normalizePullRequestReview(body, deliveryID)
	case "ping":
		// GitHub sends a ping on App install / webhook re-config so
		// the operator can verify wiring. Always answer 204.
		return nil, webhooks.ErrUnsupportedEvent
	default:
		// Any event type we did not subscribe to → 204. GitHub
		// re-delivery policy treats 2xx as "do not retry", so this
		// is the right answer.
		return nil, webhooks.ErrUnsupportedEvent
	}
}

// EventID derives the deterministic event_id from a GitHub delivery
// GUID. Exported so PR4 worker tests and PR8 reconciliation can
// compute the same UUID without re-instantiating the Source.
func EventID(deliveryID string) uuid.UUID {
	return uuid.NewSHA1(deliveryNamespace, []byte(deliveryID))
}

// --- payload structs ---
//
// Each event type has a minimal struct holding only the fields the
// cascade pipeline reads. Unmarshalling the whole GitHub payload
// would be wasteful (each delivery is ~50KB) and brittle (GitHub
// adds fields routinely). Only required fields → ErrSchemaMismatch
// on missing.

type workflowRunPayload struct {
	Action      string `json:"action"`
	WorkflowRun struct {
		Conclusion string `json:"conclusion"`
		HeadSHA    string `json:"head_sha"`
		HeadBranch string `json:"head_branch"`
		PullRequests []struct {
			Number  int32  `json:"number"`
			HTMLURL string `json:"html_url"`
		} `json:"pull_requests"`
	} `json:"workflow_run"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

func (s *Source) normalizeWorkflowRun(body []byte, deliveryID string) (*webhooks.TriggerEvent, error) {
	var p workflowRunPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("%w: %v", webhooks.ErrSchemaMismatch, err)
	}
	if p.Action != "completed" {
		return nil, webhooks.ErrUnsupportedEvent
	}
	if p.WorkflowRun.Conclusion != "failure" {
		// Only failures drive the cascade — success / cancelled /
		// neutral / timed_out are all "do not wake the agent".
		return nil, webhooks.ErrUnsupportedEvent
	}
	if p.WorkflowRun.HeadSHA == "" || p.Repository.FullName == "" {
		return nil, fmt.Errorf("%w: workflow_run missing head_sha or repository.full_name", webhooks.ErrSchemaMismatch)
	}
	// workflow_run events can carry zero, one, or many PRs in the
	// pull_requests array. Cascade only reacts when exactly one PR
	// is involved — anything else (fork PR, no-PR push run) skips.
	if len(p.WorkflowRun.PullRequests) != 1 {
		return nil, webhooks.ErrUnsupportedEvent
	}
	pr := p.WorkflowRun.PullRequests[0]
	if pr.HTMLURL == "" || pr.Number == 0 {
		return nil, fmt.Errorf("%w: workflow_run.pull_requests missing html_url or number", webhooks.ErrSchemaMismatch)
	}
	return &webhooks.TriggerEvent{
		EventID:   EventID(deliveryID),
		EventType: webhooks.EventTypeCIFailure,
		PRURL:     pr.HTMLURL,
		PRNumber:  int(pr.Number),
		// workflow_run does not carry the PR title or branch
		// directly on the top-level payload. PR lookup uses head_sha
		// + repo via the worker's GitHub API call (PR4 G5 state
		// validation also hits the API for the same row), so
		// leaving these blank is fine — the worker fetches them.
		PRTitle: "",
		HeadSHA: p.WorkflowRun.HeadSHA,
		Branch:  p.WorkflowRun.HeadBranch,
	}, nil
}

type checkRunPayload struct {
	Action   string `json:"action"`
	CheckRun struct {
		Conclusion string `json:"conclusion"`
		HeadSHA    string `json:"head_sha"`
		HTMLURL    string `json:"html_url"`
		PullRequests []struct {
			Number  int32  `json:"number"`
			HTMLURL string `json:"html_url"`
		} `json:"pull_requests"`
	} `json:"check_run"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

func (s *Source) normalizeCheckRun(body []byte, deliveryID string) (*webhooks.TriggerEvent, error) {
	var p checkRunPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("%w: %v", webhooks.ErrSchemaMismatch, err)
	}
	if p.Action != "completed" {
		return nil, webhooks.ErrUnsupportedEvent
	}
	if p.CheckRun.Conclusion != "failure" {
		return nil, webhooks.ErrUnsupportedEvent
	}
	if p.CheckRun.HeadSHA == "" || p.Repository.FullName == "" {
		return nil, fmt.Errorf("%w: check_run missing head_sha or repository.full_name", webhooks.ErrSchemaMismatch)
	}
	if len(p.CheckRun.PullRequests) != 1 {
		return nil, webhooks.ErrUnsupportedEvent
	}
	pr := p.CheckRun.PullRequests[0]
	if pr.HTMLURL == "" || pr.Number == 0 {
		return nil, fmt.Errorf("%w: check_run.pull_requests missing html_url or number", webhooks.ErrSchemaMismatch)
	}
	return &webhooks.TriggerEvent{
		EventID:   EventID(deliveryID),
		EventType: webhooks.EventTypeCIFailure,
		PRURL:     pr.HTMLURL,
		PRNumber:  int(pr.Number),
		HeadSHA:   p.CheckRun.HeadSHA,
	}, nil
}

type pullRequestPayload struct {
	Action      string `json:"action"`
	Number      int32  `json:"number"`
	PullRequest struct {
		HTMLURL string `json:"html_url"`
		Title   string `json:"title"`
		Merged  bool   `json:"merged"`
		Head    struct {
			SHA string `json:"sha"`
			Ref string `json:"ref"`
		} `json:"head"`
	} `json:"pull_request"`
	Changes struct {
		Title struct {
			From string `json:"from"`
		} `json:"title"`
	} `json:"changes"`
}

func (s *Source) normalizePullRequest(body []byte, deliveryID string) (*webhooks.TriggerEvent, error) {
	var p pullRequestPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("%w: %v", webhooks.ErrSchemaMismatch, err)
	}
	if p.PullRequest.HTMLURL == "" || p.Number == 0 {
		return nil, fmt.Errorf("%w: pull_request missing html_url or number", webhooks.ErrSchemaMismatch)
	}

	common := webhooks.TriggerEvent{
		EventID:  EventID(deliveryID),
		PRURL:    p.PullRequest.HTMLURL,
		PRNumber: int(p.Number),
		PRTitle:  p.PullRequest.Title,
		HeadSHA:  p.PullRequest.Head.SHA,
		Branch:   p.PullRequest.Head.Ref,
	}

	switch p.Action {
	case "closed":
		if !p.PullRequest.Merged {
			// Closed without merge — user cancelled, agent shouldn't
			// continue the cascade off a dead PR. Worker handles
			// cascade_state transition; the event itself is not
			// interesting to the router.
			return nil, webhooks.ErrUnsupportedEvent
		}
		evt := common
		evt.EventType = webhooks.EventTypePRMerged
		return &evt, nil

	case "edited":
		// Only title edits are interesting (G4 fallback safety net
		// for when the [PUL-N] prefix gets dropped). The `changes`
		// object holds the previous title; if not present, the edit
		// was on a non-title field and we skip.
		if p.Changes.Title.From == "" {
			return nil, webhooks.ErrUnsupportedEvent
		}
		evt := common
		evt.EventType = webhooks.EventTypePRTitleEdit
		return &evt, nil

	default:
		// pull_request.{opened,reopened,synchronize,...} are out of
		// scope — agents drive their own opens, and 'synchronize'
		// (new commits pushed) is already covered by the
		// workflow_run / check_run failure path.
		return nil, webhooks.ErrUnsupportedEvent
	}
}

type pullRequestReviewPayload struct {
	Action string `json:"action"`
	Review struct {
		State string `json:"state"`
	} `json:"review"`
	PullRequest struct {
		Number  int32  `json:"number"`
		HTMLURL string `json:"html_url"`
		Title   string `json:"title"`
		Head    struct {
			SHA string `json:"sha"`
			Ref string `json:"ref"`
		} `json:"head"`
	} `json:"pull_request"`
}

func (s *Source) normalizePullRequestReview(body []byte, deliveryID string) (*webhooks.TriggerEvent, error) {
	var p pullRequestReviewPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("%w: %v", webhooks.ErrSchemaMismatch, err)
	}
	if p.Action != "submitted" {
		return nil, webhooks.ErrUnsupportedEvent
	}
	// Only "changes_requested" gates the agent. Approvals don't wake
	// it — they reduce friction, not introduce it. Plain comments
	// (state="commented") also skip; if a comment matters, the
	// reviewer will request changes.
	if !strings.EqualFold(p.Review.State, "changes_requested") {
		return nil, webhooks.ErrUnsupportedEvent
	}
	if p.PullRequest.HTMLURL == "" || p.PullRequest.Number == 0 {
		return nil, fmt.Errorf("%w: pull_request_review missing pull_request fields", webhooks.ErrSchemaMismatch)
	}
	return &webhooks.TriggerEvent{
		EventID:   EventID(deliveryID),
		EventType: webhooks.EventTypePRReviewChange,
		PRURL:     p.PullRequest.HTMLURL,
		PRNumber:  int(p.PullRequest.Number),
		PRTitle:   p.PullRequest.Title,
		HeadSHA:   p.PullRequest.Head.SHA,
		Branch:    p.PullRequest.Head.Ref,
	}, nil
}

// FromEnv reads the GitHub secrets from env vars. Returns nil when
// the current secret is missing — caller falls back to the stub.
// Helper exists so webhooks.MountFromEnv can decide at registration
// time whether to wire the real adapter or leave the stub in place
// (e.g. dev box without GitHub App configured).
func FromEnv(getenv func(string) string) *Source {
	current := strings.TrimSpace(getenv("MULTICA_GITHUB_WEBHOOK_SECRET_CURRENT"))
	if current == "" {
		return nil
	}
	previous := strings.TrimSpace(getenv("MULTICA_GITHUB_WEBHOOK_SECRET_PREVIOUS"))
	return New(Config{SecretCurrent: current, SecretPrevious: previous})
}

// ensure interface satisfaction at compile time.
var _ webhooks.Source = (*Source)(nil)
