package jira

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrUnauthorized is returned when the Jira site rejects the email/API-token
// pair (HTTP 401/403). Callers surface it as a connect-time validation
// failure distinct from transport errors. Mirrors vcs.ErrUnauthorized.
var ErrUnauthorized = errors.New("jira: credentials unauthorized")

// ErrIssueNotFound is returned by GetIssue when the site answers 404 —
// either the issue key does not exist or the token cannot see the project.
var ErrIssueNotFound = errors.New("jira: issue not found")

// ErrBadRequest is returned when the site answers 400 — for SearchIssues
// that almost always means the JQL failed to parse. Callers surface it as a
// user-fixable error distinct from transport failures.
var ErrBadRequest = errors.New("jira: bad request")

// Account is the minimal identity returned by ValidateCredentials
// (GET /rest/api/2/myself).
type Account struct {
	AccountID   string
	DisplayName string
	Email       string
}

// Issue is the minimal Jira issue shape PR 1 needs for enrichment: enough to
// create or sync the mirrored Multica issue when a webhook payload is thin.
type Issue struct {
	ID          string
	Key         string
	Summary     string
	Description string
	Status      string
	Priority    string
	// Assignee identity, populated by SearchIssues (webhook enrichment via
	// GetIssue does not need it — the webhook payload carries its own).
	AssigneeAccountID   string
	AssigneeDisplayName string
}

// searchPageSize is the per-request page size for SearchIssues; searchMaxCap
// bounds the total issues one search will return regardless of the caller's
// maxResults, keeping a single manual sync from mirroring an unbounded JQL.
const (
	searchPageSize = 50
	searchMaxCap   = 100
)

// Client is the seam the handlers depend on; tests substitute a mock so no
// real Jira site is ever contacted from the test suite.
type Client interface {
	// ValidateCredentials confirms the email + API token work against
	// baseURL and returns the authenticated account. Maps 401/403 to
	// ErrUnauthorized.
	ValidateCredentials(ctx context.Context, baseURL, email, token string) (Account, error)
	// GetIssue fetches an issue by key for enrichment. Maps 404 to
	// ErrIssueNotFound and 401/403 to ErrUnauthorized.
	GetIssue(ctx context.Context, baseURL, email, token, key string) (Issue, error)
	// SearchIssues runs a JQL query and returns up to maxResults issues
	// (capped at 100 regardless), paginating as needed. Maps 401/403 to
	// ErrUnauthorized. Powers the pull-based sync for users who cannot
	// register webhooks on the Jira site.
	SearchIssues(ctx context.Context, baseURL, email, token, jql string, maxResults int) ([]Issue, error)
}

// HTTPClient is the production Client. The zero value is not usable; call
// NewHTTPClient. HTTPDoer is injectable for tests that want to exercise this
// implementation without a network (handler tests normally mock Client
// itself instead).
type HTTPClient struct {
	do interface {
		Do(*http.Request) (*http.Response, error)
	}
}

// NewHTTPClient returns the production Jira REST client. A nil doer uses a
// 15s-timeout http.Client (mirrors the vcs package's shared client).
func NewHTTPClient(doer *http.Client) *HTTPClient {
	if doer == nil {
		doer = &http.Client{Timeout: 15 * time.Second}
	}
	return &HTTPClient{do: doer}
}

// ValidateCredentials calls GET /rest/api/2/myself with basic auth. The v2
// path is served by both Jira Cloud and Data Center.
func (c *HTTPClient) ValidateCredentials(ctx context.Context, baseURL, email, token string) (Account, error) {
	body, err := c.get(ctx, baseURL, email, token, "/rest/api/2/myself")
	if err != nil {
		return Account{}, err
	}
	var u struct {
		AccountID    string `json:"accountId"`
		DisplayName  string `json:"displayName"`
		EmailAddress string `json:"emailAddress"`
		Name         string `json:"name"` // Data Center has name, no accountId
	}
	if err := json.Unmarshal(body, &u); err != nil {
		return Account{}, fmt.Errorf("jira: decode myself: %w", err)
	}
	accountID := u.AccountID
	if accountID == "" {
		accountID = u.Name
	}
	if accountID == "" && u.DisplayName == "" {
		return Account{}, errors.New("jira: myself response missing identity")
	}
	return Account{AccountID: accountID, DisplayName: u.DisplayName, Email: u.EmailAddress}, nil
}

