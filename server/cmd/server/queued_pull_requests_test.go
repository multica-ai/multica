package main

import (
	"context"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Seeds one issue linked to one github_pull_request and returns the issue id.
// Every column the queued-PR query filters on is a parameter so each case can
// vary exactly one of them.
func seedLinkedPR(
	t *testing.T,
	prNumber int,
	state string,
	queueState any,
	headSHA string,
	snapshotHeadSHA string,
	snapshotFetched bool,
	referenceOnly bool,
) string {
	t.Helper()
	ctx := context.Background()

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, position, number)
		VALUES ($1, 'queued pr test issue', 'in_review', 'none', 'member', $2, 0,
		        (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("seed issue: %v", err)
	}

	var prID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO github_pull_request (
			workspace_id, installation_id, repo_owner, repo_name, pr_number,
			title, state, html_url, pr_created_at, pr_updated_at,
			head_sha, snapshot_head_sha, snapshot_fetched_at, api_merge_queue_state
		) VALUES ($1, 1, 'acme', 'widget', $2, 'pr', $3, 'https://example.test/pr',
		          now(), now(), $4, $5, CASE WHEN $6::bool THEN now() ELSE NULL END, $7)
		RETURNING id
	`, testWorkspaceID, prNumber, state, headSHA, snapshotHeadSHA, snapshotFetched, queueState).Scan(&prID); err != nil {
		t.Fatalf("seed pull request: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
		INSERT INTO issue_pull_request (issue_id, pull_request_id, reference_only)
		VALUES ($1, $2, $3)
	`, issueID, prID, referenceOnly); err != nil {
		t.Fatalf("link pull request: %v", err)
	}

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM github_pull_request WHERE id = $1`, prID)
	})
	return issueID
}

func queuedIssueIDs(t *testing.T) map[[16]byte]string {
	t.Helper()
	rows, err := db.New(testPool).ListQueuedPullRequestIssuesByWorkspace(
		context.Background(), parseUUID(testWorkspaceID),
	)
	if err != nil {
		t.Fatalf("ListQueuedPullRequestIssuesByWorkspace: %v", err)
	}
	out := make(map[[16]byte]string, len(rows))
	for _, row := range rows {
		out[row.IssueID.Bytes] = row.ApiMergeQueueState.String
	}
	return out
}

// TestQueuedPullRequestQueryFiltering runs the real query against Postgres.
// Each case differs from the included one by a single column, so a filter that
// silently stops applying shows up as exactly one failure.
func TestQueuedPullRequestQueryFiltering(t *testing.T) {
	queued := seedLinkedPR(t, 9001, "open", "QUEUED", "sha-a", "sha-a", true, false)
	draft := seedLinkedPR(t, 9002, "draft", "AWAITING_CHECKS", "sha-b", "sha-b", true, false)
	notQueued := seedLinkedPR(t, 9003, "open", nil, "sha-c", "sha-c", true, false)
	// The snapshot describes an older head, so its queue state is stale.
	staleHead := seedLinkedPR(t, 9004, "open", "QUEUED", "sha-new", "sha-old", true, false)
	// Never fetched: snapshot columns carry no trustworthy value.
	neverFetched := seedLinkedPR(t, 9005, "open", "QUEUED", "sha-e", "sha-e", false, false)
	// A merged PR keeps whatever it last reported while it was open.
	merged := seedLinkedPR(t, 9006, "merged", "QUEUED", "sha-f", "sha-f", true, false)
	// A PR that merely mentions the issue is not the issue's working PR.
	reference := seedLinkedPR(t, 9007, "open", "QUEUED", "sha-g", "sha-g", true, true)

	got := queuedIssueIDs(t)
	key := func(id string) [16]byte { return parseUUID(id).Bytes }

	if state, ok := got[key(queued)]; !ok || state != "QUEUED" {
		t.Fatalf("queued PR missing or wrong state: ok=%v state=%q", ok, state)
	}
	if state, ok := got[key(draft)]; !ok || state != "AWAITING_CHECKS" {
		t.Fatalf("queued draft PR missing or wrong state: ok=%v state=%q", ok, state)
	}
	for name, id := range map[string]string{
		"not queued":     notQueued,
		"stale head":     staleHead,
		"never fetched":  neverFetched,
		"merged":         merged,
		"reference only": reference,
	} {
		if _, ok := got[key(id)]; ok {
			t.Fatalf("%s PR should not be reported as queued", name)
		}
	}
}

// TestQueuedPullRequestsEndpoint covers what the query test cannot: that the
// route is registered under the workspace group and survives auth. The payload
// is empty unless a GitHub App key is configured, so this asserts the contract
// shape rather than the rows.
func TestQueuedPullRequestsEndpoint(t *testing.T) {
	resp := authRequest(t, "GET", "/api/workspaces/"+testWorkspaceID+"/github/pull-requests/queued", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body struct {
		QueuedPullRequests []map[string]any `json:"queued_pull_requests"`
	}
	readJSON(t, resp, &body)
	if body.QueuedPullRequests == nil {
		t.Fatal("queued_pull_requests must be [], never null")
	}
}
