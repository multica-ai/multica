package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestGitHubPRPollerBackfillSharesWebhookSemantics(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	const repoURL = "https://github.com/acme/widget"
	workspaceID := dbfx.Insert(t, "workspace", testutil.Cols{
		"name":         "GitHub PR polling backfill",
		"slug":         fmt.Sprintf("github-pr-polling-%d", time.Now().UnixNano()),
		"description":  "",
		"issue_prefix": "DEV",
		"repos":        `[{"url":"` + repoURL + `"}]`,
	})
	projectID := dbfx.Project(t, "GitHub PR polling backfill", testutil.Cols{"workspace_id": workspaceID})
	dbfx.Insert(t, "project_resource", testutil.Cols{
		"project_id": projectID, "workspace_id": workspaceID,
		"resource_type": "github_repo", "resource_ref": `{"url":"` + repoURL + `"}`,
		"position": 0,
	})
	installationID := time.Now().UnixNano()
	dbfx.Insert(t, "github_installation", testutil.Cols{
		"workspace_id": workspaceID, "installation_id": installationID,
		"account_login": "acme", "account_type": "Organization",
	})

	mergedIssueID := dbfx.Issue(t, "DEV-6 equivalent", testutil.Cols{
		"workspace_id": workspaceID, "number": 6, "status": "in_progress",
	})
	secondIssueID := dbfx.Issue(t, "DEV-9 equivalent", testutil.Cols{
		"workspace_id": workspaceID, "number": 9, "status": "in_progress",
	})
	referenceIssueID := dbfx.Issue(t, "Reference-only equivalent", testutil.Cols{
		"workspace_id": workspaceID, "number": 10, "status": "in_progress",
	})
	dbfx.Cleanup(t, `DELETE FROM activity_log WHERE issue_id = ANY($1::uuid[])`, []string{mergedIssueID, secondIssueID, referenceIssueID})
	dbfx.Cleanup(t, `DELETE FROM issue_pull_request WHERE issue_id = ANY($1::uuid[])`, []string{mergedIssueID, secondIssueID, referenceIssueID})
	dbfx.Cleanup(t, `DELETE FROM github_pull_request WHERE workspace_id = $1 AND repo_owner = 'acme' AND repo_name = 'widget'`, workspaceID)
	dbfx.Cleanup(t, `DELETE FROM github_pr_poll_cursor WHERE workspace_id = $1 AND repo_owner = 'acme' AND repo_name = 'widget'`, workspaceID)

	now := time.Now().UTC().Truncate(time.Second)
	prs := map[int32]ghPullRequest{
		19: pollingTestPR(19, "OPE-734: Receipt保存失敗と商品売上集計エラーを修正", "Closes DEV-6", "dev-6-receipt-summary-fix", "closed", now.Add(-time.Minute), true),
		20: pollingTestPR(20, "DEV-9: Receipt転送をdurable outboxで耐障害化する", "Closes DEV-9", "agent/dev-9-receipt-transfer-outbox", "closed", now.Add(-30*time.Second), true),
		21: pollingTestPR(21, "Documentation", "Related DEV-10", "docs", "open", now.Add(-90*time.Second), false),
	}
	var listCalls atomic.Int32
	h := *testHandler
	h.githubAPIGet = func(_ context.Context, gotInstallation int64, path string, out any) error {
		if gotInstallation != installationID {
			return fmt.Errorf("installation = %d", gotInstallation)
		}
		switch {
		case strings.HasPrefix(path, "/repos/acme/widget/pulls?"):
			listCalls.Add(1)
			return marshalPollingResponse(out, []map[string]any{
				{"number": 20, "updated_at": prs[20].UpdatedAt},
				{"number": 19, "updated_at": prs[19].UpdatedAt},
				{"number": 21, "updated_at": prs[21].UpdatedAt},
			})
		case strings.HasPrefix(path, "/repos/acme/widget/pulls/"):
			var number int32
			if _, err := fmt.Sscanf(path, "/repos/acme/widget/pulls/%d", &number); err != nil {
				return err
			}
			return marshalPollingResponse(out, prs[number])
		default:
			return fmt.Errorf("unexpected path %s", path)
		}
	}
	cfg := GitHubPRPollConfig{Interval: time.Minute, InitialLookback: 30 * 24 * time.Hour, Overlap: 5 * time.Minute}
	if err := h.pollGitHubPullRequestsOnce(ctx, cfg); err != nil {
		t.Fatalf("initial poll: %v", err)
	}
	if err := h.pollGitHubPullRequestsOnce(ctx, cfg); err != nil {
		t.Fatalf("overlap poll: %v", err)
	}
	if listCalls.Load() != 2 {
		t.Fatalf("list calls = %d, want duplicate-resource target polled once per round", listCalls.Load())
	}

	mergedRows, err := h.Queries.ListPullRequestsByIssue(ctx, parseUUID(mergedIssueID))
	if err != nil || len(mergedRows) != 1 || mergedRows[0].PrNumber != 19 {
		t.Fatalf("merged issue PRs = %+v, err=%v", mergedRows, err)
	}
	secondRows, err := h.Queries.ListPullRequestsByIssue(ctx, parseUUID(secondIssueID))
	if err != nil || len(secondRows) != 1 || secondRows[0].PrNumber != 20 {
		t.Fatalf("DEV-9 issue PRs = %+v, err=%v", secondRows, err)
	}
	referenceRows, err := h.Queries.ListPullRequestsByIssue(ctx, parseUUID(referenceIssueID))
	if err != nil || len(referenceRows) != 0 {
		t.Fatalf("reference-only issue PRs = %+v, err=%v", referenceRows, err)
	}
	var referenceOnly bool
	dbfx.QueryRow(t, `SELECT reference_only FROM issue_pull_request WHERE issue_id = $1`, referenceIssueID).Scan(&referenceOnly)
	if !referenceOnly {
		t.Fatal("bare body reference was not stored as reference_only")
	}
	mergedIssue, err := h.Queries.GetIssue(ctx, parseUUID(mergedIssueID))
	if err != nil || mergedIssue.Status != "done" {
		t.Fatalf("DEV-6 merged close-intent status = %q, err=%v", mergedIssue.Status, err)
	}
	secondIssue, err := h.Queries.GetIssue(ctx, parseUUID(secondIssueID))
	if err != nil || secondIssue.Status != "done" {
		t.Fatalf("DEV-9 merged close-intent status = %q, err=%v", secondIssue.Status, err)
	}
	if got := dbfx.Count(t, `SELECT count(*) FROM github_pull_request WHERE workspace_id = $1 AND repo_owner = 'acme' AND repo_name = 'widget'`, workspaceID); got != 3 {
		t.Fatalf("PR rows after repeated poll = %d, want 3", got)
	}
	if got := dbfx.Count(t, `SELECT count(*) FROM issue_pull_request WHERE issue_id = ANY($1::uuid[])`, []string{mergedIssueID, secondIssueID, referenceIssueID}); got != 3 {
		t.Fatalf("link rows after repeated poll = %d, want 3", got)
	}
}

