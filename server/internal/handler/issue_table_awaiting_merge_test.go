package handler

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/integrations/ghsnapshot"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// awaitingMergeFixture owns a workspace of its own: the awaiting-merge scope
// is workspace-wide, so PR links other tests leave in the shared workspace
// would otherwise leak into every assertion.
type awaitingMergeFixture struct {
	t            *testing.T
	fx           *testutil.Fixture
	workspaceID  string
	prNumber     int
	checkOrdinal int
}

func newAwaitingMergeFixture(t *testing.T) *awaitingMergeFixture {
	t.Helper()
	suffix := time.Now().UnixNano()
	workspaceID := dbfx.Workspace(t,
		fmt.Sprintf("awaiting merge %d", suffix),
		fmt.Sprintf("awaiting-merge-%d", suffix))
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_status WHERE workspace_id = $1`, workspaceID)
	})
	return &awaitingMergeFixture{
		t:           t,
		fx:          testutil.New(testPool, workspaceID, testUserID),
		workspaceID: workspaceID,
	}
}

func (f *awaitingMergeFixture) issue(title, status string) string {
	f.t.Helper()
	return f.fx.Issue(f.t, title, testutil.Cols{"status": status})
}

// githubPR inserts a GitHub PR on head "head". Without snapshot columns it
// has no current snapshot.
func (f *awaitingMergeFixture) githubPR(state string, over testutil.Cols) string {
	f.t.Helper()
	f.prNumber++
	cols := testutil.Cols{
		"workspace_id":    f.workspaceID,
		"installation_id": 1,
		"repo_owner":      "multica-ai",
		"repo_name":       "multica",
		"pr_number":       f.prNumber,
		"title":           fmt.Sprintf("PR %d", f.prNumber),
		"state":           state,
		"html_url":        fmt.Sprintf("https://github.com/multica-ai/multica/pull/%d", f.prNumber),
		"head_sha":        "head",
		"pr_created_at":   testutil.Raw("now()"),
		"pr_updated_at":   testutil.Raw("now()"),
	}
	for key, value := range over {
		cols[key] = value
	}
	return f.fx.Insert(f.t, "github_pull_request", cols)
}

// prSnapshot is a GitHub API snapshot taken for the PR's current head.
func prSnapshot(rollup, mergeable, mergeState any) testutil.Cols {
	return testutil.Cols{
		"snapshot_head_sha":      "head",
		"snapshot_fetched_at":    testutil.Raw("now()"),
		"checks_rollup_state":    rollup,
		"api_mergeable":          mergeable,
		"api_merge_state_status": mergeState,
	}
}

func (f *awaitingMergeFixture) checkRun(prID, headSHA, status string, conclusion any) {
	f.t.Helper()
	f.checkOrdinal++
	f.fx.InsertNoID(f.t, "github_pull_request_check_run", testutil.Cols{
		"pr_id":      prID,
		"head_sha":   headSHA,
		"ordinal":    f.checkOrdinal,
		"name":       fmt.Sprintf("check %d", f.checkOrdinal),
		"status":     status,
		"conclusion": conclusion,
	}, "pr_id = $1", prID)
}

func (f *awaitingMergeFixture) link(issueID, prID string) {
	f.t.Helper()
	f.fx.InsertNoID(f.t, "issue_pull_request", testutil.Cols{
		"issue_id":        issueID,
		"pull_request_id": prID,
	}, "issue_id = $1 AND pull_request_id = $2", issueID, prID)
}

func (f *awaitingMergeFixture) exclude(issueID, prID string) {
	f.t.Helper()
	f.fx.InsertNoID(f.t, "issue_pull_request_exclusion", testutil.Cols{
		"issue_id":        issueID,
		"pull_request_id": prID,
		"workspace_id":    f.workspaceID,
	}, "issue_id = $1 AND pull_request_id = $2", issueID, prID)
}

// vcsPR inserts a Forgejo PR on head "head" of connectionID.
func (f *awaitingMergeFixture) vcsPR(connectionID, state string, over testutil.Cols) string {
	f.t.Helper()
	f.prNumber++
	cols := testutil.Cols{
		"workspace_id":  f.workspaceID,
		"connection_id": connectionID,
		"repo_owner":    "multica-ai",
		"repo_name":     "multica",
		"pr_number":     f.prNumber,
		"title":         fmt.Sprintf("VCS PR %d", f.prNumber),
		"state":         state,
		"html_url":      fmt.Sprintf("https://forgejo.example/multica-ai/multica/pulls/%d", f.prNumber),
		"head_sha":      "head",
		"pr_created_at": testutil.Raw("now()"),
		"pr_updated_at": testutil.Raw("now()"),
	}
	for key, value := range over {
		cols[key] = value
	}
	return f.fx.Insert(f.t, "vcs_pull_request", cols)
}

func (f *awaitingMergeFixture) linkVCS(issueID, prID string) {
	f.t.Helper()
	f.fx.InsertNoID(f.t, "issue_vcs_pull_request", testutil.Cols{
		"issue_id":        issueID,
		"pull_request_id": prID,
	}, "issue_id = $1 AND pull_request_id = $2", issueID, prID)
}

func (f *awaitingMergeFixture) commitStatus(connectionID, sha, state string) {
	f.t.Helper()
	statusContext := fmt.Sprintf("ci/%d", time.Now().UnixNano())
	f.fx.InsertNoID(f.t, "vcs_commit_status", testutil.Cols{
		"connection_id": connectionID,
		"sha":           sha,
		"context":       statusContext,
		"state":         state,
	}, "connection_id = $1 AND context = $2", connectionID, statusContext)
}

func (f *awaitingMergeFixture) newUUID() string {
	f.t.Helper()
	var id string
	f.fx.QueryRow(f.t, "SELECT gen_random_uuid()::text").Scan(&id)
	return id
}

func (f *awaitingMergeFixture) request(path string, body any) *http.Request {
	req := newRequest(http.MethodPost, path, body)
	req.Header.Set("X-Workspace-ID", f.workspaceID)
	return req
}

func awaitingMergeQuery() issueTableQuerySpec {
	return issueTableQuerySpec{
		Scope: issueTableScope{Kind: "awaiting_merge"},
		Sort:  issueTableSortRequest{Field: "pull_request_updated_at", Direction: "desc"},
	}
}

// rows returns the ids of an ungrouped awaiting-merge page, in order.
func (f *awaitingMergeFixture) rows(h *Handler, query issueTableQuerySpec) []string {
	f.t.Helper()
	var response issueTableRowsResponse
	testutil.Call(f.t, h.ListIssueTableRows, f.request("/api/issues/table/rows", issueTableRowsRequest{
		Query: query,
		Group: issueTableGroupSpec{Kind: "none"},
		Page:  issueTablePageRequest{Limit: 100},
	})).Want(http.StatusOK).JSON(&response)
	ids := make([]string, 0, len(response.Rows))
	for _, row := range response.Rows {
		ids = append(ids, row.Issue.ID)
	}
	if int(response.Total) != len(ids) {
		f.t.Fatalf("total = %d, rows = %d", response.Total, len(ids))
	}
	return ids
}

// groups reads the PR-state groups in order, then every group's rows, and
// returns each issue's group.
func (f *awaitingMergeFixture) groups(h *Handler) (order []string, stateOf map[string]string) {
	f.t.Helper()
	query := awaitingMergeQuery()
	group := issueTableGroupSpec{Kind: "pull_request_state"}
	var response issueTableGroupsResponse
	testutil.Call(f.t, h.ListIssueTableGroups, f.request("/api/issues/table/groups", issueTableGroupsRequest{
		Query: query,
		Group: group,
		Page:  issueTablePageRequest{Limit: 100},
	})).Want(http.StatusOK).JSON(&response)

	stateOf = map[string]string{}
	for _, descriptor := range response.Groups {
		if descriptor.Value.Kind != "pull_request_state" {
			f.t.Fatalf("group %s value kind = %q", descriptor.Key, descriptor.Value.Kind)
		}
		state, _ := descriptor.Value.Value.(string)
		if descriptor.Key != pullRequestStateGroupKeyPrefix+state {
			f.t.Fatalf("group key %q does not carry its state %q", descriptor.Key, state)
		}
		order = append(order, state)
		key := descriptor.Key
		var rows issueTableRowsResponse
		testutil.Call(f.t, h.ListIssueTableRows, f.request("/api/issues/table/rows", issueTableRowsRequest{
			Query:    query,
			Group:    group,
			GroupKey: &key,
			Page:     issueTablePageRequest{Limit: 100},
		})).Want(http.StatusOK).JSON(&rows)
		if int64(len(rows.Rows)) != descriptor.Count {
			f.t.Fatalf("group %s count = %d, rows = %d", state, descriptor.Count, len(rows.Rows))
		}
		for _, row := range rows.Rows {
			stateOf[row.Issue.ID] = state
		}
	}
	return order, stateOf
}

// snapshotHandler is testHandler with the GitHub API snapshot pipeline
// enabled, so stored snapshots count. Nothing here starts the manager, so no
// request leaves the process.
func snapshotHandler(t *testing.T) *Handler {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	t.Setenv("GITHUB_APP_ID", "1")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", string(pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})))
	client, err := ghsnapshot.NewClientFromEnv()
	if err != nil || !client.Enabled() {
		t.Fatalf("snapshot client: %v", err)
	}
	h := *testHandler
	h.PRRefresh = ghsnapshot.NewManager(client, testHandler.Queries, testPool, nil)
	return &h
}

func TestIssueTableAwaitingMergeScope(t *testing.T) {
	f := newAwaitingMergeFixture(t)
	f.fx.Exec(t, `
		INSERT INTO issue_status (workspace_id, key, name, description, category, color, position)
		VALUES ($1, 'shipped', 'Shipped', '', 'done', '#00ff00', 1)
	`, f.workspaceID)
	connectionID := f.newUUID()

	withOpenPR := func(title, status string) string {
		issueID := f.issue(title, status)
		f.link(issueID, f.githubPR("open", nil))
		return issueID
	}
	todo := withOpenPR("todo with open PR", "todo")
	inReview := withOpenPR("in review with open PR", "in_review")
	withOpenPR("done with open PR", "done")
	withOpenPR("cancelled with open PR", "cancelled")
	withOpenPR("custom done-category status with open PR", "shipped")

	for _, state := range []string{"draft", "merged", "closed"} {
		issueID := f.issue(state+" PR only", "in_progress")
		f.link(issueID, f.githubPR(state, nil))
	}

	removed := f.issue("removed link only", "in_progress")
	removedPR := f.githubPR("open", nil)
	f.link(removed, removedPR)
	f.exclude(removed, removedPR)

	removedAndKept := f.issue("removed link next to an open one", "in_progress")
	excludedPR := f.githubPR("open", nil)
	f.link(removedAndKept, excludedPR)
	f.exclude(removedAndKept, excludedPR)
	f.link(removedAndKept, f.githubPR("open", nil))

	openAndMerged := f.issue("open PR next to a merged one", "in_progress")
	f.link(openAndMerged, f.githubPR("merged", nil))
	f.link(openAndMerged, f.githubPR("open", nil))

	f.issue("no PR", "in_progress")

	vcsOpen := f.issue("open Forgejo PR", "todo")
	f.linkVCS(vcsOpen, f.vcsPR(connectionID, "open", nil))
	vcsDraft := f.issue("draft Forgejo PR", "todo")
	f.linkVCS(vcsDraft, f.vcsPR(connectionID, "draft", nil))
	vcsRemoved := f.issue("removed Forgejo link", "todo")
	vcsRemovedPR := f.vcsPR(connectionID, "open", nil)
	f.linkVCS(vcsRemoved, vcsRemovedPR)
	f.exclude(vcsRemoved, vcsRemovedPR)

	got := f.rows(testHandler, awaitingMergeQuery())
	sort.Strings(got)
	want := []string{todo, inReview, removedAndKept, openAndMerged, vcsOpen}
	sort.Strings(want)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("awaiting merge rows = %v, want %v", got, want)
	}

	// The fixed condition stays under the user's own filters.
	query := awaitingMergeQuery()
	query.Filters.Statuses = []string{"in_review", "done"}
	if got := f.rows(testHandler, query); fmt.Sprint(got) != fmt.Sprint([]string{inReview}) {
		t.Fatalf("status-filtered rows = %v, want [%s]", got, inReview)
	}
}

func TestIssueTableAwaitingMergeStaysInsideTheWorkspace(t *testing.T) {
	f := newAwaitingMergeFixture(t)
	other := newAwaitingMergeFixture(t)

	own := f.issue("own issue", "todo")
	f.link(own, f.githubPR("open", nil))

	// Another workspace's queue is not this one's.
	foreign := other.issue("foreign issue", "todo")
	other.link(foreign, other.githubPR("open", nil))

	// No foreign keys: a stale link can point at another tenant's PR. Its
	// state must not put this workspace's issue in the queue.
	strayGitHub := f.issue("linked to a foreign GitHub PR", "todo")
	f.link(strayGitHub, other.githubPR("open", nil))
	strayVCS := f.issue("linked to a foreign Forgejo PR", "todo")
	f.linkVCS(strayVCS, other.vcsPR(other.newUUID(), "open", nil))

	if got := f.rows(testHandler, awaitingMergeQuery()); fmt.Sprint(got) != fmt.Sprint([]string{own}) {
		t.Fatalf("rows = %v, want only [%s]", got, own)
	}
	if got := other.rows(testHandler, awaitingMergeQuery()); fmt.Sprint(got) != fmt.Sprint([]string{foreign}) {
		t.Fatalf("other workspace rows = %v, want only [%s]", got, foreign)
	}
	_, stateOf := f.groups(snapshotHandler(t))
	if len(stateOf) != 1 || stateOf[own] == "" {
		t.Fatalf("grouped issues = %v, want only %s", stateOf, own)
	}
}

// One open PR per issue, one row per derivePullRequestVerdict case
// (packages/core/github/pull-request-status.test.ts), so the group an issue
// lands in is the group its pill names.
func TestIssueTablePullRequestStateMirrorsVerdict(t *testing.T) {
	type checkRun struct {
		headSHA, status string
		conclusion      any
	}
	cases := []struct {
		name    string
		verdict string
		pr      testutil.Cols
		checks  []checkRun
		want    string
	}{
		{name: "no snapshot yet", verdict: "unknown", pr: nil, want: pullRequestStateChecking},
		{
			name: "snapshot of an older head", verdict: "unknown",
			pr:   withCols(prSnapshot("FAILURE", "CONFLICTING", "DIRTY"), testutil.Cols{"head_sha": "newer"}),
			want: pullRequestStateChecking,
		},
		{name: "rollup failure", verdict: "failed", pr: prSnapshot("FAILURE", "MERGEABLE", "UNSTABLE"), want: pullRequestStateNeedsAttention},
		{name: "rollup error", verdict: "failed", pr: prSnapshot("ERROR", nil, nil), want: pullRequestStateNeedsAttention},
		{
			name: "failed check under a success rollup", verdict: "failed",
			pr:     prSnapshot("SUCCESS", "MERGEABLE", "CLEAN"),
			checks: []checkRun{{"head", "completed", "timed_out"}},
			want:   pullRequestStateNeedsAttention,
		},
		{name: "failure and conflict", verdict: "failed", pr: prSnapshot("FAILURE", "CONFLICTING", "DIRTY"), want: pullRequestStateNeedsAttention},
		{name: "conflicting while checks run", verdict: "conflicting", pr: prSnapshot("PENDING", "CONFLICTING", nil), want: pullRequestStateNeedsAttention},
		{name: "dirty merge state", verdict: "conflicting", pr: prSnapshot("SUCCESS", nil, "DIRTY"), want: pullRequestStateNeedsAttention},
		{name: "rollup pending", verdict: "running", pr: prSnapshot("PENDING", "MERGEABLE", "CLEAN"), want: pullRequestStateChecking},
		{name: "rollup expected", verdict: "running", pr: prSnapshot("EXPECTED", "MERGEABLE", "BLOCKED"), want: pullRequestStateChecking},
		{
			name: "unfamiliar rollup with a running check", verdict: "running",
			pr:     prSnapshot("NEUTRAL", "MERGEABLE", "CLEAN"),
			checks: []checkRun{{"head", "in_progress", nil}},
			want:   pullRequestStateChecking,
		},
		{name: "behind base", verdict: "behind", pr: prSnapshot("SUCCESS", "MERGEABLE", "BEHIND"), want: pullRequestStateNeedsAttention},
		{name: "blocked", verdict: "blocked", pr: prSnapshot("SUCCESS", "MERGEABLE", "BLOCKED"), want: pullRequestStateNeedsAttention},
		{name: "clean", verdict: "ready", pr: prSnapshot("SUCCESS", "MERGEABLE", "CLEAN"), want: pullRequestStateReady},
		{name: "has hooks without checks", verdict: "ready", pr: prSnapshot(nil, "MERGEABLE", "HAS_HOOKS"), want: pullRequestStateReady},
		{
			name: "failed check on an old head", verdict: "ready",
			pr:     prSnapshot("SUCCESS", "MERGEABLE", "CLEAN"),
			checks: []checkRun{{"older", "completed", "failure"}},
			want:   pullRequestStateReady,
		},
		{name: "passed but unstable", verdict: "passed", pr: prSnapshot("SUCCESS", "MERGEABLE", "UNSTABLE"), want: pullRequestStateChecking},
		{name: "passed, mergeability undecided", verdict: "passed", pr: prSnapshot("SUCCESS", "UNKNOWN", "UNKNOWN"), want: pullRequestStateChecking},
		{name: "no checks", verdict: "no_checks", pr: prSnapshot(nil, nil, nil), want: pullRequestStateChecking},
	}

	f := newAwaitingMergeFixture(t)
	issueByCase := make(map[string]string, len(cases))
	for _, tc := range cases {
		issueID := f.issue(tc.name, "in_progress")
		prID := f.githubPR("open", tc.pr)
		for _, run := range tc.checks {
			f.checkRun(prID, run.headSHA, run.status, run.conclusion)
		}
		f.link(issueID, prID)
		issueByCase[tc.name] = issueID
	}

	order, stateOf := f.groups(snapshotHandler(t))
	if fmt.Sprint(order) != fmt.Sprint([]string{pullRequestStateReady, pullRequestStateNeedsAttention, pullRequestStateChecking}) {
		t.Fatalf("group order = %v", order)
	}
	for _, tc := range cases {
		if got := stateOf[issueByCase[tc.name]]; got != tc.want {
			t.Errorf("%s (verdict %s): group = %q, want %q", tc.name, tc.verdict, got, tc.want)
		}
	}

	// A deployment without the snapshot pipeline trusts no stored snapshot,
	// the way the pill shows no verdict there.
	order, stateOf = f.groups(testHandler)
	if fmt.Sprint(order) != fmt.Sprint([]string{pullRequestStateChecking}) || len(stateOf) != len(cases) {
		t.Fatalf("snapshots disabled: groups = %v over %d issues, want everything checking", order, len(stateOf))
	}
}

func withCols(base, over testutil.Cols) testutil.Cols {
	out := testutil.Cols{}
	for key, value := range base {
		out[key] = value
	}
	for key, value := range over {
		out[key] = value
	}
	return out
}

func TestIssueTablePullRequestStateWorstPullRequestWins(t *testing.T) {
	f := newAwaitingMergeFixture(t)
	connectionID := f.newUUID()
	ready := func() string { return f.githubPR("open", prSnapshot("SUCCESS", "MERGEABLE", "CLEAN")) }
	failing := func() string { return f.githubPR("open", prSnapshot("FAILURE", "MERGEABLE", "UNSTABLE")) }
	checking := func() string { return f.githubPR("open", prSnapshot("PENDING", "MERGEABLE", "CLEAN")) }
	issueWith := func(title string, prIDs ...string) string {
		issueID := f.issue(title, "in_progress")
		for _, prID := range prIDs {
			f.link(issueID, prID)
		}
		return issueID
	}

	allReady := issueWith("every PR ready", ready(), ready())
	readyAndFailing := issueWith("ready and failing", ready(), failing())
	readyAndChecking := issueWith("ready and checking", ready(), checking())
	allThree := issueWith("ready, checking and failing", ready(), checking(), failing())
	// Only open PRs vote: a failing draft or closed PR is not in the way.
	readyBesideFinished := issueWith("ready beside a draft and a closed PR", ready(),
		f.githubPR("draft", prSnapshot("FAILURE", "CONFLICTING", "DIRTY")),
		f.githubPR("closed", prSnapshot("FAILURE", "CONFLICTING", "DIRTY")))
	excludedFailing := failing()
	readyBesideRemoved := issueWith("ready beside a removed failing PR", ready(), excludedFailing)
	f.exclude(readyBesideRemoved, excludedFailing)

	readyAndVCSFailing := issueWith("ready and a failing Forgejo PR", ready())
	failingVCS := f.vcsPR(connectionID, "open", testutil.Cols{"head_sha": "vcs-failing"})
	f.commitStatus(connectionID, "vcs-failing", "passed")
	f.commitStatus(connectionID, "vcs-failing", "failed")
	f.linkVCS(readyAndVCSFailing, failingVCS)

	// A Forgejo PR never reads as ready: it has no mergeability snapshot.
	readyAndVCSPassing := issueWith("ready and a passing Forgejo PR", ready())
	passingVCS := f.vcsPR(connectionID, "open", testutil.Cols{"head_sha": "vcs-passing"})
	f.commitStatus(connectionID, "vcs-passing", "passed")
	f.linkVCS(readyAndVCSPassing, passingVCS)

	// A failure on a superseded head says nothing about the current one.
	vcsStaleFailure := f.issue("Forgejo failure on an old head", "in_progress")
	staleVCS := f.vcsPR(connectionID, "open", testutil.Cols{"head_sha": "vcs-current"})
	f.commitStatus(connectionID, "vcs-old", "failed")
	f.linkVCS(vcsStaleFailure, staleVCS)

	_, stateOf := f.groups(snapshotHandler(t))
	want := map[string]string{
		allReady:            pullRequestStateReady,
		readyAndFailing:     pullRequestStateNeedsAttention,
		readyAndChecking:    pullRequestStateChecking,
		allThree:            pullRequestStateNeedsAttention,
		readyBesideFinished: pullRequestStateReady,
		readyBesideRemoved:  pullRequestStateReady,
		readyAndVCSFailing:  pullRequestStateNeedsAttention,
		readyAndVCSPassing:  pullRequestStateChecking,
		vcsStaleFailure:     pullRequestStateChecking,
	}
	for issueID, state := range want {
		if stateOf[issueID] != state {
			t.Errorf("issue %s: group = %q, want %q", issueID, stateOf[issueID], state)
		}
	}
	if len(stateOf) != len(want) {
		t.Fatalf("grouped %d issues, want %d", len(stateOf), len(want))
	}
}

func TestIssueTableRowsSortByPullRequestUpdated(t *testing.T) {
	f := newAwaitingMergeFixture(t)
	at := func(ago string) testutil.Cols {
		return testutil.Cols{"pr_updated_at": testutil.Raw("now() - interval '" + ago + "'")}
	}
	stale := f.issue("PR updated a day ago", "todo")
	f.link(stale, f.githubPR("open", at("1 day")))
	// A merged PR's later update does not count; the open one is older.
	recentMerge := f.issue("merged recently, open PR is old", "todo")
	f.link(recentMerge, f.githubPR("merged", at("1 minute")))
	f.link(recentMerge, f.githubPR("open", at("2 days")))
	fresh := f.issue("PR updated an hour ago", "todo")
	f.link(fresh, f.githubPR("open", at("1 hour")))
	vcsFresher := f.issue("Forgejo PR updated a minute ago", "todo")
	f.linkVCS(vcsFresher, f.vcsPR(f.newUUID(), "open", at("1 minute")))

	desc := awaitingMergeQuery()
	if got, want := f.rows(testHandler, desc), []string{vcsFresher, fresh, stale, recentMerge}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("desc rows = %v, want %v", got, want)
	}
	asc := awaitingMergeQuery()
	asc.Sort.Direction = "asc"
	if got, want := f.rows(testHandler, asc), []string{recentMerge, stale, fresh, vcsFresher}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("asc rows = %v, want %v", got, want)
	}

	// The keyset cursor walks the same order one row at a time.
	var walked []string
	var cursor *string
	for {
		var page issueTableRowsResponse
		testutil.Call(t, testHandler.ListIssueTableRows, f.request("/api/issues/table/rows", issueTableRowsRequest{
			Query: desc,
			Group: issueTableGroupSpec{Kind: "none"},
			Page:  issueTablePageRequest{Limit: 1, Cursor: cursor},
		})).Want(http.StatusOK).JSON(&page)
		for _, row := range page.Rows {
			walked = append(walked, row.Issue.ID)
		}
		if page.NextCursor == nil {
			break
		}
		cursor = page.NextCursor
	}
	if want := []string{vcsFresher, fresh, stale, recentMerge}; fmt.Sprint(walked) != fmt.Sprint(want) {
		t.Fatalf("paged rows = %v, want %v", walked, want)
	}
}

func TestIssueTablePullRequestStateRejectsUnknownGroupKey(t *testing.T) {
	f := newAwaitingMergeFixture(t)
	for _, key := range []string{"pull_request_state:merged", "status:todo", ""} {
		key := key
		testutil.Call(t, testHandler.ListIssueTableRows, f.request("/api/issues/table/rows", issueTableRowsRequest{
			Query:    awaitingMergeQuery(),
			Group:    issueTableGroupSpec{Kind: "pull_request_state"},
			GroupKey: &key,
			Page:     issueTablePageRequest{Limit: 10},
		})).Want(http.StatusBadRequest)
	}
}
