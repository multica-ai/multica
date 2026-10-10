package vcs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// GongfengClient uses the public v3 API. It deliberately has no SDK dependency.
// MR detail endpoints take a global ID; UI links and webhook identities use IID.
type GongfengClient struct {
	base, token string
	client      *http.Client
}

func NewGongfengClient(base, token string) (*GongfengClient, error) {
	if _, err := gongfengWebURL(base); err != nil {
		return nil, err
	}
	c := *httpClient
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &GongfengClient{NormalizeInstanceURL(base) + "/api/v3", token, &c}, nil
}

// GongfengAPIError omits response bodies, which can contain secrets or HTML.
type GongfengAPIError struct {
	Status     int
	RetryAfter time.Duration
}

func (e *GongfengAPIError) Error() string {
	switch e.Status {
	case 401, 403:
		return "Gongfeng denied access. Check the token and repository permissions."
	case 404:
		return "Gongfeng repository or merge request is no longer accessible."
	case 429:
		return "Gongfeng rate limit reached. Sync will retry later."
	default:
		return fmt.Sprintf("Gongfeng API returned HTTP %d. Retry sync later.", e.Status)
	}
}

func (c *GongfengClient) request(ctx context.Context, method, path string, query url.Values, body, out any) (http.Header, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(b)
	}
	endpoint := c.base + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, errors.New("invalid Gongfeng API request")
	}
	req.Header.Set("PRIVATE-TOKEN", c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, errors.New("Could not reach Gongfeng. Check the server network and TLS certificate.")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := &GongfengAPIError{Status: resp.StatusCode}
		if resp.StatusCode == http.StatusTooManyRequests {
			apiErr.RetryAfter = time.Minute
			if seconds, parseErr := strconv.ParseInt(resp.Header.Get("Retry-After"), 10, 32); parseErr == nil && seconds > 0 {
				apiErr.RetryAfter = time.Duration(seconds) * time.Second
			} else if until, parseErr := http.ParseTime(resp.Header.Get("Retry-After")); parseErr == nil && time.Until(until) > 0 {
				apiErr.RetryAfter = time.Until(until)
			}
		}
		return nil, apiErr
	}
	if out != nil {
		b, err := io.ReadAll(io.LimitReader(resp.Body, (10<<20)+1))
		if err != nil || len(b) > 10<<20 {
			return nil, errors.New("Gongfeng response is too large or could not be read")
		}
		if err := json.Unmarshal(b, out); err != nil {
			return nil, errors.New("Gongfeng returned an invalid API response")
		}
	}
	return resp.Header, nil
}

const GongfengPageSize = 20

func gongfengPage(page int) url.Values {
	return url.Values{"page": {strconv.Itoa(page)}, "per_page": {strconv.Itoa(GongfengPageSize)}}
}
func gongfengNextPage(h http.Header, page, count int) (int, error) {
	if values, ok := h["X-Next-Page"]; ok && len(values) > 0 {
		if values[0] == "" || values[0] == "0" {
			return 0, nil
		}
		next, err := strconv.Atoi(values[0])
		if err != nil || next <= page || next > 10000 {
			return 0, errors.New("Gongfeng returned invalid pagination")
		}
		return next, nil
	}
	// Some v3 deployments omit pagination headers; a full page may have a successor.
	if count == GongfengPageSize {
		return page + 1, nil
	}
	return 0, nil
}

type GongfengProject struct {
	ID            int64  `json:"id,omitempty"`
	Path          string `json:"path_with_namespace"`
	WebURL        string `json:"web_url"`
	SSHURL        string `json:"ssh_url_to_repo"`
	HTTPURL       string `json:"http_url_to_repo"`
	HTTPSURL      string `json:"https_url_to_repo"`
	Description   string `json:"description"`
	DefaultBranch string `json:"default_branch"`
	Archived      bool   `json:"archived"`
}

