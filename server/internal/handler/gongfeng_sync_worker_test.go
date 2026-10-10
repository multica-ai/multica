package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/vcs"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func seedGongfengSyncPR(t *testing.T, base string) (db.VcsConnection, db.VcsPullRequest) {
	t.Helper()
	token, err := testHandler.sealVCSSecret("test-token")
	if err != nil {
		t.Fatal(err)
	}
	connID := dbfx.Insert(t, "vcs_connection", testutil.Cols{
		"workspace_id": testWorkspaceID, "provider": "gongfeng", "instance_url": base,
		"access_token_encrypted": token, "webhook_secret_encrypted": token, "account_login": "alice",
	})
	prID := dbfx.Insert(t, "vcs_pull_request", testutil.Cols{
		"workspace_id": testWorkspaceID, "connection_id": connID, "provider": "gongfeng",
		"repo_owner": "acme", "repo_name": "widget", "pr_number": 7,
		"title": "Worker regression", "state": "open", "html_url": "https://git.code.tencent.com/acme/widget/merge_requests/7",
		"branch": "feature", "head_sha": "A",
		"pr_created_at": "2026-10-09T07:00:00Z", "pr_updated_at": "2026-10-09T08:00:00Z",
	})
	conn, err := testHandler.Queries.GetVCSConnectionByID(context.Background(), parseUUID(connID))
	if err != nil {
		t.Fatal(err)
	}
	pr, err := testHandler.Queries.GetVCSPullRequestInWorkspace(context.Background(), db.GetVCSPullRequestInWorkspaceParams{ID: parseUUID(prID), WorkspaceID: parseUUID(testWorkspaceID)})
	if err != nil {
		t.Fatal(err)
	}
	return conn, pr
}

func TestGongfengViewFailureBackoff(t *testing.T) {
	withVCSBox(t)
	now := time.Now()
	for _, tc := range []struct {
		name, head, snapshot, failure string
		age                           time.Duration
		want                          int
	}{
		{"first failure", "B", "", "failed", time.Second, 0},
		{"old snapshot failure", "B", "A", "failed", time.Second, 0},
		{"same head failure", "B", "B", "failed", 2 * time.Minute, 0},
		{"retry due", "B", "", "failed", 6 * time.Minute, 1},
		{"new head", "B", "A", "", time.Second, 1},
		{"fresh snapshot", "B", "B", "", time.Second, 0},
		{"no head failure", "", "", "failed", time.Second, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newGongfengSync(testHandler)
			s.now = func() time.Time { return now }
			s.onView(db.ListVCSPullRequestsByIssueRow{
				Provider: "gongfeng", HeadSha: tc.head, SnapshotHeadSha: tc.snapshot,
				SnapshotError:       tc.failure,
				SnapshotAttemptedAt: pgtype.Timestamptz{Time: now.Add(-tc.age), Valid: true},
				SnapshotFetchedAt:   pgtype.Timestamptz{Time: now.Add(-tc.age), Valid: tc.snapshot != ""},
			})
			if got := len(s.queue); got != tc.want {
				t.Fatalf("queued %d, want %d", got, tc.want)
			}
		})
	}
}