// GetIssue calls GET /rest/api/2/issue/{key}. The v2 API returns the
// description as a plain string on Data Center and Cloud alike (v3 returns
// ADF); DescriptionText handles both defensively.
func (c *HTTPClient) GetIssue(ctx context.Context, baseURL, email, token, key string) (Issue, error) {
	body, err := c.get(ctx, baseURL, email, token, "/rest/api/2/issue/"+url.PathEscape(key))
	if err != nil {
		return Issue{}, err
	}
	var d struct {
		ID     string `json:"id"`
		Key    string `json:"key"`
		Fields struct {
			Summary     string          `json:"summary"`
			Description json.RawMessage `json:"description"`
			Status      struct {
				Name string `json:"name"`
			} `json:"status"`
			Priority struct {
				Name string `json:"name"`
			} `json:"priority"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(body, &d); err != nil {
		return Issue{}, fmt.Errorf("jira: decode issue: %w", err)
	}
	return Issue{
		ID:          d.ID,
		Key:         d.Key,
		Summary:     d.Fields.Summary,
		Description: DescriptionText(d.Fields.Description),
		Status:      d.Fields.Status.Name,
		Priority:    d.Fields.Priority.Name,
	}, nil
}

// SearchIssues calls GET /rest/api/2/search?jql=... with basic auth,
// following startAt pagination until maxResults (capped at searchMaxCap)
// issues are collected or the site reports no more results. The v2 search
// endpoint is served by both Jira Cloud and Data Center and returns the
// description as a plain string (DescriptionText handles ADF defensively).
func (c *HTTPClient) SearchIssues(ctx context.Context, baseURL, email, token, jql string, maxResults int) ([]Issue, error) {
	if maxResults <= 0 || maxResults > searchMaxCap {
		maxResults = searchMaxCap
	}
	out := make([]Issue, 0, searchPageSize)
	for startAt := 0; len(out) < maxResults; {
		pageSize := searchPageSize
		if remaining := maxResults - len(out); remaining < pageSize {
			pageSize = remaining
		}
		q := url.Values{}
		q.Set("jql", jql)
		q.Set("startAt", fmt.Sprintf("%d", startAt))
		q.Set("maxResults", fmt.Sprintf("%d", pageSize))
		q.Set("fields", "summary,description,status,priority,assignee")
		body, err := c.get(ctx, baseURL, email, token, "/rest/api/2/search?"+q.Encode())
		if err != nil {
			return nil, err
		}
		var page struct {
			StartAt    int `json:"startAt"`
			MaxResults int `json:"maxResults"`
			Total      int `json:"total"`
			Issues     []struct {
				ID     string `json:"id"`
				Key    string `json:"key"`
				Fields struct {
					Summary     string          `json:"summary"`
					Description json.RawMessage `json:"description"`
					Status      struct {
						Name string `json:"name"`
					} `json:"status"`
					Priority struct {
						Name string `json:"name"`
					} `json:"priority"`
					Assignee struct {
						AccountID   string `json:"accountId"`
						Name        string `json:"name"` // Data Center
						DisplayName string `json:"displayName"`
					} `json:"assignee"`
				} `json:"fields"`
			} `json:"issues"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("jira: decode search: %w", err)
		}
		for _, d := range page.Issues {
			accountID := d.Fields.Assignee.AccountID
			if accountID == "" {
				accountID = d.Fields.Assignee.Name
			}
			out = append(out, Issue{
				ID:                  d.ID,
				Key:                 d.Key,
				Summary:             d.Fields.Summary,
				Description:         DescriptionText(d.Fields.Description),
				Status:              d.Fields.Status.Name,
				Priority:            d.Fields.Priority.Name,
				AssigneeAccountID:   accountID,
				AssigneeDisplayName: d.Fields.Assignee.DisplayName,
			})
			if len(out) >= maxResults {
				break
			}
		}
		startAt += len(page.Issues)
		// Stop when the page came back short or the site says we've seen all.
		if len(page.Issues) == 0 || startAt >= page.Total {
			break
		}
	}
	return out, nil
}

func (c *HTTPClient) get(ctx context.Context, baseURL, email, token, path string) ([]byte, error) {
	endpoint := NormalizeBaseURL(baseURL) + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("jira: build request: %w", err)
	}
	req.SetBasicAuth(email, token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.do.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jira: request: %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		// Log the upstream status + body snippet so a bad token (401) is
		// distinguishable from an insufficient-permission token (403)
		// without leaking the secret into the HTTP response.
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		slog.Warn("jira: request rejected",
			"endpoint", endpoint,
			"status", resp.StatusCode,
			"body", strings.TrimSpace(string(b)))
		return nil, ErrUnauthorized
	case resp.StatusCode == http.StatusNotFound:
		return nil, ErrIssueNotFound
	case resp.StatusCode == http.StatusBadRequest:
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("%w: %s", ErrBadRequest, strings.TrimSpace(string(b)))
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("jira: GET %s: status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return io.ReadAll(io.LimitReader(resp.Body, 10<<20))
}
