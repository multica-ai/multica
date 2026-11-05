package ghsnapshot

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func testDBPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Skipf("skipping DB test: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Skipf("skipping DB test: database not reachable: %v", err)
	}
	// Close via t.Cleanup, registered FIRST, so it runs LAST — after every
	// row-deleting cleanup the test registers later, and after the context
	// cancel that stops the manager's workers. The previous shape, a `defer
	// pool.Close()` at each call site, ran BEFORE any t.Cleanup: the row
	// deletions then executed against a closed pool and failed silently, so
	// every run leaked its PR rows into the shared database. The refresh
	// worker looks rows up by ADDRESS (installation/owner/repo/number), which
	// every test here shares — once enough leaked rows accumulate, a sweep
	// applies a snapshot to a leaked row first, the onApplied signal fires for
	// THAT row, and the test asserts on its own row before the worker reaches
	// it. That is the "trailing refresh did not replace old snapshot" failure,
	// and the "closed pool" WARN spam was the same ordering bug seen from the
	// worker's side.
	t.Cleanup(pool.Close)
	return pool
}

// seedWorkspace inserts a minimal workspace and registers its cleanup.
// github_pull_request carries a workspace_id foreign key (it predates the
// no-FK convention), so the row must reference a real workspace.
func seedWorkspace(t *testing.T, pool *pgxpool.Pool) pgtype.UUID {
	t.Helper()
	slug := "ghsnap-" + randHex(t)
	var wsID pgtype.UUID
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO workspace (name, slug, description, issue_prefix) VALUES ($1,$2,$3,$4) RETURNING id`,
		"ghsnap test", slug, "ghsnap test workspace", "GHS").Scan(&wsID); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM workspace WHERE id=$1`, wsID)
	})
	return wsID
}

func randHex(t *testing.T) string {
	t.Helper()
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	const hexdigits = "0123456789abcdef"
	out := make([]byte, 16)
	for i, v := range b {
		out[i*2] = hexdigits[v>>4]
		out[i*2+1] = hexdigits[v&0x0f]
	}
	return string(out)
}

func seedPR(t *testing.T, pool *pgxpool.Pool, q *db.Queries, headSHA string) db.GithubPullRequest {
	return seedPRAt(t, pool, q, 987654, "r", 4242, headSHA)
}

func seedPRAt(t *testing.T, pool *pgxpool.Pool, q *db.Queries, installationID int64, repoName string, prNumber int32, headSHA string) db.GithubPullRequest {
	t.Helper()
	ts := pgtype.Timestamptz{Time: time.Unix(1_700_000_000, 0), Valid: true}
	pr, err := q.UpsertGitHubPullRequest(context.Background(), db.UpsertGitHubPullRequestParams{
		WorkspaceID:    seedWorkspace(t, pool),
		InstallationID: installationID,
		RepoOwner:      "o",
		RepoName:       repoName,
		PrNumber:       prNumber,
		Title:          "t",
		State:          "open",
		HtmlUrl:        "http://x",
		HeadSha:        headSHA,
		PrCreatedAt:    ts,
		PrUpdatedAt:    ts,
	})
	if err != nil {
		t.Fatalf("seed PR: %v", err)
	}
	return pr
}

