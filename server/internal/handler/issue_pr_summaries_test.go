package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestListIssuesIncludesLinkedPullRequestsInOnePage(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized")
	}
	ctx := context.Background()
	issueID := createTestIssue(t, fmt.Sprintf("PR summary %d", time.Now().UnixNano()), "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })
	now := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	pr, err := testHandler.Queries.UpsertGitHubPullRequest(ctx, db.UpsertGitHubPullRequestParams{
		WorkspaceID: parseUUID(testWorkspaceID), InstallationID: 1,
		RepoOwner: "acme", RepoName: fmt.Sprintf("summary-%d", time.Now().UnixNano()),
		PrNumber: 42, Title: "summary", State: "draft", HtmlUrl: "https://github.com/acme/repo/pull/42",
		PrCreatedAt: now, PrUpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue_pull_request WHERE pull_request_id = $1`, pr.ID)
		testPool.Exec(context.Background(), `DELETE FROM github_pull_request WHERE id = $1`, pr.ID)
	})
	if _, err := testHandler.Queries.LinkIssueToPullRequest(ctx, db.LinkIssueToPullRequestParams{IssueID: parseUUID(issueID), PullRequestID: pr.ID}); err != nil {
		t.Fatal(err)
	}
	connectionID := dbfx.Insert(t, "vcs_connection", testutil.Cols{
		"workspace_id": testWorkspaceID, "provider": "gitlab",
		"instance_url":  fmt.Sprintf("https://gitlab-%d.example", time.Now().UnixNano()),
		"account_login": "acme", "access_token_encrypted": "test", "webhook_secret_encrypted": "test",
	})
	vcsPRID := dbfx.Insert(t, "vcs_pull_request", testutil.Cols{
		"workspace_id": testWorkspaceID, "connection_id": connectionID, "provider": "gitlab",
		"repo_owner": "acme", "repo_name": "repo", "pr_number": 43,
		"title": "vcs summary", "state": "open", "html_url": "https://gitlab.example/acme/repo/-/merge_requests/43",
		"head_sha": "new-head", "pr_created_at": time.Now(), "pr_updated_at": time.Now(),
	})
	dbfx.InsertNoID(t, "issue_vcs_pull_request", testutil.Cols{
		"issue_id": issueID, "pull_request_id": vcsPRID,
	}, "issue_id = $1 AND pull_request_id = $2", issueID, vcsPRID)
	dbfx.InsertNoID(t, "vcs_commit_status", testutil.Cols{
		"connection_id": connectionID, "sha": "new-head", "context": "build", "state": "failed",
	}, "connection_id = $1 AND sha = $2 AND context = $3", connectionID, "new-head", "build")
	dbfx.InsertNoID(t, "vcs_commit_status", testutil.Cols{
		"connection_id": connectionID, "sha": "old-head", "context": "build", "state": "passed",
	}, "connection_id = $1 AND sha = $2 AND context = $3", connectionID, "old-head", "build")

	w := httptest.NewRecorder()
	testHandler.ListIssues(w, newRequest(http.MethodGet, "/api/issues?workspace_id="+testWorkspaceID+"&limit=500", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list issues: %d %s", w.Code, w.Body.String())
	}
	var body struct {
		Issues []IssueResponse `json:"issues"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	for _, issue := range body.Issues {
		if issue.ID != issueID {
			continue
		}
		if issue.LinkedPullRequests == nil || len(*issue.LinkedPullRequests) != 2 {
			t.Fatalf("linked PRs = %+v, want two", issue.LinkedPullRequests)
		}
		byProvider := map[string]IssuePullRequestSummary{}
		for _, summary := range *issue.LinkedPullRequests {
			byProvider[summary.Provider] = summary
		}
		github := byProvider["github"]
		if github.State != "draft" || github.Number != 42 || github.HTMLURL != "https://github.com/acme/repo/pull/42" {
			t.Fatalf("unexpected GitHub PR: %+v", github)
		}
		gitlab := byProvider["gitlab"]
		if gitlab.State != "open" || gitlab.Number != 43 || gitlab.ChecksConclusion == nil || *gitlab.ChecksConclusion != "failed" {
			t.Fatalf("unexpected GitLab PR or CI: %+v", gitlab)
		}
		return
	}
	t.Fatal("created issue absent from list")
}
