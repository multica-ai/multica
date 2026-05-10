package github

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseRepoURL(t *testing.T) {
	tests := []struct {
		in        string
		owner     string
		repo      string
		wantError bool
	}{
		{"https://github.com/multica-ai/multica", "multica-ai", "multica", false},
		{"https://github.com/multica-ai/multica.git", "multica-ai", "multica", false},
		{"https://github.com/multica-ai/multica/", "multica-ai", "multica", false},
		{"  https://github.com/owner/repo  ", "owner", "repo", false},
		{"http://github.com/owner/repo", "", "", true},
		{"https://gitlab.com/owner/repo", "", "", true},
		{"https://github.com/", "", "", true},
		{"https://github.com/owner", "", "", true},
		{"not a url", "", "", true},
	}
	for _, tt := range tests {
		owner, repo, err := ParseRepoURL(tt.in)
		if tt.wantError {
			if err == nil {
				t.Errorf("ParseRepoURL(%q): expected error, got %s/%s", tt.in, owner, repo)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseRepoURL(%q): unexpected error: %v", tt.in, err)
			continue
		}
		if owner != tt.owner || repo != tt.repo {
			t.Errorf("ParseRepoURL(%q): got %s/%s, want %s/%s", tt.in, owner, repo, tt.owner, tt.repo)
		}
	}
}

// TestListPullRequests_HappyPath verifies the Authorization header, the
// query string GitHub wants, and the JSON decode path all work end-to-end
// against a mocked GitHub server.
func TestListPullRequests_HappyPath(t *testing.T) {
	body := `[{
        "number": 42, "title": "Add Ship Hub", "state": "open", "draft": false,
        "html_url": "https://github.com/owner/repo/pull/42",
        "body": "summary",
        "user": {"login": "alice", "avatar_url": "https://example.com/a.png"},
        "base": {"ref": "main"},
        "head": {"ref": "feat/ship-hub", "sha": "abc123"},
        "labels": [{"name": "feat", "color": "00ff00"}],
        "additions": 100, "deletions": 50, "changed_files": 5,
        "created_at": "2026-04-30T00:00:00Z", "updated_at": "2026-05-01T00:00:00Z"
    }]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || !strings.HasPrefix(r.URL.Path, "/repos/owner/repo/pulls") {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("auth header: got %q", got)
		}
		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Errorf("accept header: got %q", got)
		}
		if got := r.URL.Query().Get("state"); got != "open" {
			t.Errorf("state query: got %q", got)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(body))
	}))
	defer srv.Close()

	c := NewClient("test-token")
	c.BaseURL = srv.URL
	prs, err := c.ListPullRequests(context.Background(), "owner", "repo", ListOptions{})
	if err != nil {
		t.Fatalf("ListPullRequests: %v", err)
	}
	if len(prs) != 1 {
		t.Fatalf("len(prs): got %d", len(prs))
	}
	pr := prs[0]
	if pr.Number != 42 || pr.Title != "Add Ship Hub" || pr.State != "open" {
		t.Errorf("unexpected PR: %+v", pr)
	}
	if pr.User.Login != "alice" || pr.Head.SHA != "abc123" || len(pr.Labels) != 1 {
		t.Errorf("unexpected nested fields: %+v", pr)
	}
	if pr.UpdatedAt.IsZero() || !pr.UpdatedAt.Equal(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("UpdatedAt: got %v", pr.UpdatedAt)
	}
}

// TestListPullRequests_ErrorMapping covers the four GitHub failure modes
// we care about. Each must map to a distinct typed error so the Ship Hub
// service can decide whether to retry, surface to the user, or back off.
func TestListPullRequests_ErrorMapping(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		headers map[string]string
		body    string
		wantErr error
	}{
		{"not found", http.StatusNotFound, nil, "", ErrNotFound},
		{"unauthorized", http.StatusUnauthorized, nil, "", ErrUnauthorized},
		{"primary rate limit", http.StatusForbidden, map[string]string{"X-RateLimit-Remaining": "0"}, "", ErrRateLimited},
		{"secondary rate limit", http.StatusForbidden, nil, `{"message":"You have exceeded a secondary rate limit"}`, ErrRateLimited},
		{"forbidden non-rate", http.StatusForbidden, nil, `{"message":"Resource not accessible by integration"}`, ErrForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for k, v := range tc.headers {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tc.status)
				if tc.body != "" {
					w.Write([]byte(tc.body))
				}
			}))
			defer srv.Close()

			c := NewClient("t")
			c.BaseURL = srv.URL
			_, err := c.ListPullRequests(context.Background(), "o", "r", ListOptions{})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestGetCombinedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/commits/abc/status") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Write([]byte(`{"state":"success"}`))
	}))
	defer srv.Close()

	c := NewClient("")
	c.BaseURL = srv.URL
	state, err := c.GetCombinedStatus(context.Background(), "o", "r", "abc")
	if err != nil {
		t.Fatalf("GetCombinedStatus: %v", err)
	}
	if state != "success" {
		t.Errorf("state: got %q", state)
	}
}

// TestListWorkflowRuns_HappyPath verifies the deploy poller's read
// path — branch/status/per_page query params, the workflow file name
// in the URL path, the wrapper-object decode (GitHub returns
// `{ "workflow_runs": [...] }` not a bare array).
func TestListWorkflowRuns_HappyPath(t *testing.T) {
	body := `{
        "total_count": 1,
        "workflow_runs": [{
            "id": 9876,
            "name": "Deploy production",
            "head_sha": "deadbeef",
            "head_branch": "main",
            "status": "completed",
            "conclusion": "success",
            "html_url": "https://github.com/o/r/actions/runs/9876",
            "created_at": "2026-05-09T10:00:00Z",
            "updated_at": "2026-05-09T10:05:00Z",
            "run_started_at": "2026-05-09T10:00:01Z"
        }]
    }`
	var seenPath, seenBranch, seenStatus, seenPerPage string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		seenBranch = r.URL.Query().Get("branch")
		seenStatus = r.URL.Query().Get("status")
		seenPerPage = r.URL.Query().Get("per_page")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(body))
	}))
	defer srv.Close()

	c := NewClient("test-token")
	c.BaseURL = srv.URL
	runs, err := c.ListWorkflowRuns(context.Background(), "o", "r", "production.yml", ListWorkflowRunsOptions{
		Branch:  "main",
		Status:  "completed",
		PerPage: 10,
	})
	if err != nil {
		t.Fatalf("ListWorkflowRuns: %v", err)
	}
	if seenPath != "/repos/o/r/actions/workflows/production.yml/runs" {
		t.Errorf("path: got %q", seenPath)
	}
	if seenBranch != "main" || seenStatus != "completed" || seenPerPage != "10" {
		t.Errorf("query: branch=%q status=%q per_page=%q", seenBranch, seenStatus, seenPerPage)
	}
	if len(runs) != 1 {
		t.Fatalf("len(runs): got %d", len(runs))
	}
	r := runs[0]
	if r.ID != 9876 || r.HeadSHA != "deadbeef" || r.Conclusion != "success" || r.HeadBranch != "main" {
		t.Errorf("unexpected run: %+v", r)
	}
}

// TestListWorkflowRuns_EmptyWorkflowName guards the error path that
// keeps the poller from accidentally hitting `/runs?...` (no workflow
// id) which would 404 against every GitHub repo and burn rate budget.
func TestListWorkflowRuns_EmptyWorkflowName(t *testing.T) {
	c := NewClient("t")
	if _, err := c.ListWorkflowRuns(context.Background(), "o", "r", "", ListWorkflowRunsOptions{}); err == nil {
		t.Fatal("expected error for empty workflow name")
	}
}

// TestListWorkflowRuns_DefaultPerPage ensures the per_page default is
// applied when the caller passes 0 (and clamped to 100 when > 100).
func TestListWorkflowRuns_DefaultPerPage(t *testing.T) {
	var seenPerPage string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPerPage = r.URL.Query().Get("per_page")
		w.Write([]byte(`{"workflow_runs":[]}`))
	}))
	defer srv.Close()

	c := NewClient("t")
	c.BaseURL = srv.URL
	if _, err := c.ListWorkflowRuns(context.Background(), "o", "r", "p.yml", ListWorkflowRunsOptions{}); err != nil {
		t.Fatalf("ListWorkflowRuns: %v", err)
	}
	if seenPerPage != "10" {
		t.Errorf("default per_page: got %q, want 10", seenPerPage)
	}

	if _, err := c.ListWorkflowRuns(context.Background(), "o", "r", "p.yml", ListWorkflowRunsOptions{PerPage: 500}); err != nil {
		t.Fatalf("ListWorkflowRuns clamp: %v", err)
	}
	if seenPerPage != "100" {
		t.Errorf("clamp per_page: got %q, want 100", seenPerPage)
	}
}

// TestUnauthClientNoAuthHeader verifies we don't accidentally send a bare
// "Bearer " (which some servers reject) when no token is set.
func TestUnauthClientNoAuthHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("expected no auth header, got %q", r.Header.Get("Authorization"))
		}
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := NewClient("")
	c.BaseURL = srv.URL
	if _, err := c.ListPullRequests(context.Background(), "o", "r", ListOptions{}); err != nil {
		t.Fatalf("ListPullRequests: %v", err)
	}
}