func (p GongfengProject) CloneURL() string {
	if p.SSHURL != "" {
		return p.SSHURL
	}
	if p.HTTPSURL != "" {
		return p.HTTPSURL
	}
	return p.HTTPURL
}
func (p GongfengProject) validate() error {
	owner, name := splitNamespace(p.Path)
	if p.ID <= 0 || owner == "" || name == "" {
		return errors.New("Gongfeng returned an invalid repository identity")
	}
	if _, err := gongfengWebURL(p.WebURL); err != nil {
		return errors.New("Gongfeng returned an invalid repository URL")
	}
	return nil
}
func (c *GongfengClient) Projects(ctx context.Context, search string, page int) ([]GongfengProject, int, error) {
	q := gongfengPage(page)
	if search != "" {
		q.Set("search", search)
	}
	var projects []GongfengProject
	h, err := c.request(ctx, "GET", "/projects", q, nil, &projects)
	if err != nil {
		return nil, 0, err
	}
	if projects == nil {
		return nil, 0, errors.New("Gongfeng returned an invalid repository list")
	}
	for _, p := range projects {
		if err := p.validate(); err != nil {
			return nil, 0, err
		}
	}
	next, err := gongfengNextPage(h, page, len(projects))
	return projects, next, err
}
func (c *GongfengClient) Project(ctx context.Context, id string) (GongfengProject, error) {
	var p GongfengProject
	_, err := c.request(ctx, "GET", "/projects/"+url.PathEscape(id), nil, nil, &p)
	if err == nil {
		err = p.validate()
	}
	return p, err
}