func TestGitHubPRPollerPreservesCrossWorkspaceCloseIntentPolicy(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	installationID := time.Now().UnixNano()
	workspaceIDs := make([]string, 2)
	issueIDs := make([]string, 2)
	for i := range workspaceIDs {
		workspaceIDs[i] = dbfx.Insert(t, "workspace", testutil.Cols{
			"name":         fmt.Sprintf("Polling close policy %d", i),
			"slug":         fmt.Sprintf("polling-close-policy-%d-%d", i, installationID),
			"description":  "",
			"issue_prefix": "DEV",
		})
		dbfx.Insert(t, "github_installation", testutil.Cols{
			"workspace_id": workspaceIDs[i], "installation_id": installationID,
			"account_login": "acme", "account_type": "Organization",
		})
		issueIDs[i] = dbfx.Issue(t, "Ambiguous DEV-6", testutil.Cols{
			"workspace_id": workspaceIDs[i], "number": 6, "status": "in_progress",
		})
	}
	dbfx.Cleanup(t, `DELETE FROM activity_log WHERE issue_id = ANY($1::uuid[])`, issueIDs)
	dbfx.Cleanup(t, `DELETE FROM issue_pull_request WHERE issue_id = ANY($1::uuid[])`, issueIDs)
	dbfx.Cleanup(t, `DELETE FROM github_pull_request WHERE workspace_id = $1 AND repo_owner = 'acme' AND repo_name = 'widget'`, workspaceIDs[0])
	dbfx.Cleanup(t, `DELETE FROM github_pr_poll_cursor WHERE workspace_id = $1 AND repo_owner = 'acme' AND repo_name = 'widget'`, workspaceIDs[0])

	pr := pollingTestPR(19, "Receipt save fix", "Closes DEV-6", "dev-6-receipt-summary-fix", "closed", time.Now().UTC(), true)
	h := *testHandler
	h.githubAPIGet = func(_ context.Context, _ int64, path string, out any) error {
		if strings.HasPrefix(path, "/repos/acme/widget/pulls?") {
			return marshalPollingResponse(out, []map[string]any{{"number": 19, "updated_at": pr.UpdatedAt}})
		}
		if path == "/repos/acme/widget/pulls/19" {
			return marshalPollingResponse(out, pr)
		}
		return fmt.Errorf("unexpected path %s", path)
	}
	target := githubPRPollTarget{
		workspaceID: parseUUID(workspaceIDs[0]), owner: "acme", repo: "widget",
		installations: []int64{installationID},
	}
	if err := h.pollGitHubRepository(ctx, GitHubPRPollConfig{
		Interval: time.Minute, InitialLookback: 30 * 24 * time.Hour, Overlap: 5 * time.Minute,
	}, target); err != nil {
		t.Fatal(err)
	}
	firstIssue, err := h.Queries.GetIssue(ctx, parseUUID(issueIDs[0]))
	if err != nil || firstIssue.Status != "in_progress" {
		t.Fatalf("ambiguous close-intent issue status = %q, err=%v", firstIssue.Status, err)
	}
	var closeIntent bool
	dbfx.QueryRow(t, `SELECT close_intent FROM issue_pull_request WHERE issue_id = $1`, issueIDs[0]).Scan(&closeIntent)
	if closeIntent {
		t.Fatal("poller granted close intent ambiguous across installation bindings")
	}
	if got := dbfx.Count(t, `SELECT count(*) FROM issue_pull_request WHERE issue_id = $1`, issueIDs[1]); got != 0 {
		t.Fatalf("poller mirrored PR into unselected workspace: links=%d", got)
	}
}

