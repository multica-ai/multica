package attributionbackfill

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// These tests exercise the migration-198 backfill hook against a live
// Postgres. They connect to DATABASE_URL (default
// postgres://multica:multica@localhost:5432/multica?sslmode=disable),
// matching every other live-Postgres suite in the repo, and skip cleanly
// when no database is reachable so CI without a DB sees SKIP, not failure.
//
// Each test isolates itself in a throwaway schema so it never touches the
// real agent_task_queue, and drops the schema on cleanup.

const (
	// The exact strict constraint installed by migration 197, applied to
	// the fixture table so we can prove VALIDATE passes only after the hook
	// reconciles the violating rows.
	strictCheck = `originator_user_id IS NULL
		OR (accountable_user_id IS NOT NULL AND accountable_user_id = originator_user_id)`
)

func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("could not connect to %s: %v", dbURL, err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database not reachable at %s: %v", dbURL, err)
	}
	return pool
}

// newFixture creates a throwaway schema with a minimal agent_task_queue-like
// table carrying only the attribution columns. It returns the
// schema-qualified, quoted table identifier for HookOptions.Table.
//
// The strict constraint is deliberately NOT added here: callers seed their
// (possibly violating) rows first, then call addStrictConstraint, exactly
// mirroring the real upgrade order where legacy rows predate migration 197.
// A NOT VALID CHECK still rejects new INSERTs, so a violating row can only
// exist if it was written before the constraint was added.
func newFixture(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	ctx := context.Background()
	schema := fmt.Sprintf("attr_backfill_test_%d_%d", time.Now().UnixNano(), rand.IntN(1_000_000))
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %q`, schema)); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP SCHEMA %q CASCADE`, schema))
	})
	table := fmt.Sprintf("%q.agent_task_queue", schema)
	if _, err := pool.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE %s (
			id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			originator_user_id  UUID NULL,
			accountable_user_id UUID NULL,
			originator_source   TEXT NULL
		)`, table)); err != nil {
		t.Fatalf("create fixture table: %v", err)
	}
	return table
}

// addStrictConstraint installs migration 197's strict CHECK as NOT VALID,
// which does not scan existing rows — matching the real migration.
func addStrictConstraint(t *testing.T, pool *pgxpool.Pool, table string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), fmt.Sprintf(`
		ALTER TABLE %s
		ADD CONSTRAINT agent_task_queue_accountable_matches_originator_strict
		CHECK (%s) NOT VALID`, table, strictCheck)); err != nil {
		t.Fatalf("add strict constraint: %v", err)
	}
}

func TestHook_ReconcilesViolatingRowsSoValidatePasses(t *testing.T) {
	pool := newTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	table := newFixture(t, pool)

	const (
		u1 = "11111111-1111-1111-1111-111111111111" // legacy: originator set, accountable NULL, source NULL
		u2 = "22222222-2222-2222-2222-222222222222" // rolling-deploy: same shape
		u3 = "33333333-3333-3333-3333-333333333333" // already-good: accountable == originator, precise source
		u4 = "44444444-4444-4444-4444-444444444444" // mismatch originator
		u5 = "55555555-5555-5555-5555-555555555555" // mismatch accountable
		u6 = "66666666-6666-6666-6666-666666666666" // legit divergent: originator NULL, accountable set
	)

	// id, originator, accountable, source
	rows := [][4]any{
		{"aaaaaaaa-0000-0000-0000-000000000001", u1, nil, nil},             // -> backfill to u1, source 'backfill'
		{"aaaaaaaa-0000-0000-0000-000000000002", u2, nil, nil},             // -> backfill to u2, source 'backfill'
		{"aaaaaaaa-0000-0000-0000-000000000003", u3, u3, "direct_human"},   // untouched
		{"aaaaaaaa-0000-0000-0000-000000000004", u4, u5, "direct_human"},   // mismatch -> accountable u4, source kept
		{"aaaaaaaa-0000-0000-0000-000000000005", nil, u6, "rule_owner"},    // legit divergent, untouched
		{"aaaaaaaa-0000-0000-0000-000000000006", nil, nil, "unattributed"}, // orphan, untouched
	}
	for _, r := range rows {
		if _, err := pool.Exec(ctx,
			fmt.Sprintf(`INSERT INTO %s (id, originator_user_id, accountable_user_id, originator_source) VALUES ($1,$2,$3,$4)`, table),
			r[0], r[1], r[2], r[3]); err != nil {
			t.Fatalf("insert fixture row %v: %v", r[0], err)
		}
	}
	addStrictConstraint(t, pool, table)

	res, err := Hook(ctx, pool, HookOptions{Table: table})
	if err != nil {
		t.Fatalf("Hook: %v", err)
	}
	// u1, u2 (NULL accountable) + the mismatch row = 3 reconciled.
	if res.RowsBackfilled != 3 {
		t.Errorf("RowsBackfilled = %d, want 3", res.RowsBackfilled)
	}
	if res.MismatchNormalized != 1 {
		t.Errorf("MismatchNormalized = %d, want 1", res.MismatchNormalized)
	}

	// The whole point: VALIDATE now succeeds.
	if _, err := pool.Exec(ctx, fmt.Sprintf(
		`ALTER TABLE %s VALIDATE CONSTRAINT agent_task_queue_accountable_matches_originator_strict`, table)); err != nil {
		t.Fatalf("VALIDATE after hook should pass, got: %v", err)
	}

	assertRow := func(id, wantAccountable, wantSource string) {
		t.Helper()
		var acc, src *string
		if err := pool.QueryRow(ctx, fmt.Sprintf(
			`SELECT accountable_user_id::text, originator_source FROM %s WHERE id=$1`, table), id).Scan(&acc, &src); err != nil {
			t.Fatalf("read row %s: %v", id, err)
		}
		gotAcc, gotSrc := "<nil>", "<nil>"
		if acc != nil {
			gotAcc = *acc
		}
		if src != nil {
			gotSrc = *src
		}
		if gotAcc != wantAccountable || gotSrc != wantSource {
			t.Errorf("row %s = (accountable %s, source %s), want (%s, %s)", id, gotAcc, gotSrc, wantAccountable, wantSource)
		}
	}
	assertRow("aaaaaaaa-0000-0000-0000-000000000001", u1, "backfill")     // legacy filled, source stamped
	assertRow("aaaaaaaa-0000-0000-0000-000000000002", u2, "backfill")     // rolling filled, source stamped
	assertRow("aaaaaaaa-0000-0000-0000-000000000003", u3, "direct_human") // untouched
	assertRow("aaaaaaaa-0000-0000-0000-000000000004", u4, "direct_human") // normalized to originator, source preserved
	assertRow("aaaaaaaa-0000-0000-0000-000000000005", u6, "rule_owner")   // legit divergent untouched
	assertRow("aaaaaaaa-0000-0000-0000-000000000006", "<nil>", "unattributed")

	// Idempotent: a second run reconciles nothing.
	res2, err := Hook(ctx, pool, HookOptions{Table: table})
	if err != nil {
		t.Fatalf("second Hook: %v", err)
	}
	if res2.RowsBackfilled != 0 || res2.Batches != 0 {
		t.Errorf("second run not a no-op: %+v", res2)
	}
}

func TestHook_EmptyTableIsNoOp(t *testing.T) {
	pool := newTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	table := newFixture(t, pool)
	addStrictConstraint(t, pool, table)

	res, err := Hook(ctx, pool, HookOptions{Table: table})
	if err != nil {
		t.Fatalf("Hook on empty table: %v", err)
	}
	if res.RowsBackfilled != 0 || res.MismatchNormalized != 0 || res.Batches != 0 {
		t.Errorf("empty-table run should be a no-op, got %+v", res)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(
		`ALTER TABLE %s VALIDATE CONSTRAINT agent_task_queue_accountable_matches_originator_strict`, table)); err != nil {
		t.Fatalf("VALIDATE on empty table should pass, got: %v", err)
	}
}

// TestHook_ConcurrentForkNotClobbered reproduces the stale-selection race: a
// row is selected as violating, but a concurrent writer flips it to a
// legitimate originator-NULL fork (originator NULL, accountable set) while the
// hook is mid-flight. The FOR UPDATE lock + repeated outer predicate must make
// the hook skip that row rather than overwrite the writer's accountable value
// with NULL, while still reconciling the untouched violating rows.
func TestHook_ConcurrentForkNotClobbered(t *testing.T) {
	pool := newTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	table := newFixture(t, pool)

	const (
		u1 = "11111111-1111-1111-1111-111111111111" // R: legacy originator, gets flipped by the writer
		u2 = "22222222-2222-2222-2222-222222222222" // fork accountable the writer sets
		u3 = "33333333-3333-3333-3333-333333333333" // R2: control violating row, must be backfilled
	)
	rID := "aaaaaaaa-0000-0000-0000-0000000000f1"
	r2ID := "aaaaaaaa-0000-0000-0000-0000000000f2"
	for _, r := range [][2]any{{rID, u1}, {r2ID, u3}} {
		if _, err := pool.Exec(ctx, fmt.Sprintf(
			`INSERT INTO %s (id, originator_user_id, accountable_user_id, originator_source) VALUES ($1,$2,NULL,NULL)`, table),
			r[0], r[1]); err != nil {
			t.Fatalf("seed row %v: %v", r[0], err)
		}
	}
	addStrictConstraint(t, pool, table)

	// Writer B: open a transaction that flips R to a legit originator-NULL fork
	// and holds the row lock without committing yet.
	writer, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire writer conn: %v", err)
	}
	defer writer.Release()
	tx, err := writer.Begin(ctx)
	if err != nil {
		t.Fatalf("begin writer tx: %v", err)
	}
	if _, err := tx.Exec(ctx, fmt.Sprintf(
		`UPDATE %s SET originator_user_id = NULL, accountable_user_id = $2, originator_source = 'rule_owner' WHERE id = $1`, table),
		rID, u2); err != nil {
		t.Fatalf("writer flip: %v", err)
	}

	// Run the hook concurrently; it will block trying to FOR UPDATE-lock R.
	type hookResult struct {
		res Result
		err error
	}
	done := make(chan hookResult, 1)
	go func() {
		res, err := Hook(ctx, pool, HookOptions{Table: table})
		done <- hookResult{res, err}
	}()

	// Wait until the hook is actually blocked on a lock before committing the
	// writer — this deterministically forces the "row changed under the
	// selection" ordering rather than relying on a sleep.
	waitUntilBlocked(t, pool, table)

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit writer: %v", err)
	}

	hr := <-done
	if hr.err != nil {
		t.Fatalf("hook: %v", hr.err)
	}
	// Only the control row R2 should have been reconciled; R was dropped.
	if hr.res.RowsBackfilled != 1 {
		t.Errorf("RowsBackfilled = %d, want 1 (R2 only; R must be skipped)", hr.res.RowsBackfilled)
	}

	// R keeps the writer's legit fork — NOT clobbered to (NULL, NULL).
	var origR, accR, srcR *string
	if err := pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT originator_user_id::text, accountable_user_id::text, originator_source FROM %s WHERE id=$1`, table), rID).
		Scan(&origR, &accR, &srcR); err != nil {
		t.Fatalf("read R: %v", err)
	}
	if origR != nil || accR == nil || *accR != u2 || srcR == nil || *srcR != "rule_owner" {
		t.Errorf("R was clobbered: originator=%v accountable=%v source=%v; want (NULL, %s, rule_owner)", deref(origR), deref(accR), deref(srcR), u2)
	}

	// R2 was reconciled.
	var accR2, srcR2 *string
	if err := pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT accountable_user_id::text, originator_source FROM %s WHERE id=$1`, table), r2ID).Scan(&accR2, &srcR2); err != nil {
		t.Fatalf("read R2: %v", err)
	}
	if accR2 == nil || *accR2 != u3 || srcR2 == nil || *srcR2 != "backfill" {
		t.Errorf("R2 not reconciled: accountable=%v source=%v; want (%s, backfill)", deref(accR2), deref(srcR2), u3)
	}

	// And the constraint validates.
	if _, err := pool.Exec(ctx, fmt.Sprintf(
		`ALTER TABLE %s VALIDATE CONSTRAINT agent_task_queue_accountable_matches_originator_strict`, table)); err != nil {
		t.Fatalf("VALIDATE after concurrent run should pass, got: %v", err)
	}
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

// waitUntilBlocked polls pg_stat_activity until at least one backend is blocked
// on a lock while running a statement against the fixture schema, so the test
// can commit the writer at exactly the racy moment. It fails if the hook never
// blocks within the timeout, which would itself signal the FOR UPDATE guard is
// missing.
func waitUntilBlocked(t *testing.T, pool *pgxpool.Pool, table string) {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		if err := pool.QueryRow(ctx, `
			SELECT count(*) FROM pg_stat_activity
			WHERE wait_event_type = 'Lock'
			  AND state = 'active'
			  AND query ILIKE '%' || $1 || '%'`, table).Scan(&n); err != nil {
			t.Fatalf("poll pg_stat_activity: %v", err)
		}
		if n > 0 {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("hook never blocked on the row lock; FOR UPDATE guard likely missing")
}

func TestHook_BatchingReconcilesAll(t *testing.T) {
	pool := newTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	table := newFixture(t, pool)

	// Seed more violating rows than one batch so the loop must iterate.
	const n = 25
	for i := 0; i < n; i++ {
		if _, err := pool.Exec(ctx, fmt.Sprintf(
			`INSERT INTO %s (originator_user_id, accountable_user_id, originator_source) VALUES (gen_random_uuid(), NULL, NULL)`, table)); err != nil {
			t.Fatalf("seed row %d: %v", i, err)
		}
	}
	addStrictConstraint(t, pool, table)

	res, err := Hook(ctx, pool, HookOptions{Table: table, BatchSize: 10})
	if err != nil {
		t.Fatalf("Hook: %v", err)
	}
	if res.RowsBackfilled != n {
		t.Errorf("RowsBackfilled = %d, want %d", res.RowsBackfilled, n)
	}
	if res.Batches != 3 { // 10 + 10 + 5
		t.Errorf("Batches = %d, want 3", res.Batches)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(
		`ALTER TABLE %s VALIDATE CONSTRAINT agent_task_queue_accountable_matches_originator_strict`, table)); err != nil {
		t.Fatalf("VALIDATE after batched backfill should pass, got: %v", err)
	}
}