func checkRunCount(t *testing.T, pool *pgxpool.Pool, prID pgtype.UUID) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM github_pull_request_check_run WHERE pr_id=$1`, prID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestListStaleUndecidedGitHubPRsExcludesDecidedAndRotatesCursor(t *testing.T) {
	pool := testDBPool(t)
	q := db.New(pool)
	ctx := context.Background()
	now := time.Unix(1_700_010_000, 0)
	// Go runs DB-backed packages concurrently against one database. Keep this
	// cursor test at the top of the BIGINT address space so ordinary fixtures
	// from another package sort only after this test's bounded page wraps.
	const (
		firstInstallationID  int64 = 1<<63 - 2
		secondInstallationID int64 = 1<<63 - 1
		beforeFixtures       int64 = firstInstallationID - 1
	)

	settled := seedPRAt(t, pool, q, firstInstallationID, "settled", 1, "S")
	oldest := seedPRAt(t, pool, q, firstInstallationID, "oldest", 2, "O")
	running := seedPRAt(t, pool, q, secondInstallationID, "running", 3, "R")
	newer := seedPRAt(t, pool, q, secondInstallationID, "newer", 4, "N")
	prs := []db.GithubPullRequest{settled, oldest, running, newer}
	t.Cleanup(func() {
		for _, pr := range prs {
			_, _ = pool.Exec(context.Background(), `DELETE FROM github_pull_request_check_run WHERE pr_id=$1`, pr.ID)
			_, _ = pool.Exec(context.Background(), `DELETE FROM github_pull_request WHERE id=$1`, pr.ID)
		}
	})

	setSnapshot := func(pr db.GithubPullRequest, fetchedAt time.Time, mergeable, rollup string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `
			UPDATE github_pull_request
			SET snapshot_head_sha=head_sha, snapshot_fetched_at=$2,
			    api_mergeable=$3, checks_rollup_state=$4
			WHERE id=$1`,
			pr.ID, fetchedAt, mergeable, rollup); err != nil {
			t.Fatal(err)
		}
	}
	setSnapshot(settled, now.Add(-time.Hour), "MERGEABLE", "SUCCESS")
	setSnapshot(oldest, now.Add(-40*time.Minute), "UNKNOWN", "PENDING")
	setSnapshot(running, now.Add(-30*time.Minute), "MERGEABLE", "SUCCESS")
	setSnapshot(newer, now.Add(-20*time.Minute), "MERGEABLE", "PENDING")
	if _, err := pool.Exec(ctx, `
		INSERT INTO github_pull_request_check_run
		    (pr_id, head_sha, ordinal, name, status, is_status_context)
		VALUES ($1, 'R', 0, 'backend', 'in_progress', false)`,
		running.ID); err != nil {
		t.Fatal(err)
	}

	rows, err := q.ListStaleUndecidedGitHubPRs(ctx, db.ListStaleUndecidedGitHubPRsParams{
		OlderThan:           tsFromTime(now.Add(-10 * time.Minute)),
		AfterInstallationID: beforeFixtures,
		AfterRepoOwner:      "",
		AfterRepoName:       "",
		AfterPrNumber:       0,
		MaxRows:             10,
	})
	if err != nil {
		t.Fatal(err)
	}
	gotRepos := make(map[string]bool, len(rows))
	for _, row := range rows {
		gotRepos[row.RepoName] = true
	}
	if gotRepos["settled"] {
		t.Fatal("decided snapshot remained in the periodic TTL sweep")
	}
	for _, repo := range []string{"oldest", "running", "newer"} {
		if !gotRepos[repo] {
			t.Fatalf("undecided repo %q missing from TTL sweep: %+v", repo, rows)
		}
	}

	first, err := q.ListStaleUndecidedGitHubPRs(ctx, db.ListStaleUndecidedGitHubPRsParams{
		OlderThan:           tsFromTime(now.Add(-10 * time.Minute)),
		AfterInstallationID: beforeFixtures,
		AfterRepoOwner:      "",
		AfterRepoName:       "",
		AfterPrNumber:       0,
		MaxRows:             1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].RepoName != "oldest" {
		t.Fatalf("first bounded sweep = %+v, want first address", first)
	}

	// Advance from the last returned address without changing its stale data.
	// Even a perpetually failing first address cannot pin the LIMIT forever.
	second, err := q.ListStaleUndecidedGitHubPRs(ctx, db.ListStaleUndecidedGitHubPRsParams{
		OlderThan:           tsFromTime(now.Add(-10 * time.Minute)),
		AfterInstallationID: first[0].InstallationID,
		AfterRepoOwner:      first[0].RepoOwner,
		AfterRepoName:       first[0].RepoName,
		AfterPrNumber:       first[0].PrNumber,
		MaxRows:             1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 || second[0].RepoName != "newer" {
		t.Fatalf("second bounded sweep = %+v, want next address", second)
	}
}

// watchPR links pr to a new issue carrying a pull_request wakeup rule for
// event, as a "wake on merge conflict" rule does.
func watchPR(t *testing.T, pool *pgxpool.Pool, pr db.GithubPullRequest, event string, enabled bool) {
	t.Helper()
	ctx := context.Background()
	var issueID pgtype.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO issue (workspace_id,title,creator_type,creator_id)
		VALUES ($1,'watch','member',gen_random_uuid()) RETURNING id`, pr.WorkspaceID).Scan(&issueID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM issue_wakeup WHERE issue_id=$1`, issueID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM issue_pull_request WHERE issue_id=$1`, issueID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM issue WHERE id=$1`, issueID)
	})
	if _, err := pool.Exec(ctx, `INSERT INTO issue_pull_request(issue_id,pull_request_id) VALUES($1,$2)`, issueID, pr.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO issue_wakeup(id,workspace_id,issue_id,instruction,kind,mode,enabled,condition)
		VALUES(gen_random_uuid(),$1,$2,'go','event','once',$3,jsonb_build_object('type','pull_request','event',$4::text))`,
		pr.WorkspaceID, issueID, enabled, event); err != nil {
		t.Fatal(err)
	}
}