func TestGongfengImportPreservesKnownHeadAndResetsFailuresOnNewHead(t *testing.T) {
	withVCSBox(t)
	ctx := context.Background()
	conn, pr := seedGongfengSyncPR(t, "https://gongfeng-head.test")
	q := testHandler.Queries
	load := func() db.VcsPullRequest {
		t.Helper()
		row, err := q.GetVCSPullRequestInWorkspace(ctx, db.GetVCSPullRequestInWorkspaceParams{ID: pr.ID, WorkspaceID: pr.WorkspaceID})
		if err != nil {
			t.Fatal(err)
		}
		return row
	}
	dbfx.Exec(t, "UPDATE vcs_pull_request SET snapshot_head_sha='A', snapshot_fetched_at=now(), snapshot=$2 WHERE id=$1", pr.ID, []byte(`{"total":1,"passed":1,"rollup":"success"}`))
	markFailure := func(row db.VcsPullRequest) {
		t.Helper()
		if err := q.FailGongfengSnapshot(ctx, db.FailGongfengSnapshotParams{ID: row.ID, HeadSha: row.HeadSha, PrUpdatedAt: row.PrUpdatedAt, SnapshotError: "failed"}); err != nil {
			t.Fatal(err)
		}
	}
	markFailure(pr)
	event := vcs.PullRequestEvent{RepoOwner: "acme", RepoName: "widget", Number: 7, Title: "Worker regression", State: "open", HTMLURL: pr.HtmlUrl, Branch: "feature", CreatedAt: "2026-10-09T07:00:00Z", UpdatedAt: "2026-10-09T08:00:00Z"}
	if err := testHandler.mirrorVCSPullRequest(ctx, conn, event); err != nil {
		t.Fatal(err)
	}
	same := load()
	if same.HeadSha != "A" || same.SnapshotError != "failed" || !same.SnapshotAttemptedAt.Valid {
		t.Fatalf("same-revision import erased known head or cooldown: %+v", same)
	}
	event.HeadSHA, event.UpdatedAt = "B", "2026-10-09T08:01:00Z"
	if err := testHandler.mirrorVCSPullRequest(ctx, conn, event); err != nil {
		t.Fatal(err)
	}
	fresh := load()
	if fresh.HeadSha != "B" || fresh.SnapshotError != "" || fresh.SnapshotAttemptedAt.Valid {
		t.Fatalf("new head did not clear the previous failure: %+v", fresh)
	}
	markFailure(fresh)
	event.HeadSHA, event.UpdatedAt = "A", "2026-10-09T08:00:00Z"
	if err := testHandler.mirrorVCSPullRequest(ctx, conn, event); err != nil {
		t.Fatal(err)
	}
	stale := load()
	if stale.HeadSha != "B" || stale.SnapshotError != "failed" || !stale.SnapshotAttemptedAt.Valid {
		t.Fatalf("stale event erased the current failure: %+v", stale)
	}
	event.HeadSHA, event.UpdatedAt = "", "2026-10-09T08:02:00Z"
	if err := testHandler.mirrorVCSPullRequest(ctx, conn, event); err != nil {
		t.Fatal(err)
	}
	unknown := load()
	if unknown.HeadSha != "" || unknown.SnapshotError != "" || unknown.SnapshotAttemptedAt.Valid {
		t.Fatalf("new revision without a head must invalidate old checks: %+v", unknown)
	}
}

