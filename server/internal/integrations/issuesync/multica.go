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

// MulticaClient reads and writes issues through the workspace REST API.
//
// It is a plain HTTP client rather than a service-layer call on purpose: the
// sync runs as an operator command against a live deployment, so it must go
// through the same authenticated surface a person uses. That also keeps the
// package free of database access, which is what makes the whole contract
// testable without a running server.
type MulticaClient struct {
	BaseURL     string
	Token       string
	WorkspaceID string
	HTTP        *http.Client
}

// NewMulticaClient returns a client with a bounded timeout. The sync walks
// pages of issues, so a hung request must fail rather than stall the run.
func NewMulticaClient(baseURL, token, workspaceID string) *MulticaClient {
	return &MulticaClient{
		BaseURL:     strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		Token:       token,
		WorkspaceID: workspaceID,
		HTTP:        &http.Client{Timeout: 30 * time.Second},
	}
}

// multicaIssue is the subset of the issue payload the sync reads. Fields the
// mirror does not compare are absent rather than decoded and ignored, so a
// server-side addition cannot silently enter the comparison.
type multicaIssue struct {
	ID          string  `json:"id"`
	Identifier  string  `json:"identifier"`
	Title       string  `json:"title"`
	Description *string `json:"description"`
	Status      string  `json:"status"`
	Priority    string  `json:"priority"`
	DueDate     *string `json:"due_date"`
	UpdatedAt   string  `json:"updated_at"`
}

// multicaIssuePage is the list envelope. total is advisory: the server answers
// with the page size when its count query fails, so the walk trusts an empty
// page to end it rather than trusting total.
type multicaIssuePage struct {
	Issues []multicaIssue `json:"issues"`
	Total  int            `json:"total"`
}

// ListIssues returns every issue in the workspace, following pagination.
//
// projectID narrows the walk to one project; "" lists the whole workspace. The
// page size is the server's maximum, because a smaller page only adds round
// trips to a walk that already reads everything.
func (c *MulticaClient) ListIssues(ctx context.Context, projectID string) ([]Issue, error) {
	const pageSize = 100
	var issues []Issue
	for offset := 0; ; {
		query := url.Values{}
		query.Set("limit", strconv.Itoa(pageSize))
		query.Set("offset", strconv.Itoa(offset))
		if projectID != "" {
			query.Set("project_id", projectID)
		}
		var page multicaIssuePage
		if err := c.do(ctx, http.MethodGet, "/api/issues?"+query.Encode(), nil, &page); err != nil {
			return nil, fmt.Errorf("list issues at offset %d: %w", offset, err)
		}
		for _, raw := range page.Issues {
			issues = append(issues, raw.toIssue())
		}
		// An empty page ends the walk whatever total claims. A short page also
		// ends it: the server never returns fewer rows than asked for while
		// more remain.
		if len(page.Issues) < pageSize {
			return issues, nil
		}
		offset += len(page.Issues)
	}
}

func (m multicaIssue) toIssue() Issue {
	issue := Issue{
		Side:      SideMultica,
		Key:       m.Identifier,
		Title:     m.Title,
		Status:    m.Status,
		Priority:  m.Priority,
		UpdatedAt: parseTime(m.UpdatedAt),
	}
	if m.Description != nil {
		issue.Body = *m.Description
	}
	if m.DueDate != nil {
		issue.DueDate = *m.DueDate
	}
	return issue
}

// UpdateIssue writes the fields a decision selected. Only the named fields are
// sent, so a field the run did not decide about keeps whatever the server has
// — including edits made between the read and the write.
func (c *MulticaClient) UpdateIssue(ctx context.Context, id string, fields []string, from Issue) error {
	body := map[string]any{}
	for _, field := range fields {
		switch field {
		case FieldTitle:
			body["title"] = from.Title
		case FieldDescription:
			body["description"] = from.Body
		case FieldStatus:
			body["status"] = from.Status
		case FieldPriority:
			body["priority"] = from.Priority
		case FieldDueDate:
			body["due_date"] = from.DueDate
		}
	}
	if len(body) == 0 {
		return nil
	}
	// suppress_run keeps a mirror write from starting an agent run: the sync
	// copies content, it does not ask anyone to act on it.
	body["suppress_run"] = true
	if err := c.do(ctx, http.MethodPut, "/api/issues/"+url.PathEscape(id), body, nil); err != nil {
		return fmt.Errorf("update issue %s: %w", id, err)
	}
	return nil
}

// CreateIssue creates the Multica counterpart of a tracker issue and returns
// the created issue, whose identifier the caller stores in the tracker marker.
func (c *MulticaClient) CreateIssue(ctx context.Context, from Issue, projectID string) (Issue, error) {
	body := map[string]any{
		"title":       from.Title,
		"description": from.Body,
		"status":      from.Status,
	}
	if from.Priority != "" {
		body["priority"] = from.Priority
	}
	if from.DueDate != "" {
		body["due_date"] = from.DueDate
	}
	if projectID != "" {
		body["project_id"] = projectID
	}
	var created multicaIssue
	if err := c.do(ctx, http.MethodPost, "/api/issues", body, &created); err != nil {
		return Issue{}, fmt.Errorf("create issue %q: %w", from.Title, err)
	}
	return created.toIssue(), nil
}

// do performs one request and decodes a JSON response into out when out is not
// nil. A non-2xx response is an error carrying a bounded slice of the body,
// which is where the server explains what it refused.
func (c *MulticaClient) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if c.WorkspaceID != "" {
		req.Header.Set("X-Workspace-ID", c.WorkspaceID)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%s %s: status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(detail)))
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%s %s: decode response: %w", method, path, err)
	}
	return nil
}

// parseTime reads the RFC3339 timestamps the API emits. An unparseable value
// becomes the zero time rather than an error: a timestamp is only a tie-break
// here, and refusing the whole run over one malformed field would be worse
// than treating that issue as the older side.
func parseTime(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return parsed
}