type GongfengMR struct {
	ID              int64  `json:"id"`
	IID             int32  `json:"iid"`
	Title           string `json:"title"`
	Description     string `json:"description"`
	State           string `json:"state"`
	SourceBranch    string `json:"source_branch"`
	SourceProjectID int64  `json:"source_project_id"`
	SHA             string `json:"sha"`
	MergeStatus     string `json:"merge_status"`
	WorkInProgress  bool   `json:"work_in_progress"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
	MergedAt        string `json:"merged_at"`
	ClosedAt        string `json:"closed_at"`
	Author          struct {
		Username  string `json:"username"`
		AvatarURL string `json:"avatar_url"`
	} `json:"author"`
}

func (m GongfengMR) Event(project GongfengProject) (PullRequestEvent, error) {
	created, err := parseGongfengTime(m.CreatedAt)
	if err != nil {
		return PullRequestEvent{}, err
	}
	updated, err := parseGongfengTime(m.UpdatedAt)
	if err != nil {
		return PullRequestEvent{}, err
	}
	owner, name := splitNamespace(project.Path)
	if m.ID <= 0 || m.IID <= 0 || owner == "" || name == "" {
		return PullRequestEvent{}, errors.New("invalid Gongfeng MR identity")
	}
	state := m.State
	if state == "opened" || state == "reopened" {
		state = "open"
		title := strings.ToLower(m.Title)
		if m.WorkInProgress || strings.HasPrefix(title, "wip:") || strings.HasPrefix(title, "draft:") {
			state = "draft"
		}
	}
	if state != "open" && state != "draft" && state != "closed" && state != "merged" {
		return PullRequestEvent{}, errors.New("unknown Gongfeng MR state")
	}
	ev := PullRequestEvent{Action: "sync", RepoOwner: owner, RepoName: name, Number: m.IID, Title: m.Title, Body: m.Description, State: state, HTMLURL: strings.TrimRight(project.WebURL, "/") + "/merge_requests/" + strconv.Itoa(int(m.IID)), Branch: m.SourceBranch, HeadSHA: m.SHA, AuthorLogin: m.Author.Username, AuthorAvatarURL: m.Author.AvatarURL, CreatedAt: created, UpdatedAt: updated}
	if state == "merged" {
		ev.Action = "merge"
		ev.MergedAt = updated
		if m.MergedAt != "" {
			ev.MergedAt, err = parseGongfengTime(m.MergedAt)
		}
	}
	if state == "closed" {
		ev.Action = "close"
		ev.ClosedAt = updated
		if m.ClosedAt != "" {
			ev.ClosedAt, err = parseGongfengTime(m.ClosedAt)
		}
	}
	return ev, err
}
func (c *GongfengClient) MergeRequests(ctx context.Context, project string, page int) ([]GongfengMR, int, error) {
	var mrs []GongfengMR
	q := gongfengPage(page)
	// Omit state to include all MRs; official v3 treats "all" as a literal state.
	q.Set("order_by", "updated_at")
	q.Set("sort", "desc")
	h, err := c.request(ctx, "GET", "/projects/"+url.PathEscape(project)+"/merge_requests", q, nil, &mrs)
	if err != nil {
		return nil, 0, err
	}
	if mrs == nil {
		return nil, 0, errors.New("Gongfeng returned an invalid MR list")
	}
	next, err := gongfengNextPage(h, page, len(mrs))
	return mrs, next, err
}
func (c *GongfengClient) MergeRequest(ctx context.Context, project string, iid int32) (GongfengMR, error) {
	var mrs []GongfengMR
	q := gongfengPage(1)
	q.Set("iid", strconv.Itoa(int(iid)))
	_, err := c.request(ctx, "GET", "/projects/"+url.PathEscape(project)+"/merge_requests", q, nil, &mrs)
	if err != nil {
		return GongfengMR{}, err
	}
	for _, m := range mrs {
		if m.IID == iid && m.ID > 0 {
			var out GongfengMR
			_, err = c.request(ctx, "GET", fmt.Sprintf("/projects/%s/merge_request/%d", url.PathEscape(project), m.ID), nil, nil, &out)
			if err == nil && (out.IID != iid || out.ID != m.ID) {
				err = errors.New("Gongfeng returned mismatched MR identity")
			}
			return out, err
		}
	}
	return GongfengMR{}, &GongfengAPIError{Status: 404}
}
func (c *GongfengClient) HeadSHA(ctx context.Context, m GongfengMR) (string, error) {
	if m.SHA != "" {
		return m.SHA, nil
	}
	if m.SourceProjectID <= 0 || m.SourceBranch == "" {
		return "", errors.New("Gongfeng MR source commit is unavailable")
	}
	var branch struct {
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	_, err := c.request(ctx, "GET", fmt.Sprintf("/projects/%d/repository/branches/%s", m.SourceProjectID, url.PathEscape(m.SourceBranch)), nil, nil, &branch)
	if err == nil && branch.Commit.ID == "" {
		err = errors.New("Gongfeng branch has no commit")
	}
	return branch.Commit.ID, err
}

type GongfengChecks struct {
	Total       int64    `json:"total"`
	Passed      int64    `json:"passed"`
	Failed      int64    `json:"failed"`
	Pending     int64    `json:"pending"`
	Running     int64    `json:"running"`
	FailedNames []string `json:"failed_names"`
	Rollup      *string  `json:"rollup"`
	Mergeable   *string  `json:"mergeable"`
}

func (c *GongfengClient) Checks(ctx context.Context, project int64, sha string) (GongfengChecks, error) {
	out := GongfengChecks{FailedNames: []string{}}
	type status struct {
		ID        int64  `json:"id"`
		SHA       string `json:"sha"`
		State     string `json:"state"`
		Name      string `json:"name"`
		Context   string `json:"context"`
		UpdatedAt string `json:"updated_at"`
	}
	latest := map[string]status{}
	for page := 1; page > 0; {
		if page > 100 {
			return out, errors.New("Gongfeng check pagination exceeded the sync limit")
		}
		var statuses []status
		h, err := c.request(ctx, "GET", fmt.Sprintf("/projects/%d/commits/%s/statuses", project, url.PathEscape(sha)), gongfengPage(page), nil, &statuses)
		if err != nil {
			return out, err
		}
		if statuses == nil {
			return out, errors.New("Gongfeng returned an invalid check list")
		}
		for _, s := range statuses {
			if s.SHA != "" && s.SHA != sha {
				return out, errors.New("Gongfeng returned checks for a different commit")
			}
			key := s.Context
			if key == "" {
				key = s.Name
			}
			if key == "" {
				return out, errors.New("Gongfeng check has no name")
			}
			if prev, ok := latest[key]; !ok || s.ID > prev.ID {
				latest[key] = s
			}
		}
		page, err = gongfengNextPage(h, page, len(statuses))
		if err != nil {
			return out, err
		}
	}
	for name, s := range latest {
		out.Total++
		switch s.State {
		case "success":
			out.Passed++
		case "failure", "error":
			out.Failed++
			out.FailedNames = append(out.FailedNames, name)
		case "pending":
			out.Pending++
		default:
			return out, errors.New("Gongfeng returned an unknown check status")
		}
	}
	sort.Strings(out.FailedNames)
	if out.Total > 0 {
		rollup := "success"
		if out.Pending > 0 {
			rollup = "pending"
		}
		if out.Failed > 0 {
			rollup = "failure"
		}
		out.Rollup = &rollup
	}
	return out, nil
}

type GongfengHook struct {
	ID            int64  `json:"id,omitempty"`
	URL           string `json:"url"`
	Token         string `json:"token,omitempty"`
	MergeRequests bool   `json:"merge_requests_events"`
	Push          bool   `json:"push_events"`
	TagPush       bool   `json:"tag_push_events"`
	Issues        bool   `json:"issues_events"`
	Notes         bool   `json:"note_events"`
	Review        bool   `json:"review_events"`
	VerifySSL     bool   `json:"enable_ssl_verification"`
}

func (c *GongfengClient) EnsureHook(ctx context.Context, project int64, endpoint, secret string) (int64, error) {
	if _, err := gongfengWebURL(endpoint); err != nil {
		return 0, errors.New("Set the Multica public URL before enabling repository sync")
	}
	if secret == "" {
		return 0, errors.New("Gongfeng webhook secret is missing")
	}
	for page := 1; page > 0; {
		if page > 100 {
			return 0, errors.New("Gongfeng webhook pagination exceeded the sync limit")
		}
		var hooks []GongfengHook
		h, err := c.request(ctx, "GET", fmt.Sprintf("/projects/%d/hooks", project), gongfengPage(page), nil, &hooks)
		if err != nil {
			return 0, err
		}
		if hooks == nil {
			return 0, errors.New("Gongfeng returned an invalid webhook list")
		}
		for _, hook := range hooks {
			if hook.URL == endpoint && hook.ID > 0 {
				hook.Token = secret
				hook.MergeRequests = true
				hook.VerifySSL = true
				id := hook.ID
				hook.ID = 0
				var saved GongfengHook
				_, err = c.request(ctx, "PUT", fmt.Sprintf("/projects/%d/hooks/%d", project, id), nil, hook, &saved)
				return id, err
			}
		}
		page, err = gongfengNextPage(h, page, len(hooks))
		if err != nil {
			return 0, err
		}
	}
	var saved GongfengHook
	_, err := c.request(ctx, "POST", fmt.Sprintf("/projects/%d/hooks", project), nil, GongfengHook{URL: endpoint, Token: secret, MergeRequests: true, VerifySSL: true}, &saved)
	if err == nil && saved.ID <= 0 {
		err = errors.New("Gongfeng returned an invalid webhook identity")
	}
	return saved.ID, err
}
func (c *GongfengClient) RemoveHook(ctx context.Context, project, hookID int64, endpoint string) error {
	if hookID <= 0 {
		return nil
	}
	var hook GongfengHook
	_, err := c.request(ctx, "GET", fmt.Sprintf("/projects/%d/hooks/%d", project, hookID), nil, nil, &hook)
	var apiErr *GongfengAPIError
	if errors.As(err, &apiErr) && apiErr.Status == 404 {
		return nil
	}
	if err != nil {
		return err
	}
	if hook.ID != hookID || hook.URL != endpoint {
		return errors.New("The Gongfeng webhook changed. Remove it in Gongfeng before disconnecting.")
	}
	_, err = c.request(ctx, "DELETE", fmt.Sprintf("/projects/%d/hooks/%d", project, hookID), nil, nil, nil)
	return err
}
