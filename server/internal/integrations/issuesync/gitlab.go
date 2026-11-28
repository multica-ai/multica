package issuesync

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// GitLabClient reads and writes issues through the GitLab REST API v4.
//
// The sync talks to GitLab directly rather than through the vcs provider
// abstraction: that package models pull requests and CI status, which is a
// different surface with a different webhook contract. Sharing it would mean
// widening it for a caller it was not designed for.
type GitLabClient struct {
	BaseURL   string
	Token     string
	ProjectID int64
	HTTP      *http.Client
}

// NewGitLabClient returns a client for one project. A run is scoped to a single
// project because the link marker stores the project id, and matching across
// projects would need a composite key the marker does not carry.
func NewGitLabClient(baseURL, token string, projectID int64) *GitLabClient {
	return &GitLabClient{
		BaseURL:   strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		Token:     token,
		ProjectID: projectID,
		HTTP:      &http.Client{Timeout: 30 * time.Second},
	}
}

// gitlabIssue is the subset of the GitLab issue payload the sync reads.
type gitlabIssue struct {
	IID         int64    `json:"iid"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	State       string   `json:"state"`
	Labels      []string `json:"labels"`
	DueDate     *string  `json:"due_date"`
	UpdatedAt   string   `json:"updated_at"`
}

// ListIssues returns every issue in the project, following pagination.
//
// scope=all is required: the default scope=created_by hides issues the token's
// user did not open, which would silently drop most of a team's board from the
// mirror.
func (c *GitLabClient) ListIssues(ctx context.Context) ([]Issue, error) {
	const pageSize = 100
	var issues []Issue
	for page := 1; ; page++ {
		query := url.Values{}
		query.Set("scope", "all")
		query.Set("state", "all")
		query.Set("per_page", strconv.Itoa(pageSize))
		query.Set("page", strconv.Itoa(page))
		query.Set("order_by", "created_at")
		query.Set("sort", "asc")

		var raw []gitlabIssue
		headers, err := c.do(ctx, http.MethodGet, c.projectPath("/issues")+"?"+query.Encode(), nil, &raw)
		if err != nil {
			return nil, fmt.Errorf("list issues page %d: %w", page, err)
		}
		for _, issue := range raw {
			issues = append(issues, issue.toIssue())
		}
		// GitLab signals the end with an empty page or a missing next-page
		// header. Checking both keeps a server that omits the header from
		// looping forever on the last page.
		if len(raw) < pageSize || headers.Get("X-Next-Page") == "" {
			return issues, nil
		}
	}
}

func (g gitlabIssue) toIssue() Issue {
	issue := Issue{
		Side:      SideGitLab,
		Key:       strconv.FormatInt(g.IID, 10),
		Title:     g.Title,
		Body:      g.Description,
		UpdatedAt: parseTime(g.UpdatedAt),
	}
	if status, ok := StatusFromLabels(g.Labels); ok {
		issue.Status = status
	}
	if g.DueDate != nil {
		issue.DueDate = *g.DueDate
	}
	return issue
}

// UpdateIssue writes the fields a decision selected, then reconciles the status
// label and the open/closed state.
//
// The status label and the state are written separately from the content
// fields because GitLab has no single call that sets both: the label is part of
// the issue body update, while closing is a state event. current is the tracker
// issue as it was read, which is what names the label to remove.
func (c *GitLabClient) UpdateIssue(ctx context.Context, iid int64, fields []string, from, current Issue) error {
	body := map[string]any{}
	for _, field := range fields {
		switch field {
		case FieldTitle:
			body["title"] = from.Title
		case FieldDescription:
			body["description"] = from.Body
		case FieldDueDate:
			body["due_date"] = from.DueDate
		}
	}
	if len(body) > 0 {
		if _, err := c.do(ctx, http.MethodPut, c.projectPath("/issues/"+strconv.FormatInt(iid, 10)), body, nil); err != nil {
			return fmt.Errorf("update issue %d: %w", iid, err)
		}
	}
	if containsField(fields, FieldStatus) {
		if err := c.setStatus(ctx, iid, from.Status, current.Status); err != nil {
			return err
		}
	}
	return nil
}

// setStatus applies a canonical status to a GitLab issue: the status label
// carries the value, and the issue state follows whether that status closes it.
//
// The new label is added before the old one is removed, so a failure between
// the two calls leaves two status labels rather than none. StatusFromLabels
// takes the first recognized label, so the next run still resolves a status and
// finishes the cleanup instead of losing the value.
func (c *GitLabClient) setStatus(ctx context.Context, iid int64, status, previous string) error {
	label, ok := StatusLabel(status)
	if !ok {
		return fmt.Errorf("issue %d: status %q has no tracker label", iid, status)
	}
	path := c.projectPath("/issues/" + strconv.FormatInt(iid, 10))
	if _, err := c.do(ctx, http.MethodPut, path, map[string]any{"add_labels": label}, nil); err != nil {
		return fmt.Errorf("add status label to issue %d: %w", iid, err)
	}
	// GitLab removes labels by exact name, not by prefix, so the label to drop
	// has to be named. previous is the status the issue carried when it was
	// read; an issue with no status label has nothing to remove.
	if previous != "" && previous != status {
		if old, ok := StatusLabel(previous); ok {
			if _, err := c.do(ctx, http.MethodPut, path, map[string]any{"remove_labels": old}, nil); err != nil {
				return fmt.Errorf("remove status label from issue %d: %w", iid, err)
			}
		}
	}
	return c.setState(ctx, iid, IsClosing(status))
}

// setState opens or closes the issue to match the status. GitLab treats these
// as events, not fields, so each is its own endpoint.
func (c *GitLabClient) setState(ctx context.Context, iid int64, closed bool) error {
	action := "close"
	if !closed {
		action = "reopen"
	}
	path := c.projectPath("/issues/" + strconv.FormatInt(iid, 10) + "/" + action)
	if _, err := c.do(ctx, http.MethodPost, path, nil, nil); err != nil {
		return fmt.Errorf("%s issue %d: %w", action, iid, err)
	}
	return nil
}

// CreateIssue creates the GitLab counterpart of a Multica issue and returns the
// created issue, whose IID the caller stores in the Multica marker.
func (c *GitLabClient) CreateIssue(ctx context.Context, from Issue) (Issue, error) {
	body := map[string]any{
		"title":       from.Title,
		"description": from.Body,
	}
	if from.DueDate != "" {
		body["due_date"] = from.DueDate
	}
	if label, ok := StatusLabel(from.Status); ok {
		body["labels"] = label
	}
	var created gitlabIssue
	if _, err := c.do(ctx, http.MethodPost, c.projectPath("/issues"), body, &created); err != nil {
		return Issue{}, fmt.Errorf("create issue %q: %w", from.Title, err)
	}
	issue := created.toIssue()
	// A newly created issue is open. A status that closes it has to be applied
	// as an event, which the create call cannot do.
	if IsClosing(from.Status) {
		if err := c.setState(ctx, created.IID, true); err != nil {
			return Issue{}, err
		}
	}
	return issue, nil
}

// projectPath builds an API path under the configured project.
func (c *GitLabClient) projectPath(suffix string) string {
	return "/api/v4/projects/" + strconv.FormatInt(c.ProjectID, 10) + suffix
}

// do performs one request and decodes a JSON response into out when out is not
// nil. It returns the response headers so a caller can read pagination.
func (c *GitLabClient) do(ctx context.Context, method, path string, body, out any) (http.Header, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("PRIVATE-TOKEN", c.Token)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("%s %s: status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(detail)))
	}
	if out == nil {
		return resp.Header, nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return nil, fmt.Errorf("%s %s: decode response: %w", method, path, err)
	}
	return resp.Header, nil
}

// containsField reports whether fields names field.
func containsField(fields []string, field string) bool {
	for _, candidate := range fields {
		if candidate == field {
			return true
		}
	}
	return false
}
