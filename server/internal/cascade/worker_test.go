package cascade

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// workerTestDBPool / workerMakeWorkspaceAndUser / workerInsertCascadeIssue
// are local fixtures specific to worker_test.go. They mirror the
// pattern from internal/handler/handler_test.go: read DATABASE_URL,
// fall back to dev defaults, skip if unreachable. Named with a
// worker prefix to avoid collision when a sibling test file is added.
func workerTestDBPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Skipf("no database: %v", err)
		return nil
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database not reachable: %v", err)
		return nil
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

func workerMakeWorkspaceAndUser(t *testing.T, pool *pgxpool.Pool) (uuid.UUID, func()) {
	t.Helper()
	ctx := context.Background()
	var userID, workspaceID string
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('test', $1) RETURNING id`,
		"worker-test-"+uuid.New().String()+"@example.com").Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO workspace (name, slug) VALUES ('worker-test', $1) RETURNING id`,
		"worker-test-"+uuid.New().String()[:8]).Scan(&workspaceID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`,
		workspaceID, userID); err != nil {
		t.Fatalf("insert member: %v", err)
	}
	wsUUID, _ := uuid.Parse(workspaceID)
	cleanup := func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	}
	return wsUUID, cleanup
}

func workerInsertCascadeIssue(t *testing.T, pool *pgxpool.Pool, workspaceID uuid.UUID, number int, state string, _ *time.Time, progressJSON string) uuid.UUID {
	t.Helper()
	var creatorID string
	if err := pool.QueryRow(context.Background(),
		`SELECT user_id::text FROM member WHERE workspace_id = $1 LIMIT 1`, workspaceID).Scan(&creatorID); err != nil {
		t.Fatalf("lookup owner: %v", err)
	}
	var pj any
	if progressJSON != "" {
		pj = progressJSON
	}
	var id string
	if err := pool.QueryRow(context.Background(), `
        INSERT INTO issue (workspace_id, title, status, creator_type, creator_id, number,
                           cascade_state, cascade_started_at, cascade_progress)
        VALUES ($1, $2, 'in_progress', 'member', $3, $4, $5, now() - interval '5 minutes', $6::jsonb)
        RETURNING id`,
		workspaceID, "worker test "+state, creatorID, number, state, pj,
	).Scan(&id); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	u, _ := uuid.Parse(id)
	return u
}

// fakeSpawner records every Spawn call. HasActiveRun returns the
// stored bool.
type fakeSpawner struct {
	spawnCalls atomic.Int64
	hasActive  atomic.Bool
	spawnErr   error
	lastIssue  uuid.UUID
	lastCtx    TriggerContext
}

func (f *fakeSpawner) Spawn(_ context.Context, issueID uuid.UUID, tc TriggerContext) error {
	f.spawnCalls.Add(1)
	f.lastIssue = issueID
	f.lastCtx = tc
	return f.spawnErr
}
func (f *fakeSpawner) HasActiveRun(_ context.Context, _ uuid.UUID) (bool, error) {
	return f.hasActive.Load(), nil
}

func setupWorkerTest(t *testing.T) (*pgxpool.Pool, uuid.UUID, uuid.UUID, func()) {
	t.Helper()
	pool := workerTestDBPool(t)
	if pool == nil {
		return nil, uuid.Nil, uuid.Nil, nil
	}
	ws, cleanup := workerMakeWorkspaceAndUser(t, pool)
	// Insert an issue with cascade_state='approved' so the worker
	// has a target to spawn for.
	issueID := workerInsertCascadeIssue(t, pool, ws, 9001, "approved", nil, `{"total_prs":3,"current_step":1}`)
	return pool, ws, issueID, cleanup
}

// insertRetrigger inserts a row directly with issue_id set so the
// worker's lookup path can be skipped and we test the rest of the
// pipeline.
func insertRetrigger(t *testing.T, pool *pgxpool.Pool, issueID uuid.UUID, prURL, headSHA, eventType string) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(context.Background(), `
        INSERT INTO cascade_retrigger (event_id, issue_id, pr_url, pr_number, head_sha, event_type)
        VALUES ($1, $2, $3, 1, $4, $5)
        RETURNING id`,
		uuid.New(), issueID, prURL, headSHA, eventType,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert retrigger: %v", err)
	}
	return id
}

func TestWorker_HappyPath_SpawnsAndMarks(t *testing.T) {
	pool, _, issueID, cleanup := setupWorkerTest(t)
	if pool == nil {
		return
	}
	defer cleanup()

	rowID := insertRetrigger(t, pool, issueID, "https://github.com/o/r/pull/1", "sha-1", "ci_failure")
	defer pool.Exec(context.Background(), `DELETE FROM cascade_retrigger WHERE id = $1`, rowID)

	sp := &fakeSpawner{}
	w := NewWorker(pool, sp, nil)
	w.PollOnce(context.Background())

	if sp.spawnCalls.Load() != 1 {
		t.Fatalf("expected 1 spawn, got %d", sp.spawnCalls.Load())
	}
	if sp.lastIssue != issueID {
		t.Errorf("spawned wrong issue: got %v, want %v", sp.lastIssue, issueID)
	}

	// Row must be marked processed with action='spawn'.
	var action string
	var processedAt *time.Time
	if err := pool.QueryRow(context.Background(),
		`SELECT action, processed_at FROM cascade_retrigger WHERE id = $1`, rowID).Scan(&action, &processedAt); err != nil {
		t.Fatalf("read back row: %v", err)
	}
	if action != "spawn" {
		t.Errorf("action = %q, want spawn", action)
	}
	if processedAt == nil {
		t.Errorf("processed_at not set")
	}
}

func TestWorker_ActiveRun_QueuesPending(t *testing.T) {
	pool, _, issueID, cleanup := setupWorkerTest(t)
	if pool == nil {
		return
	}
	defer cleanup()

	rowID := insertRetrigger(t, pool, issueID, "https://github.com/o/r/pull/2", "sha-q", "pr_review_change")
	defer pool.Exec(context.Background(), `DELETE FROM cascade_retrigger WHERE id = $1`, rowID)

	sp := &fakeSpawner{}
	sp.hasActive.Store(true) // active run on this issue

	w := NewWorker(pool, sp, nil)
	w.PollOnce(context.Background())

	if sp.spawnCalls.Load() != 0 {
		t.Errorf("expected no spawn when run active, got %d", sp.spawnCalls.Load())
	}

	// Pending row must exist for this issue.
	var pendingEID uuid.UUID
	if err := pool.QueryRow(context.Background(),
		`SELECT event_id FROM cascade_pending_event WHERE issue_id = $1`, issueID).Scan(&pendingEID); err != nil {
		t.Fatalf("expected pending row: %v", err)
	}

	// Action on the row must be 'queued_pending'.
	var action string
	if err := pool.QueryRow(context.Background(),
		`SELECT action FROM cascade_retrigger WHERE id = $1`, rowID).Scan(&action); err != nil {
		t.Fatalf("read action: %v", err)
	}
	if action != "queued_pending" {
		t.Errorf("action = %q, want queued_pending", action)
	}
}

func TestWorker_LoopGuard_TripsAfterThreshold(t *testing.T) {
	pool, _, issueID, cleanup := setupWorkerTest(t)
	if pool == nil {
		return
	}
	defer cleanup()

	prURL := "https://github.com/o/r/pull/9999"
	// Pre-seed 3 distinct-head_sha 'spawn' rows in the 6h window.
	for i, sha := range []string{"a", "b", "c"} {
		_, err := pool.Exec(context.Background(), `
            INSERT INTO cascade_retrigger (event_id, issue_id, pr_url, pr_number, head_sha, event_type, action, processed_at)
            VALUES ($1, $2, $3, $4, $5, 'ci_failure', 'spawn', now() - interval '1 hour')`,
			uuid.New(), issueID, prURL, i+1, sha)
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	// 4th retrigger — should trip the guard.
	rowID := insertRetrigger(t, pool, issueID, prURL, "d", "ci_failure")
	defer pool.Exec(context.Background(), `DELETE FROM cascade_retrigger WHERE pr_url = $1`, prURL)

	sp := &fakeSpawner{}
	w := NewWorker(pool, sp, nil)
	w.PollOnce(context.Background())

	if sp.spawnCalls.Load() != 0 {
		t.Errorf("expected no spawn after loop guard, got %d", sp.spawnCalls.Load())
	}

	var state string
	if err := pool.QueryRow(context.Background(),
		`SELECT cascade_state FROM issue WHERE id = $1`, issueID).Scan(&state); err != nil {
		t.Fatalf("read state: %v", err)
	}
	if state != "loop_guarded" {
		t.Errorf("cascade_state = %q, want loop_guarded", state)
	}

	var action string
	_ = pool.QueryRow(context.Background(),
		`SELECT action FROM cascade_retrigger WHERE id = $1`, rowID).Scan(&action)
	if action != "loop_guard_skip" {
		t.Errorf("action = %q, want loop_guard_skip", action)
	}
}

func TestWorker_SpawnFailureLeavesRowUnprocessed(t *testing.T) {
	pool, _, issueID, cleanup := setupWorkerTest(t)
	if pool == nil {
		return
	}
	defer cleanup()

	rowID := insertRetrigger(t, pool, issueID, "https://github.com/o/r/pull/3", "sha-fail", "ci_failure")
	defer pool.Exec(context.Background(), `DELETE FROM cascade_retrigger WHERE id = $1`, rowID)

	sp := &fakeSpawner{spawnErr: errors.New("spawn boom")}
	w := NewWorker(pool, sp, nil)
	w.PollOnce(context.Background())

	if sp.spawnCalls.Load() != 1 {
		t.Errorf("expected spawn attempt, got %d", sp.spawnCalls.Load())
	}

	var action *string
	var processedAt *time.Time
	if err := pool.QueryRow(context.Background(),
		`SELECT action, processed_at FROM cascade_retrigger WHERE id = $1`, rowID).Scan(&action, &processedAt); err != nil {
		t.Fatalf("read row: %v", err)
	}
	if processedAt != nil {
		t.Errorf("expected processed_at NULL after spawn failure, got %v", processedAt)
	}
	if action != nil {
		t.Errorf("expected action NULL after spawn failure, got %q", *action)
	}
}

func TestWorker_NoIssueIDIsScopeSkip(t *testing.T) {
	// Per the worker's contract, rows with issue_id NULL are
	// scope-skipped (a follow-up will backfill them when title +
	// branch land on the row schema). This pins that behavior so a
	// future change doesn't silently spawn against NULL issues.
	pool := workerTestDBPool(t)
	if pool == nil {
		return
	}
	ws, cleanup := workerMakeWorkspaceAndUser(t, pool)
	defer cleanup()
	_ = ws

	var rowID int64
	if err := pool.QueryRow(context.Background(), `
        INSERT INTO cascade_retrigger (event_id, pr_url, pr_number, head_sha, event_type)
        VALUES ($1, 'u', 1, 's', 'ci_failure')
        RETURNING id`, uuid.New()).Scan(&rowID); err != nil {
		t.Fatalf("insert: %v", err)
	}
	defer pool.Exec(context.Background(), `DELETE FROM cascade_retrigger WHERE id = $1`, rowID)

	sp := &fakeSpawner{}
	w := NewWorker(pool, sp, nil)
	w.PollOnce(context.Background())

	if sp.spawnCalls.Load() != 0 {
		t.Errorf("expected no spawn for NULL issue_id, got %d", sp.spawnCalls.Load())
	}
	var action string
	_ = pool.QueryRow(context.Background(),
		`SELECT action FROM cascade_retrigger WHERE id = $1`, rowID).Scan(&action)
	if action != "scope_filter_skip" {
		t.Errorf("action = %q, want scope_filter_skip", action)
	}
}

func TestWorker_DrainPending_SpawnsWhenPending(t *testing.T) {
	pool, _, issueID, cleanup := setupWorkerTest(t)
	if pool == nil {
		return
	}
	defer cleanup()

	// Seed a pending event manually (in production the worker writes
	// this via queuePending; here we go straight to SQL to keep the
	// test focused on the drain side).
	eid := uuid.New()
	tcJSON := `{"event_id":"` + eid.String() + `","event_type":"pr_merged","pr_url":"https://x/y/pull/9","pr_number":9,"head_sha":"merged-sha"}`

	// queuePending needs a cascade_retrigger row to reference (FK).
	var retrigID int64
	_ = pool.QueryRow(context.Background(), `
        INSERT INTO cascade_retrigger (event_id, issue_id, pr_url, pr_number, head_sha, event_type)
        VALUES ($1, $2, 'u', 9, 's', 'pr_merged') RETURNING id`,
		eid, issueID).Scan(&retrigID)
	defer pool.Exec(context.Background(), `DELETE FROM cascade_retrigger WHERE id = $1`, retrigID)

	_, err := pool.Exec(context.Background(),
		`INSERT INTO cascade_pending_event (issue_id, event_id, trigger_context) VALUES ($1, $2, $3::jsonb)`,
		issueID, eid, tcJSON)
	if err != nil {
		t.Fatalf("seed pending: %v", err)
	}

	sp := &fakeSpawner{}
	w := NewWorker(pool, sp, nil)
	w.DrainPending(context.Background(), issueID)

	if sp.spawnCalls.Load() != 1 {
		t.Fatalf("expected drain spawn, got %d", sp.spawnCalls.Load())
	}
	if sp.lastCtx.EventID != eid {
		t.Errorf("trigger context not propagated: got %v, want %v", sp.lastCtx.EventID, eid)
	}

	// Pending row must be gone.
	var n int
	_ = pool.QueryRow(context.Background(),
		`SELECT count(*) FROM cascade_pending_event WHERE issue_id = $1`, issueID).Scan(&n)
	if n != 0 {
		t.Errorf("pending row not deleted: count = %d", n)
	}
}

func TestWorker_DrainPending_NoPendingIsQuiet(t *testing.T) {
	pool, _, issueID, cleanup := setupWorkerTest(t)
	if pool == nil {
		return
	}
	defer cleanup()

	sp := &fakeSpawner{}
	w := NewWorker(pool, sp, nil)
	w.DrainPending(context.Background(), issueID)
	if sp.spawnCalls.Load() != 0 {
		t.Errorf("expected no spawn when no pending, got %d", sp.spawnCalls.Load())
	}
}