// A watched address whose fetches keep failing never gets a newer snapshot.
// The bounded sweep must still move past it to the others.
func TestConflictWatchSweepRotatesPastFailingAddresses(t *testing.T) {
	pool := testDBPool(t)
	q := db.New(pool)
	ctx := context.Background()
	now := time.Now()
	const installationID int64 = 7_313_000_002
	repo := "rotate-" + randHex(t)
	for number := int32(1); number <= 2; number++ {
		pr := seedPRAt(t, pool, q, installationID, repo, number, "H")
		t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM github_pull_request WHERE id=$1`, pr.ID) })
		if _, err := pool.Exec(ctx, `UPDATE github_pull_request
			SET snapshot_head_sha=head_sha, snapshot_fetched_at=$2, api_mergeable='MERGEABLE', checks_rollup_state='SUCCESS'
			WHERE id=$1`, pr.ID, now.Add(time.Duration(number-3)*time.Hour)); err != nil {
			t.Fatal(err)
		}
		watchPR(t, pool, pr, "conflicts", true)
	}

	m := NewManager(enabledClient(t), q, pool, nil)
	m.sweepMaxRows = 1
	seen := map[int32]bool{}
	// Other packages' fixtures share this database, so allow enough passes
	// to wrap past them; fetches never run, as if every one failed.
	for sweep := 0; sweep < 50 && !(seen[1] && seen[2]); sweep++ {
		m.sweepConflictWatched(ctx)
		for len(m.queue) > 0 {
			addr := <-m.queue
			if addr.InstallationID == installationID && addr.Repo == repo {
				seen[addr.Number] = true
			}
			m.mu.Lock()
			delete(m.active, addr)
			m.mu.Unlock()
		}
	}
	if !seen[1] || !seen[2] {
		t.Fatalf("bounded sweeps visited %v; a failing address starved the rest", seen)
	}
}

func TestConflictWatchedGitHubPRQueries(t *testing.T) {
	pool := testDBPool(t)
	q := db.New(pool)
	ctx := context.Background()
	now := time.Now()
	const installationID int64 = 7_313_000_001
	repo := "watch-" + randHex(t)

	seed := func(number int32, state string, fetchedAt time.Time) db.GithubPullRequest {
		t.Helper()
		pr := seedPRAt(t, pool, q, installationID, repo, number, fmt.Sprintf("H%d", number))
		if _, err := pool.Exec(ctx, `UPDATE github_pull_request
			SET state=$2, snapshot_head_sha=head_sha, snapshot_fetched_at=$3, api_mergeable='MERGEABLE', checks_rollup_state='SUCCESS'
			WHERE id=$1`, pr.ID, state, fetchedAt); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM github_pull_request WHERE id=$1`, pr.ID) })
		return pr
	}
	watch := func(pr db.GithubPullRequest, event string, enabled bool) {
		t.Helper()
		watchPR(t, pool, pr, event, enabled)
	}

	stale := now.Add(-time.Hour)
	watched := seed(1, "open", stale)
	watch(watched, "conflicts", true)
	draft := seed(2, "draft", stale)
	watch(draft, "conflicts", true)
	fresh := seed(3, "open", now)
	watch(fresh, "conflicts", true)
	checksOnly := seed(4, "open", stale)
	watch(checksOnly, "checks_finished", true)
	disabled := seed(5, "open", stale)
	watch(disabled, "conflicts", false)
	closed := seed(6, "closed", stale)
	watch(closed, "conflicts", true)
	seed(7, "open", stale)

	rows, err := q.ListStaleConflictWatchedGitHubPRs(ctx, db.ListStaleConflictWatchedGitHubPRsParams{
		OlderThan: tsFromTime(now.Add(-5 * time.Minute)),
		MaxRows:   1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	var swept []int32
	for _, r := range rows {
		if r.InstallationID == installationID && r.RepoName == repo {
			swept = append(swept, r.PrNumber)
		}
	}
	// Decided snapshots stay in this sweep; only stale, open, watched PRs.
	if fmt.Sprint(swept) != "[1 2]" && fmt.Sprint(swept) != "[2 1]" {
		t.Fatalf("watched sweep = %v, want PRs 1 and 2", swept)
	}

	numbers, err := q.ListConflictWatchedGitHubPRNumbersInRepo(ctx, db.ListConflictWatchedGitHubPRNumbersInRepoParams{
		InstallationID: installationID, RepoOwner: "o", RepoName: repo, PrNumber: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	// After PR 2 merges: every other open watched PR, fresh or not.
	if fmt.Sprint(numbers) != "[1 3]" {
		t.Fatalf("watched PRs after a merge = %v, want [1 3]", numbers)
	}
	other, err := q.ListConflictWatchedGitHubPRNumbersInRepo(ctx, db.ListConflictWatchedGitHubPRNumbersInRepoParams{
		InstallationID: installationID, RepoOwner: "o", RepoName: repo + "-other", PrNumber: 0,
	})
	if err != nil || len(other) != 0 {
		t.Fatalf("another repository's merge must not reach these PRs: %v %v", other, err)
	}

	// Workers are not started, so the queue shows what each trigger enqueued.
	m := NewManager(enabledClient(t), q, pool, nil)
	m.mergeDelays = []time.Duration{time.Millisecond, 2 * time.Millisecond}
	queued := func() []int32 {
		t.Helper()
		var got []int32
		for {
			select {
			case addr := <-m.queue:
				if addr.InstallationID == installationID && addr.Repo == repo {
					got = append(got, addr.Number)
				}
			default:
				m.mu.Lock()
				m.active = map[address]bool{}
				m.mu.Unlock()
				return got
			}
		}
	}
	m.RefreshConflictWatchers(ctx, installationID, "o", repo, 2)
	time.Sleep(50 * time.Millisecond)
	if got := queued(); fmt.Sprint(got) != "[1 3]" && fmt.Sprint(got) != "[3 1]" {
		t.Fatalf("merge refreshed %v, want PRs 1 and 3 once each", got)
	}
	m.sweepOnce(ctx)
	if got := queued(); fmt.Sprint(got) != "[1 2]" && fmt.Sprint(got) != "[2 1]" {
		t.Fatalf("sweep refreshed %v, want stale watched PRs 1 and 2", got)
	}
}

// TestApplySnapshotHeadSHAGuard is the acceptance-criterion-1 regression: a slow
// response for an old head must never overwrite a newer head's snapshot.
func TestApplySnapshotHeadSHAGuard(t *testing.T) {
	pool := testDBPool(t)
	q := db.New(pool)
	ctx := context.Background()
	now := time.Unix(1_700_000_100, 0)

	pr := seedPR(t, pool, q, "B")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM github_pull_request_check_run WHERE pr_id=$1`, pr.ID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM github_pull_request WHERE id=$1`, pr.ID)
	})

	m := &Manager{queries: q, pool: pool, now: func() time.Time { return now }}

	// 1. A response for head "A" while the row is at "B" → discarded, nothing written.
	applied, err := m.applySnapshot(ctx, pr.ID, &PRSnapshot{HeadSHA: "A", Mergeable: "CONFLICTING", MergeStateStatus: "DIRTY"})
	if err != nil {
		t.Fatal(err)
	}
	if applied {
		t.Fatal("mismatched-head snapshot must be discarded")
	}
	got, _ := q.GetGitHubPullRequestByID(ctx, pr.ID)
	if got.SnapshotHeadSha != "" || got.ApiMergeable.Valid {
		t.Fatalf("discarded write leaked into row: %+v", got)
	}

	// 2. Matching head "B" → applied; snapshot columns + per-check rows written.
	applied, err = m.applySnapshot(ctx, pr.ID, &PRSnapshot{
		HeadSHA: "B", Mergeable: "MERGEABLE", MergeStateStatus: "CLEAN", HasChecks: true, RollupState: "FAILURE",
		Contexts: []CheckContext{
			{Name: "backend", Status: "completed", Conclusion: "failure"},
			{Name: "vercel", Status: "completed", Conclusion: "success", IsStatusContext: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !applied {
		t.Fatal("matching-head snapshot must apply")
	}
	got, _ = q.GetGitHubPullRequestByID(ctx, pr.ID)
	if got.SnapshotHeadSha != "B" || got.ApiMergeable.String != "MERGEABLE" || got.ChecksRollupState.String != "FAILURE" {
		t.Fatalf("snapshot not written: %+v", got)
	}
	if n := checkRunCount(t, pool, pr.ID); n != 2 {
		t.Fatalf("check runs = %d, want 2", n)
	}

	// 3. Head advances to "C" (a new push mirrored by the pull_request webhook);
	//    a late in-flight response for the old head "B" must be discarded and must
	//    NOT overwrite the stored snapshot.
	if _, err := pool.Exec(ctx, `UPDATE github_pull_request SET head_sha='C' WHERE id=$1`, pr.ID); err != nil {
		t.Fatal(err)
	}
	applied, err = m.applySnapshot(ctx, pr.ID, &PRSnapshot{HeadSHA: "B", Mergeable: "CONFLICTING", MergeStateStatus: "DIRTY"})
	if err != nil {
		t.Fatal(err)
	}
	if applied {
		t.Fatal("late response for the old head must be discarded once head advanced")
	}
	got, _ = q.GetGitHubPullRequestByID(ctx, pr.ID)
	if got.SnapshotHeadSha != "B" || got.ApiMergeable.String != "MERGEABLE" {
		t.Fatalf("stale late write corrupted the snapshot: %+v", got)
	}
	if n := checkRunCount(t, pool, pr.ID); n != 2 {
		t.Fatalf("check runs after stale late write = %d, want 2 (unchanged)", n)
	}
}

// TestInFlightOldHeadKeepsTrailingRefresh covers the synchronize race from the
// PR review: while head A is fetching, a webhook advances the mirrored row to B
// and enqueues again. A is discarded by the head guard, but the coalesced
// trailing edge must still fetch and apply B immediately.
func TestInFlightOldHeadKeepsTrailingRefresh(t *testing.T) {
	pool := testDBPool(t)
	q := db.New(pool)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	pr := seedPR(t, pool, q, "A")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM github_pull_request_check_run WHERE pr_id=$1`, pr.ID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM github_pull_request WHERE id=$1`, pr.ID)
	})

	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondFetched := make(chan struct{})
	applied := make(chan struct{}, 1)
	fetchCalls := 0

	m := NewManager(enabledClient(t), q, pool, func(context.Context, pgtype.UUID) {
		select {
		case applied <- struct{}{}:
		default:
		}
	})
	m.concurrency = 2
	m.sweepInterval = time.Hour
	m.jitter = func() time.Duration { return 0 }
	m.fetch = func(context.Context, *Client, int64, string, string, int32) (*PRSnapshot, error) {
		fetchCalls++
		if fetchCalls == 1 {
			close(firstStarted)
			<-releaseFirst
			return &PRSnapshot{
				HeadSHA: "A", Mergeable: "MERGEABLE", MergeStateStatus: "CLEAN",
				HasChecks: true, RollupState: "SUCCESS",
			}, nil
		}
		close(secondFetched)
		return &PRSnapshot{
			HeadSHA: "B", Mergeable: "CONFLICTING", MergeStateStatus: "DIRTY",
			HasChecks: true, RollupState: "FAILURE",
			Contexts: []CheckContext{{Name: "backend", Status: "completed", Conclusion: "failure"}},
		}, nil
	}

	m.Start(ctx)
	m.Enqueue(pr.InstallationID, pr.RepoOwner, pr.RepoName, pr.PrNumber)
	select {
	case <-firstStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("first head fetch did not start")
	}

	if _, err := pool.Exec(ctx, `UPDATE github_pull_request SET head_sha='B' WHERE id=$1`, pr.ID); err != nil {
		t.Fatal(err)
	}
	m.Enqueue(pr.InstallationID, pr.RepoOwner, pr.RepoName, pr.PrNumber)

	select {
	case <-secondFetched:
		t.Fatal("second fetch started concurrently; single-PR in-flight guard failed")
	case <-time.After(20 * time.Millisecond):
	}
	close(releaseFirst)

	select {
	case <-secondFetched:
	case <-time.After(2 * time.Second):
		t.Fatal("new-head trailing refresh was swallowed")
	}
	select {
	case <-applied:
	case <-time.After(2 * time.Second):
		t.Fatal("new-head snapshot was not applied")
	}

	got, err := q.GetGitHubPullRequestByID(context.Background(), pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SnapshotHeadSha != "B" || got.ApiMergeable.String != "CONFLICTING" {
		t.Fatalf("trailing refresh did not replace old snapshot: %+v", got)
	}
	if n := checkRunCount(t, pool, pr.ID); n != 1 {
		t.Fatalf("new-head check runs = %d, want 1", n)
	}
}

// TestApplySnapshotReplacesRuns proves each successful apply is an atomic batch
// replace, not an accumulation.
func TestApplySnapshotReplacesRuns(t *testing.T) {
	pool := testDBPool(t)
	q := db.New(pool)
	ctx := context.Background()
	now := time.Unix(1_700_000_200, 0)

	pr := seedPR(t, pool, q, "H")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM github_pull_request_check_run WHERE pr_id=$1`, pr.ID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM github_pull_request WHERE id=$1`, pr.ID)
	})
	m := &Manager{queries: q, pool: pool, now: func() time.Time { return now }}

	three := &PRSnapshot{HeadSHA: "H", Mergeable: "MERGEABLE", MergeStateStatus: "CLEAN", HasChecks: true, RollupState: "PENDING",
		Contexts: []CheckContext{{Name: "a", Status: "in_progress"}, {Name: "b", Status: "in_progress"}, {Name: "c", Status: "in_progress"}}}
	if _, err := m.applySnapshot(ctx, pr.ID, three); err != nil {
		t.Fatal(err)
	}
	if n := checkRunCount(t, pool, pr.ID); n != 3 {
		t.Fatalf("after first apply: %d runs, want 3", n)
	}
	one := &PRSnapshot{HeadSHA: "H", Mergeable: "MERGEABLE", MergeStateStatus: "CLEAN", HasChecks: true, RollupState: "SUCCESS",
		Contexts: []CheckContext{{Name: "a", Status: "completed", Conclusion: "success"}}}
	if _, err := m.applySnapshot(ctx, pr.ID, one); err != nil {
		t.Fatal(err)
	}
	if n := checkRunCount(t, pool, pr.ID); n != 1 {
		t.Fatalf("after replace: %d runs, want 1 (old runs deleted)", n)
	}
}