func TestGongfengWorkerKeepsTrailingRefreshForTerminalHead(t *testing.T) {
	withVCSBox(t)
	var advanced atomic.Bool
	var projects, checksA, checksB atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	mr := func() map[string]any {
		sha, stamp, state := "A", "2026-10-09T08:00:00Z", "opened"
		if advanced.Load() {
			sha, stamp, state = "B", "2026-10-09T08:01:00Z", "merged"
		}
		return map[string]any{"id": 901, "iid": 7, "title": "Worker regression", "state": state, "sha": sha, "source_project_id": 55, "source_branch": "feature", "created_at": "2026-10-09T07:00:00Z", "updated_at": stamp}
	}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/projects/acme/widget":
			projects.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 55, "path_with_namespace": "acme/widget", "web_url": "https://git.code.tencent.com/acme/widget"})
		case "/api/v3/projects/55/merge_requests":
			_ = json.NewEncoder(w).Encode([]any{mr()})
		case "/api/v3/projects/55/merge_request/901":
			_ = json.NewEncoder(w).Encode(mr())
		case "/api/v3/projects/55/commits/A/statuses":
			if checksA.Add(1) == 1 {
				close(started)
			}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			_, _ = w.Write([]byte(`[{"id":1,"context":"unit","state":"success","sha":"A"}]`))
		case "/api/v3/projects/55/commits/B/statuses":
			checksB.Add(1)
			_, _ = w.Write([]byte(`[{"id":2,"context":"unit","state":"failure","sha":"B"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(provider.Close)
	conn, pr := seedGongfengSyncPR(t, provider.URL)
	s := newGongfengSync(testHandler)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.worker(ctx); close(done) }()
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }); cancel(); <-done })
	s.enqueuePR(pr)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("old-head fetch did not start")
	}
	advanced.Store(true)
	event := vcs.PullRequestEvent{RepoOwner: "acme", RepoName: "widget", Number: 7, Title: "Worker regression", State: "merged", HTMLURL: pr.HtmlUrl, Branch: "feature", HeadSHA: "B", CreatedAt: "2026-10-09T07:00:00Z", UpdatedAt: "2026-10-09T08:01:00Z", MergedAt: "2026-10-09T08:01:00Z"}
	if err := testHandler.mirrorVCSPullRequest(ctx, conn, event); err != nil {
		t.Fatal(err)
	}
	s.enqueuePR(pr)
	s.enqueuePR(pr)
	// The snapshot's own realtime invalidation must not create another chase.
	s.onView(db.ListVCSPullRequestsByIssueRow{ID: pr.ID, WorkspaceID: pr.WorkspaceID, ConnectionID: conn.ID, Provider: "gongfeng", HeadSha: "B"})
	releaseOnce.Do(func() { close(release) })
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		idle := len(s.pending) == 0
		s.mu.Unlock()
		if idle {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	row, err := testHandler.Queries.GetVCSPullRequestInWorkspace(ctx, db.GetVCSPullRequestInWorkspaceParams{ID: pr.ID, WorkspaceID: pr.WorkspaceID})
	if err != nil {
		t.Fatal(err)
	}
	var checks vcs.GongfengChecks
	if err := json.Unmarshal(row.Snapshot, &checks); err != nil {
		t.Fatal(err)
	}
	if row.State != "merged" || row.SnapshotHeadSha != "B" || checks.Failed != 1 || projects.Load() != 2 || checksA.Load() != 1 || checksB.Load() != 1 {
		t.Fatalf("trailing refresh not applied exactly once: state=%s head=%s checks=%+v requests=%d/%d/%d", row.State, row.SnapshotHeadSha, checks, projects.Load(), checksA.Load(), checksB.Load())
	}
}

func TestGongfengRateLimitAppliesAcrossJobsAndAllowsOtherConnections(t *testing.T) {
	withVCSBox(t)
	var calls, otherCalls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "600")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(provider.Close)
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { otherCalls.Add(1); w.WriteHeader(503) }))
	t.Cleanup(other.Close)
	conn, pr := seedGongfengSyncPR(t, provider.URL)
	_, otherPR := seedGongfengSyncPR(t, other.URL)
	s := newGongfengSync(testHandler)
	now := time.Now()
	s.now = func() time.Time { return now }
	ctx := context.Background()
	s.refreshPR(ctx, pr)
	s.refreshPR(ctx, pr)
	repo, err := testHandler.Queries.AddGongfengRepository(ctx, db.AddGongfengRepositoryParams{ConnectionID: conn.ID, ProjectID: 55, Path: "acme/widget", WebUrl: "https://git.code.tencent.com/acme/widget"})
	if err != nil {
		t.Fatal(err)
	}
	dbfx.Cleanup(t, "DELETE FROM gongfeng_repository WHERE connection_id=$1", conn.ID)
	s.syncRepository(ctx, repo)
	s.refreshPR(ctx, otherPR)
	if calls.Load() != 1 || otherCalls.Load() != 1 {
		t.Fatalf("rate limit leaked across jobs or tenants: %d / %d", calls.Load(), otherCalls.Load())
	}
	now = now.Add(6 * time.Minute)
	s.refreshPR(ctx, pr)
	if calls.Load() != 1 {
		t.Fatal("ignored the provider's longer Retry-After")
	}
	now = now.Add(5 * time.Minute)
	s.refreshPR(ctx, pr)
	if calls.Load() != 2 {
		t.Fatal("expired rate limit did not retry")
	}
	// Reconnection/credential rotation must not inherit a previous token's pause.
	if _, err := testHandler.Queries.RotateVCSConnectionWebhookSecret(ctx, db.RotateVCSConnectionWebhookSecretParams{ID: conn.ID, WorkspaceID: conn.WorkspaceID, WebhookSecretEncrypted: conn.WebhookSecretEncrypted}); err != nil {
		t.Fatal(err)
	}
	s.refreshPR(ctx, pr)
	if calls.Load() != 3 {
		t.Fatal("new connection revision inherited the old cooldown")
	}
	row, err := testHandler.Queries.GetVCSPullRequestInWorkspace(ctx, db.GetVCSPullRequestInWorkspaceParams{ID: pr.ID, WorkspaceID: pr.WorkspaceID})
	if err != nil || !strings.Contains(row.SnapshotError, "rate limit") {
		t.Fatalf("missing safe error: %+v %v", row, err)
	}
}

func TestGongfengSweepRotatesPastPausedBatches(t *testing.T) {
	withVCSBox(t)
	ctx := context.Background()
	conn, firstPR := seedGongfengSyncPR(t, "https://gongfeng-sweep.test")
	issueID := dbfx.Issue(t, "Gongfeng sweep fairness")
	dbfx.Cleanup(t, "DELETE FROM issue_vcs_pull_request WHERE issue_id=$1", issueID)
	ids := map[pgtype.UUID]bool{firstPR.ID: false}
	for n := 20; n < 61; n++ {
		id := dbfx.Insert(t, "vcs_pull_request", testutil.Cols{
			"workspace_id": testWorkspaceID, "connection_id": conn.ID, "provider": "gongfeng",
			"repo_owner": "acme", "repo_name": "widget", "pr_number": n,
			"title": "Waiting for snapshot", "state": "open", "html_url": "https://git.code.tencent.com/acme/widget/merge_requests/7",
			"pr_created_at": "2026-10-09T07:00:00Z", "pr_updated_at": "2026-10-09T08:00:00Z",
		})
		ids[parseUUID(id)] = false
	}
	for id := range ids {
		if _, err := testHandler.Queries.LinkIssueToVCSPullRequest(ctx, db.LinkIssueToVCSPullRequestParams{IssueID: parseUUID(issueID), PullRequestID: id}); err != nil {
			t.Fatal(err)
		}
	}
	for n := int64(1); n <= 21; n++ {
		if _, err := testHandler.Queries.AddGongfengRepository(ctx, db.AddGongfengRepositoryParams{ConnectionID: conn.ID, ProjectID: n, Path: "acme/widget", WebUrl: "https://git.code.tencent.com/acme/widget"}); err != nil {
			t.Fatal(err)
		}
	}
	dbfx.Cleanup(t, "DELETE FROM gongfeng_repository WHERE connection_id=$1", conn.ID)
	s := newGongfengSync(testHandler)
	repositories := map[int64]bool{}
	// No workers run: every row remains due, just as skipped rate-limited jobs
	// do. Consecutive sweeps must still rotate past the first bounded batch.
	for batch := 0; batch < 2; batch++ {
		s.sweep(ctx)
		for len(s.queue) > 0 {
			job := <-s.queue
			delete(s.pending, job.key)
			if job.pr != nil {
				if _, ok := ids[job.pr.ID]; ok {
					ids[job.pr.ID] = true
				}
			} else if job.repository.ConnectionID == conn.ID {
				repositories[job.repository.ProjectID] = true
			}
		}
	}
	for id, seen := range ids {
		if !seen {
			t.Fatalf("MR %s starved behind a paused batch", uuidToString(id))
		}
	}
	if len(repositories) != 21 {
		t.Fatalf("visited %d repositories, want 21", len(repositories))
	}
}