func TestGitHubPRPollerFailurePreservesCursorThenRetriesOverlap(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	installationID := time.Now().UnixNano()
	dbfx.Insert(t, "github_installation", testutil.Cols{
		"workspace_id": testWorkspaceID, "installation_id": installationID,
		"account_login": "acme", "account_type": "Organization",
	})
	dbfx.Cleanup(t, `DELETE FROM github_pull_request WHERE workspace_id = $1 AND repo_owner = 'acme' AND repo_name = 'retry'`, testWorkspaceID)
	dbfx.Cleanup(t, `DELETE FROM github_pr_poll_cursor WHERE workspace_id = $1 AND repo_owner = 'acme' AND repo_name = 'retry'`, testWorkspaceID)

	oldCursor := time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Second)
	if err := testHandler.Queries.UpsertGitHubPRPollCursor(ctx, db.UpsertGitHubPRPollCursorParams{
		WorkspaceID: parseUUID(testWorkspaceID), RepoOwner: "acme", RepoName: "retry",
		CursorUpdatedAt: pgtype.Timestamptz{Time: oldCursor, Valid: true},
	}); err != nil {
		t.Fatal(err)
	}
	insideOverlap := pollingTestPR(30, "Incremental PR", "", "incremental", "open", oldCursor.Add(-4*time.Minute), false)
	outsideOverlap := pollingTestPR(31, "Old PR", "", "old", "open", oldCursor.Add(-6*time.Minute), false)
	insideOverlap.HTMLURL = "https://github.com/acme/retry/pull/30"
	outsideOverlap.HTMLURL = "https://github.com/acme/retry/pull/31"
	var fail atomic.Bool
	fail.Store(true)
	var detailCalls atomic.Int32
	h := *testHandler
	h.githubAPIGet = func(_ context.Context, _ int64, path string, out any) error {
		switch {
		case strings.HasPrefix(path, "/repos/acme/retry/pulls?"):
			if fail.Load() {
				return errors.New("temporary GitHub API failure")
			}
			return marshalPollingResponse(out, []map[string]any{
				{"number": 30, "updated_at": insideOverlap.UpdatedAt},
				{"number": 31, "updated_at": outsideOverlap.UpdatedAt},
			})
		case path == "/repos/acme/retry/pulls/30":
			detailCalls.Add(1)
			return marshalPollingResponse(out, insideOverlap)
		case path == "/repos/acme/retry/pulls/31":
			return errors.New("poller fetched beyond overlap cutoff")
		default:
			return fmt.Errorf("unexpected path %s", path)
		}
	}
	target := githubPRPollTarget{
		workspaceID: parseUUID(testWorkspaceID), owner: "acme", repo: "retry",
		installations: []int64{installationID},
	}
	cfg := GitHubPRPollConfig{Interval: time.Minute, InitialLookback: 30 * 24 * time.Hour, Overlap: 5 * time.Minute}
	if err := h.pollGitHubRepository(ctx, cfg, target); err == nil {
		t.Fatal("API failure unexpectedly succeeded")
	}
	cursor, err := h.Queries.GetGitHubPRPollCursor(ctx, db.GetGitHubPRPollCursorParams{
		WorkspaceID: target.workspaceID, RepoOwner: "acme", RepoName: "retry",
	})
	if err != nil || !cursor.Time.Equal(oldCursor) {
		t.Fatalf("cursor after failure = %s, err=%v; want %s", cursor.Time, err, oldCursor)
	}

	fail.Store(false)
	if err := h.pollGitHubRepository(ctx, cfg, target); err != nil {
		t.Fatalf("retry poll: %v", err)
	}
	if detailCalls.Load() != 1 {
		t.Fatalf("detail calls = %d, want only the PR inside overlap", detailCalls.Load())
	}
	cursor, err = h.Queries.GetGitHubPRPollCursor(ctx, db.GetGitHubPRPollCursorParams{
		WorkspaceID: target.workspaceID, RepoOwner: "acme", RepoName: "retry",
	})
	if err != nil || !cursor.Time.After(oldCursor) {
		t.Fatalf("cursor after successful retry = %s, err=%v; want after %s", cursor.Time, err, oldCursor)
	}
	if got := dbfx.Count(t, `SELECT count(*) FROM github_pull_request WHERE workspace_id = $1 AND repo_owner = 'acme' AND repo_name = 'retry'`, testWorkspaceID); got != 1 {
		t.Fatalf("incremental PR rows = %d, want 1", got)
	}
}

