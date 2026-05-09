// Package github implements the small slice of the GitHub REST API that
// Ship Hub Phase 1 needs: listing pull requests for a repo and reading the
// combined commit status for a SHA. We deliberately avoid pulling in
// go-github so the dependency footprint stays small and the response shape
// is mapped exactly to the columns our DB cares about — extra fields would
// just bloat the JSON cache.
package github

import (
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
	Additions    int        `json:"additions"`
	Deletions    int        `json:"deletions"`
	ChangedFiles int        `json:"changed_files"`
	Mergeable    *bool      `json:"mergeable,omitempty"`
	Labels       []Label    `json:"labels"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	MergedAt     *time.Time `json:"merged_at,omitempty"`
	ClosedAt     *time.Time `json:"closed_at,omitempty"`
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

// do executes a request against the GitHub API and decodes the body into
// `target`. Centralized so all calls go through the same auth + error
// translation path.
func (c *Client) do(ctx context.Context, method, path string, target any) error {
	base := c.BaseURL
	if base == "" {
		base = apiBase
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
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
	case http.StatusOK:
		if target == nil {
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