func TestRESTSnapshotRefreshStoresResultsAndPreservesThemOnPermissionFailure(t *testing.T) {
	pool := testDBPool(t)
	q := db.New(pool)
	ctx := context.Background()
	pr := seedPRAt(t, pool, q, 1, "r", 5, "sha1")
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM github_pull_request_check_run WHERE pr_id=$1`, pr.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM github_pull_request WHERE id=$1`, pr.ID)
	})
	var phase atomic.Int32
	srv := snapshotServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/pulls/5":
			writeTestPR(w, "sha1", true, "clean")
		case "/repos/o/r/commits/sha1/check-runs":
			if phase.Load() == 2 {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			conclusion := "success"
			if phase.Load() == 1 {
				conclusion = "failure"
			}
			_, _ = fmt.Fprintf(w, `{"total_count":1,"check_runs":[{"id":1,"head_sha":"sha1","name":"ci","status":"completed","conclusion":%q}]}`, conclusion)
		case "/repos/o/r/commits/sha1/status":
			_, _ = w.Write([]byte(`{"total_count":0,"statuses":[]}`))
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer srv.Close()
	applied := 0
	m := NewManager(newTestClient(t, srv.URL), q, pool, func(context.Context, pgtype.UUID) { applied++ })
	m.jitter = func() time.Duration { return 0 }
	addr := address{InstallationID: 1, Owner: "o", Repo: "r", Number: 5}
	for i, want := range []string{"SUCCESS", "FAILURE", "FAILURE"} {
		phase.Store(int32(i))
		m.process(ctx, addr)
		got, err := q.GetGitHubPullRequestByID(ctx, pr.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.SnapshotHeadSha != "sha1" || !got.SnapshotFetchedAt.Valid || got.ChecksRollupState.String != want || checkRunCount(t, pool, pr.ID) != 1 {
			t.Fatalf("phase %d: stored snapshot=%+v, want %s", i, got, want)
		}
	}
	if applied != 2 || len(m.rateUntil) != 0 {
		t.Fatalf("permission failure must preserve snapshot without notification or installation pause: applied=%d limits=%v", applied, m.rateUntil)
	}
}
