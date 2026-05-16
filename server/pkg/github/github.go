// Package github implements the small slice of the GitHub REST API that
// Ship Hub Phase 1 needs: listing pull requests for a repo and reading the
// combined commit status for a SHA. We deliberately avoid pulling in
// go-github so the dependency footprint stays small and the response shape
// is mapped exactly to the columns our DB cares about — extra fields would
// just bloat the JSON cache.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// apiBase is the public GitHub REST endpoint. Tests override this on the
// Client struct directly so an httptest server can stand in.
const apiBase = "https://api.github.com"

// Errors returned by the client. Wrapping errors with these so callers
// (the Ship Hub service / handler) can distinguish auth failure from a
// rate limit and respond accordingly.
var (
	// ErrNotFound is returned for HTTP 404. The repo may be private and
	// the token may not have access, or it may not exist at all — GitHub
	// doesn't distinguish, so neither do we.
	ErrNotFound = errors.New("github: not found")
	// ErrUnauthorized is returned for HTTP 401. The token is missing or
	// invalid; the workspace owner needs to re-enter it.
	ErrUnauthorized = errors.New("github: unauthorized")
	// ErrRateLimited is returned for HTTP 403 when the X-RateLimit-Remaining
	// header is "0" — indicates a primary or secondary rate-limit hit, not
	// a permission problem.
	ErrRateLimited = errors.New("github: rate limited")
	// ErrForbidden is returned for HTTP 403 NOT caused by rate-limiting —
	// e.g. SSO required, IP allowlist mismatch, repo archived for writes.
	ErrForbidden = errors.New("github: forbidden")
	// ErrInvalidRepoURL is returned by ParseRepoURL when the input doesn't
	// look like https://github.com/owner/repo.
	ErrInvalidRepoURL = errors.New("github: invalid repo url")
	// ErrUnprocessable is returned for HTTP 422. GitHub's "unprocessable"
	// covers a handful of write-side failures we want to surface as
	// distinct from generic 400s — most importantly "PR is not mergeable"
	// (the merge endpoint returns 405 historically and 422 today; we map
	// both into this typed error so the caller doesn't have to care).
	ErrUnprocessable = errors.New("github: unprocessable")
	// ErrConflict is returned for HTTP 409. Used by the merge endpoint
	// when the head SHA changed mid-flight and by the update-branch
	// endpoint when there's nothing to update.
	ErrConflict = errors.New("github: conflict")
)

// Client is a thin REST wrapper. Zero-config construction (NewClient)
// uses the default HTTP client and the public api.github.com base; tests
// swap BaseURL and HTTPClient via the exported fields.
type Client struct {
	Token      string
	BaseURL    string
	HTTPClient *http.Client
}

// NewClient builds a Client with sensible defaults. token may be empty —
// public-repo requests will still work but at the much lower
// unauthenticated rate limit.
func NewClient(token string) *Client {
	return &Client{
		Token:      token,
		BaseURL:    apiBase,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// ParseRepoURL extracts (owner, repo) from a GitHub https URL. We only
// accept https because we never want to authenticate over http, and the
// project_resource validator already rejects non-https values; this is
// defense in depth.
//
// Accepts trailing ".git" and trailing slashes since users often paste
// what they cloned with.
func ParseRepoURL(rawURL string) (owner, repo string, err error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", "", fmt.Errorf("%w: %v", ErrInvalidRepoURL, err)
	}
	if u.Scheme != "https" || u.Host != "github.com" {
		return "", "", fmt.Errorf("%w: must be https://github.com/...", ErrInvalidRepoURL)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("%w: missing owner/repo", ErrInvalidRepoURL)
	}
	owner = parts[0]
	repo = strings.TrimSuffix(parts[1], ".git")
	return owner, repo, nil
}

// PullRequest is the wire shape we read from GitHub and map to the
// pull_request table. Field names mirror GitHub's REST naming so the
// JSON unmarshal is mechanical.
type PullRequest struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	State   string `json:"state"` // "open" | "closed"
	Draft   bool   `json:"draft"`
	HTMLURL string `json:"html_url"`
	Body    string `json:"body"`
	User    struct {
		Login     string `json:"login"`
		AvatarURL string `json:"avatar_url"`
	} `json:"user"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
	Head struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
	// MergeCommitSHA is the commit GitHub created on the base branch when
	// the PR merged — NOT the PR's head SHA. Empty until the PR is merged.
	// The merge-train reconciler reads this to record the true merged SHA
	// after a missed webhook.
	MergeCommitSHA string     `json:"merge_commit_sha"`
	Additions      int        `json:"additions"`
	Deletions      int        `json:"deletions"`
	ChangedFiles   int        `json:"changed_files"`
	Mergeable      *bool      `json:"mergeable,omitempty"`
	Labels         []Label    `json:"labels"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	MergedAt       *time.Time `json:"merged_at,omitempty"`
	ClosedAt       *time.Time `json:"closed_at,omitempty"`
}