func TestParseGitHubRepositoryURL(t *testing.T) {
	for _, raw := range []string{
		"https://github.com/multica-ai/multica",
		"ssh://git@github.com/multica-ai/multica.git",
		"git@github.com:multica-ai/multica.git",
	} {
		owner, repo, ok := parseGitHubRepositoryURL(raw)
		if !ok || owner != "multica-ai" || repo != "multica" {
			t.Fatalf("parse %q = %q/%q, %v", raw, owner, repo, ok)
		}
	}
	if _, _, ok := parseGitHubRepositoryURL("https://gitlab.com/multica-ai/multica"); ok {
		t.Fatal("non-GitHub repository was accepted")
	}
}

func pollingTestPR(number int32, title, body, branch, state string, updatedAt time.Time, merged bool) ghPullRequest {
	pr := ghPullRequest{
		Number: number, HTMLURL: fmt.Sprintf("https://github.com/acme/widget/pull/%d", number),
		Title: title, Body: body, State: state, Merged: merged,
		CreatedAt: updatedAt.Add(-time.Hour).Format(time.RFC3339), UpdatedAt: updatedAt.Format(time.RFC3339),
		Additions: 3, Deletions: 1, ChangedFiles: 1,
	}
	pr.Head.Ref = branch
	pr.Head.SHA = fmt.Sprintf("head-%d", number)
	pr.User.Login = "octocat"
	if merged {
		pr.MergedAt = updatedAt.Format(time.RFC3339)
		pr.ClosedAt = updatedAt.Format(time.RFC3339)
	}
	return pr
}

func marshalPollingResponse(out, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}
