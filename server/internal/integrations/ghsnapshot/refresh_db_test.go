package ghsnapshot

import (
	"context"
	"crypto/rand"
	"os"
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
	t.Helper()
	ts := pgtype.Timestamptz{Time: time.Unix(1_700_000_000, 0), Valid: true}
	pr, err := q.UpsertGitHubPullRequest(context.Background(), db.UpsertGitHubPullRequestParams{
		WorkspaceID:    seedWorkspace(t, pool),
		InstallationID: 987654,
		RepoOwner:      "o",
		RepoName:       "r",
		PrNumber:       4242,
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

// TestApplySnapshotHeadSHAGuard is the acceptance-criterion-1 regression: a slow
// response for an old head must never overwrite a newer head's snapshot.
func TestApplySnapshotHeadSHAGuard(t *testing.T) {
	pool := testDBPool(t)
	defer pool.Close()
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
	defer pool.Close()
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
	defer pool.Close()
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