// Label is the minimal label payload we keep — name and color cover the
// Kanban chip rendering needs without bloating the JSONB column.
type Label struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// ListOptions controls /pulls pagination.
type ListOptions struct {
	// State is "open", "closed", or "all". Empty defaults to "open".
	State string
	// PerPage clamps to 100 (GitHub's max). Default 50.
	PerPage int
	// Page is 1-indexed. Default 1. The Ship Hub Phase 1 reconciler only
	// needs the first page; we expose the field for tests + future paging.
	Page int
}

// ListPullRequests calls GET /repos/{owner}/{repo}/pulls and returns the
// parsed slice. The list endpoint does NOT include additions / deletions /
// changed_files / mergeable — those are detail-only fields. We accept that
// trade-off for Phase 1 because rendering the Kanban from the cheap list
// endpoint is far less rate-budget-intensive than per-PR detail fetches;
// the diff-size badge can be filled in later via a follow-up call.
func (c *Client) ListPullRequests(ctx context.Context, owner, repo string, opts ListOptions) ([]PullRequest, error) {
	state := opts.State
	if state == "" {
		state = "open"
	}
	perPage := opts.PerPage
	if perPage <= 0 {
		perPage = 50
	}
	if perPage > 100 {
		perPage = 100
	}
	page := opts.Page
	if page <= 0 {
		page = 1
	}
	q := url.Values{}
	q.Set("state", state)
	q.Set("per_page", strconv.Itoa(perPage))
	q.Set("page", strconv.Itoa(page))
	q.Set("sort", "updated")
	q.Set("direction", "desc")
	path := fmt.Sprintf("/repos/%s/%s/pulls?%s", url.PathEscape(owner), url.PathEscape(repo), q.Encode())

	var out []PullRequest
	if err := c.do(ctx, "GET", path, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// PullRequestFile is the entry shape for GET /repos/{owner}/{repo}/pulls/{n}/files.
// Phase 5 — used by the risk classifier to inspect changed-file paths and
// detect migration / Dockerfile / handler / infra changes. Only filename +
// status + patch are needed today; the rest of the wire shape (sha, blob_url,
// raw_url, contents_url, additions/deletions/changes counts) is ignored.
type PullRequestFile struct {
	// Repo-relative path. "server/migrations/083_x.up.sql" not "/repo/server/...".
	Filename string `json:"filename"`
	// "added" | "modified" | "removed" | "renamed" | "copied" |
	// "changed" | "unchanged".
	Status string `json:"status"`
	// Unified-diff fragment. Capped server-side by GitHub. The classifier
	// scans this for substring matches like "DELETE FROM" / "DROP TABLE"
	// when looking at migration files.
	Patch string `json:"patch"`
	// PreviousFilename is set when the entry is a rename, so the classifier
	// can include both old and new paths in pattern checks.
	PreviousFilename string `json:"previous_filename,omitempty"`
}

// ListPullRequestFiles fetches the changed-file list for a PR. The endpoint
// paginates at 30/page by default — we request 100/page since the classifier
// only consults the first page (a PR with >100 changed files is already
// flagged "high" by changed_files alone in upsertPR). page=1 is the only
// caller we have today.
func (c *Client) ListPullRequestFiles(ctx context.Context, owner, repo string, prNumber int) ([]PullRequestFile, error) {
	path := fmt.Sprintf(
		"/repos/%s/%s/pulls/%d/files?per_page=100&page=1",
		url.PathEscape(owner), url.PathEscape(repo), prNumber,
	)
	var out []PullRequestFile
	if err := c.do(ctx, "GET", path, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CombinedStatus is the wire shape returned by /repos/.../commits/{sha}/status.
// We only project the State field today; the per-context list and total_count
// are useful in a richer UI but not for the Ship Hub badge.
type CombinedStatus struct {
	State string `json:"state"` // "pending" | "success" | "failure" | ""
}

// GetCombinedStatus returns the combined status string for a SHA. Empty
// string when the repo has no statuses configured (a brand-new repo).
func (c *Client) GetCombinedStatus(ctx context.Context, owner, repo, sha string) (string, error) {
	path := fmt.Sprintf("/repos/%s/%s/commits/%s/status",
		url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(sha))
	var out CombinedStatus
	if err := c.do(ctx, "GET", path, &out); err != nil {
		return "", err
	}
	return out.State, nil
}

// GetPullRequest fetches a single PR, including merge_commit_sha (the
// commit GitHub created on the base branch when the PR merged — NOT the
// PR's head SHA). Used by the merge-train reconciler to record the true
// merged SHA after a missed webhook.
func (c *Client) GetPullRequest(ctx context.Context, owner, repo string, prNumber int) (*PullRequest, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d",
		url.PathEscape(owner), url.PathEscape(repo), prNumber)
	var out PullRequest
	if err := c.do(ctx, "GET", path, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// do is the GET/no-body shorthand that delegates to doWithBody. Kept as a
// thin wrapper so existing read-only call sites don't need to thread a
// nil body argument through.
func (c *Client) do(ctx context.Context, method, path string, target any) error {
	return c.doWithBody(ctx, method, path, nil, target)
}

// doWithBody executes a request against the GitHub API with an optional
// JSON body. Centralized so all read AND write calls share auth +
// error-translation behavior.
//
// reqBody, when non-nil, is JSON-marshaled and sent as the request body
// with Content-Type: application/json. target, when non-nil, receives
// the decoded response. Either may be nil — empty-response writes (e.g.
// PUT /update-branch returning 202 No Content) pass nil for both.
func (c *Client) doWithBody(ctx context.Context, method, path string, reqBody, target any) error {
	base := c.BaseURL
	if base == "" {
		base = apiBase
	}
	var bodyReader io.Reader
	if reqBody != nil {
		buf, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("github: encode request body: %w", err)
		}
		bodyReader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, bodyReader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusAccepted, http.StatusNoContent:
		if target == nil || len(body) == 0 {
			return nil
		}
		if err := json.Unmarshal(body, target); err != nil {
			return fmt.Errorf("github: decode response: %w", err)
		}
		return nil
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusUnauthorized:
		return ErrUnauthorized
	case http.StatusForbidden:
		// Rate-limit shows up as 403 with X-RateLimit-Remaining: 0 (primary)
		// or as a "secondary rate limit" message body. Distinguish so the
		// caller can back off vs. surface a permission error to the user.
		if resp.Header.Get("X-RateLimit-Remaining") == "0" {
			return ErrRateLimited
		}
		if strings.Contains(strings.ToLower(string(body)), "secondary rate limit") {
			return ErrRateLimited
		}
		return ErrForbidden
	case http.StatusConflict:
		// Used by the merge endpoint for "head SHA mismatch" and by the
		// update-branch endpoint for "branch is up to date / nothing to
		// merge". Phase 3 surfaces this as a hint, not an error, so the
		// chip can render "already up to date".
		return fmt.Errorf("%w: %s", ErrConflict, truncate(body, 256))
	case http.StatusMethodNotAllowed:
		// GitHub's merge endpoint returns 405 (not 422) when the PR is not
		// mergeable. Bucket it with 422 so the caller has one error to
		// check for "couldn't merge for state-related reasons".
		return fmt.Errorf("%w: %s", ErrUnprocessable, truncate(body, 256))
	case http.StatusUnprocessableEntity:
		return fmt.Errorf("%w: %s", ErrUnprocessable, truncate(body, 256))
	default:
		// Anything else is unexpected — pass through so the caller logs and
		// the operator can debug from the response body.
		return fmt.Errorf("github: %s %s: status %d: %s", method, path, resp.StatusCode, truncate(body, 256))
	}
}

// truncate keeps error logs from ballooning when GitHub returns a giant HTML
// page (e.g. a maintenance window response). 256 bytes is enough to spot
// the issue without flooding the log.
func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}

// MergeResult is the response from PUT /repos/.../pulls/{n}/merge. We
// only project the fields the chip surfaces back to the user — the merge
// SHA + GitHub's natural-language message.
type MergeResult struct {
	SHA     string `json:"sha"`
	Merged  bool   `json:"merged"`
	Message string `json:"message"`
}

// mergePullRequestRequest is the payload PUT /pulls/{n}/merge accepts.
// CommitMessage is intentionally empty: the chip just kicks the merge
// and lets GitHub's default merge-commit message stand.
type mergePullRequestRequest struct {
	SHA           string `json:"sha,omitempty"`
	MergeMethod   string `json:"merge_method,omitempty"`
	CommitTitle   string `json:"commit_title,omitempty"`
	CommitMessage string `json:"commit_message,omitempty"`
}

// MergePullRequest issues PUT /repos/{owner}/{repo}/pulls/{n}/merge.
// Method is one of "merge", "squash", "rebase"; an empty string defaults
// to "merge" (GitHub's documented default). When `sha` is non-empty
// GitHub will refuse to merge if the head has moved — Phase 3 doesn't
// require this guard but threading it through keeps the door open for
// "merge only if SHA still equals X" automations later.
//
// Returns a typed ErrUnprocessable for the "PR not mergeable" case so
// the handler can surface a 422.
func (c *Client) MergePullRequest(ctx context.Context, owner, repo string, prNumber int, method, sha string) (*MergeResult, error) {
	switch method {
	case "", "merge", "squash", "rebase":
	default:
		return nil, fmt.Errorf("github: invalid merge method %q", method)
	}
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/merge",
		url.PathEscape(owner), url.PathEscape(repo), prNumber)
	body := mergePullRequestRequest{MergeMethod: method, SHA: sha}
	var out MergeResult
	if err := c.doWithBody(ctx, "PUT", path, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// updatePullRequestBranchRequest is the body PUT /pulls/{n}/update-branch
// accepts. expected_head_sha is the optimistic-concurrency guard — if
// the head moved since we computed it, GitHub aborts the merge instead
// of clobbering newer commits.
type updatePullRequestBranchRequest struct {
	ExpectedHeadSHA string `json:"expected_head_sha,omitempty"`
}

// UpdatePullRequestBranch triggers GitHub's "Update branch" action — it
// merges the base branch into the head branch (NOT a true rebase; that
// requires either client-side git work or the GraphQL `updatePullRequest`
// with `updateMethod: REBASE`). We surface the merge-into variant as
// "rebase_on_main" in Phase 3 because it's the available primitive for
// non-stacked PRs and matches what most teams actually want when they
// hit the chip.
//
// Returns nil on success (GitHub returns 202 No Content); ErrConflict
// when the branch is already up to date.
func (c *Client) UpdatePullRequestBranch(ctx context.Context, owner, repo string, prNumber int, expectedSHA string) error {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/update-branch",
		url.PathEscape(owner), url.PathEscape(repo), prNumber)
	body := updatePullRequestBranchRequest{ExpectedHeadSHA: expectedSHA}
	return c.doWithBody(ctx, "PUT", path, body, nil)
}

// Comment mirrors GitHub's issue comment shape. Both review-side and
// issue-side comments share the same response wire shape; we use issue
// comments for the chip ("post a PR comment") because they show up in
// the review timeline without needing diff coordinates.
type Comment struct {
	ID      int64  `json:"id"`
	HTMLURL string `json:"html_url"`
	Body    string `json:"body"`
	User    User   `json:"user"`
}

// createCommentRequest is the body POST /issues/{n}/comments accepts.
type createCommentRequest struct {
	Body string `json:"body"`
}

// CreatePullRequestComment posts a comment on the PR conversation tab.
// GitHub treats PRs as issues for comment purposes, so the path uses the
// /issues/.../comments endpoint with the PR number — that's how every
// "general" PR comment is written via the API.
func (c *Client) CreatePullRequestComment(ctx context.Context, owner, repo string, prNumber int, body string) (*Comment, error) {
	if strings.TrimSpace(body) == "" {
		return nil, errors.New("github: comment body is empty")
	}
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments",
		url.PathEscape(owner), url.PathEscape(repo), prNumber)
	var out Comment
	if err := c.doWithBody(ctx, "POST", path, createCommentRequest{Body: body}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// dismissReviewRequest is the body PUT /reviews/{id}/dismissals accepts.
type dismissReviewRequest struct {
	Message string `json:"message"`
	Event   string `json:"event,omitempty"`
}

// DismissPullRequestReview dismisses an existing review. Requires
// repository "admin" permission on the GitHub side; the typed Forbidden
// error lets the handler surface that distinction to the user.
func (c *Client) DismissPullRequestReview(ctx context.Context, owner, repo string, prNumber int, reviewID int64, message string) error {
	if strings.TrimSpace(message) == "" {
		return errors.New("github: dismiss message is required")
	}
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/reviews/%d/dismissals",
		url.PathEscape(owner), url.PathEscape(repo), prNumber, reviewID)
	body := dismissReviewRequest{Message: message, Event: "DISMISS"}
	return c.doWithBody(ctx, "PUT", path, body, nil)
}

// updatePullRequestRequest is the body PATCH /pulls/{n} accepts. We use
// it only for state transitions (closing a stale PR) — re-titling or
// changing the body should go through the dedicated UI flows.
type updatePullRequestRequest struct {
	State string `json:"state,omitempty"`
}

// ClosePullRequest closes the PR (without merging). The author's
// authorization isn't enforced server-side here — the handler must
// project-membership-gate before calling this.
func (c *Client) ClosePullRequest(ctx context.Context, owner, repo string, prNumber int) error {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d",
		url.PathEscape(owner), url.PathEscape(repo), prNumber)
	body := updatePullRequestRequest{State: "closed"}
	return c.doWithBody(ctx, "PATCH", path, body, nil)
}

// ReviewEvent is the verb GitHub accepts on POST /pulls/{n}/reviews.
// Three values per the GitHub REST docs — anything else is a 422.
type ReviewEvent string

const (
	// ReviewEventApprove approves the PR. GitHub permits an empty body.
	ReviewEventApprove ReviewEvent = "APPROVE"
	// ReviewEventRequestChanges requires a non-empty body server-side
	// (GitHub returns 422 without one). The handler validates upstream
	// so the user gets a 400 with a useful message.
	ReviewEventRequestChanges ReviewEvent = "REQUEST_CHANGES"
	// ReviewEventComment posts a plain comment review. GitHub also
	// requires a non-empty body for this event.
	ReviewEventComment ReviewEvent = "COMMENT"
)

// submitReviewRequest is the body POST /pulls/{n}/reviews accepts.
// Comments (per-line review threads) is intentionally omitted — Phase 6.5
// only does whole-PR reviews. Adding inline comments is a future scope.
type submitReviewRequest struct {
	Event ReviewEvent `json:"event"`
	Body  string      `json:"body,omitempty"`
}

// SubmitReview posts a PR review.
//
// Body validation: APPROVE accepts an empty body; COMMENT and
// REQUEST_CHANGES require a body server-side (GitHub returns 422
// without). We mirror that here so the handler can surface a clean
// 400 instead of forwarding GitHub's terse 422 message.
func (c *Client) SubmitReview(ctx context.Context, owner, repo string, prNumber int, event ReviewEvent, body string) (*Review, error) {
	switch event {
	case ReviewEventApprove, ReviewEventRequestChanges, ReviewEventComment:
	default:
		return nil, fmt.Errorf("github: invalid review event %q", event)
	}
	if (event == ReviewEventComment || event == ReviewEventRequestChanges) && strings.TrimSpace(body) == "" {
		return nil, fmt.Errorf("github: review body is required for event %s", event)
	}
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/reviews",
		url.PathEscape(owner), url.PathEscape(repo), prNumber)
	var out Review
	if err := c.doWithBody(ctx, "POST", path, submitReviewRequest{Event: event, Body: body}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// dispatchWorkflowRequest is the body POST /actions/workflows/{file}/dispatches
// accepts. Inputs is a flat map of string keys/values — GitHub coerces
// non-string types but our chip only ever forwards an environment_id
// string so the simpler shape is fine.
type dispatchWorkflowRequest struct {
	Ref    string            `json:"ref"`
	Inputs map[string]string `json:"inputs,omitempty"`
}

// WorkflowRun mirrors the GitHub Actions
// /repos/{owner}/{repo}/actions/workflows/{workflow_id}/runs response
// shape. We only project the fields the Ship Hub deploy poller needs
// (head_sha to match against release.merged_main_sha, conclusion to
// gate "successful run", html_url for the channel announcement). The
// rest of the wire shape (head_commit, repository, actor, etc.) is
// ignored.
type WorkflowRun struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	HeadSHA      string `json:"head_sha"`
	HeadBranch   string `json:"head_branch"`
	Status       string `json:"status"`     // "queued" | "in_progress" | "completed"
	Conclusion   string `json:"conclusion"` // "success" | "failure" | "cancelled" | "skipped" | "timed_out" | ...
	HTMLURL      string `json:"html_url"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
	RunStartedAt string `json:"run_started_at"`
}

// ListWorkflowRunsOptions controls /actions/workflows/{file}/runs paging
// + filters. Defaults: branch="" (no filter), status="" (no filter),
// per_page=10. The deploy poller passes branch="main", status="completed",
// per_page=10 — enough to catch every CI completion in a 2-minute window
// for any sane release cadence.
type ListWorkflowRunsOptions struct {
	Branch  string
	Status  string
	PerPage int
}

// ListWorkflowRuns fetches recent runs for a workflow file. The
// workflow_id parameter accepts both the numeric id and the file name
// (e.g. "production.yml") — GitHub treats both as valid path params.
//
// Used by the Ship Hub deploy workflow poller to discover successful
// runs on main and auto-link the matching release without requiring
// the user to set up GitHub `deployment_status` webhooks (which Vercel,
// Netlify, Cloudflare, and most custom CI providers do not fire).
func (c *Client) ListWorkflowRuns(ctx context.Context, owner, repo, workflowFile string, opts ListWorkflowRunsOptions) ([]WorkflowRun, error) {
	if strings.TrimSpace(workflowFile) == "" {
		return nil, errors.New("github: workflow file is required")
	}
	perPage := opts.PerPage
	if perPage <= 0 {
		perPage = 10
	}
	if perPage > 100 {
		perPage = 100
	}
	q := url.Values{}
	if opts.Branch != "" {
		q.Set("branch", opts.Branch)
	}
	if opts.Status != "" {
		q.Set("status", opts.Status)
	}
	q.Set("per_page", strconv.Itoa(perPage))

	path := fmt.Sprintf("/repos/%s/%s/actions/workflows/%s/runs?%s",
		url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(workflowFile), q.Encode())

	// GitHub wraps the array in `{ "total_count": N, "workflow_runs": [...] }`.
	var wrapper struct {
		WorkflowRuns []WorkflowRun `json:"workflow_runs"`
	}
	if err := c.do(ctx, "GET", path, &wrapper); err != nil {
		return nil, err
	}
	return wrapper.WorkflowRuns, nil
}

// CompareCommitsResult is the subset of GitHub's compare-commits
// response that Ship Hub cares about. `Status` is one of "ahead",
// "behind", "identical", or "diverged" — only the first two are
// useful for ancestor checks. `AheadBy` is the number of commits
// `head` is ahead of `base`; for our use case ("is base an ancestor
// of head") we look for `status` in {"ahead", "identical"} (i.e.
// head includes everything in base).
//
// The response includes much more (commit list, files, etc.); we
// decode only the fields needed to bound the JSON cost. See
// https://docs.github.com/en/rest/commits/commits#compare-two-commits
type CompareCommitsResult struct {
	Status   string `json:"status"`
	AheadBy  int    `json:"ahead_by"`
	BehindBy int    `json:"behind_by"`
}

// CompareCommits asks GitHub whether `head` is a descendant of `base`
// (or identical). Returns the comparison result so the caller can
// decide what to do with each Status.
//
// Used by the deploy linker to verify that a deploy's head_sha
// includes a stuck release's `merged_main_sha` in its git history
// before linking them. Pre-this-method, the deploy linker fell back
// to a time-based heuristic (PR #41) — "the deploy fired after the
// merge, so probably contains it." That's correct most of the time
// but not always (someone can deploy a stale branch from a manual
// workflow_dispatch). Asking git directly is the durable answer.
//
// Path uses `{base}...{head}` (three dots — the three-dot form
// computes the diff *between* the two commits at the merge-base,
// which is what GitHub's API requires). The two-dot variant exists
// in git semantics but isn't what compare/{a}...{b} REST endpoint
// reflects.
//
// Rate-limit cost: 1 call per check. The poller should call this
// only after the strict SHA match misses + a candidate stuck
// release exists — keeps the budget bounded to "at most one extra
// API call per stuck release per poll tick."
func (c *Client) CompareCommits(ctx context.Context, owner, repo, base, head string) (*CompareCommitsResult, error) {
	base = strings.TrimSpace(base)
	head = strings.TrimSpace(head)
	if base == "" || head == "" {
		return nil, errors.New("github: compare requires non-empty base and head")
	}
	path := fmt.Sprintf("/repos/%s/%s/compare/%s...%s",
		url.PathEscape(owner), url.PathEscape(repo),
		url.PathEscape(base), url.PathEscape(head))
	var res CompareCommitsResult
	if err := c.do(ctx, "GET", path, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// IsAncestor returns true when `head` includes `base` in its git
// history (status="ahead" or "identical"). False otherwise — including
// the diverged + behind cases, which mean the release's commits are
// NOT actually deployed.
//
// Wraps CompareCommits with the canonical interpretation for the
// "is this deploy carrying that release's work?" question. Errors
// bubble through unchanged so the caller can choose to log + fall
// back rather than assert.
func (c *Client) IsAncestor(ctx context.Context, owner, repo, base, head string) (bool, error) {
	if base == head {
		return true, nil
	}
	res, err := c.CompareCommits(ctx, owner, repo, base, head)
	if err != nil {
		return false, err
	}
	return res.Status == "ahead" || res.Status == "identical", nil
}

// DispatchWorkflow triggers a workflow_dispatch event on the named
// workflow file. ref is the branch or tag name (NOT a SHA — GitHub
// requires a ref label here). Returns nil on the standard 204 No Content
// success path.
//
// The smoke-tests chip uses this; the workspace setting
// `ship_hub_smoke_workflow` is the workflow file name (e.g.
// "smoke-tests.yml"). The chip is hidden when the setting is empty.
func (c *Client) DispatchWorkflow(ctx context.Context, owner, repo, workflowFile, ref string, inputs map[string]string) error {
	if strings.TrimSpace(workflowFile) == "" {
		return errors.New("github: workflow file is required")
	}
	if strings.TrimSpace(ref) == "" {
		return errors.New("github: dispatch ref is required")
	}
	path := fmt.Sprintf("/repos/%s/%s/actions/workflows/%s/dispatches",
		url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(workflowFile))
	body := dispatchWorkflowRequest{Ref: ref, Inputs: inputs}
	return c.doWithBody(ctx, "POST", path, body, nil)
}
