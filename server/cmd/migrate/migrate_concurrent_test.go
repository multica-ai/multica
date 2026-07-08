package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"
)

// MUL-2956 — concurrent migration race test.
//
// PR multica-ai/multica#3658 (MUL-2923) added a Postgres advisory lock
// around the migration loop to serialize concurrent runners. This file
// is the live-Postgres test that proves the lock is actually doing its
// job. We run N goroutines that all call runMigrations against the same
// database with the same options, and assert:
//
//  1. Pending: when migrations have NOT been applied, every goroutine
//     returns nil and exactly one application of each migration lands
//     in the bookkeeping table — no duplicate-key blow-ups, no missing
//     rows, and (since our test fixtures are deliberately non-idempotent
//     bare CREATE TABLE / ALTER TABLE) no "relation already exists"
//     failures from the SQL itself, which would prove the lock isn't
//     serializing.
//  2. Already applied: rerunning the same N-way race against the just-
//     populated bookkeeping table sends every goroutine down the EXISTS
//     no-op path; nobody re-applies anything and the underlying schema
//     is unchanged.
//  3. Lock serialization: while one connection holds the same advisory
//     lock externally, every concurrent runMigrations is observed to
//     wait, and only after the external holder releases does the lock
//     get acquired. This catches the regression where the lock would
//     get attached to a random pooled connection (the bug fixed in
//     MUL-2923 / #3658) and effectively become a no-op.
//
// The test connects to whatever DATABASE_URL points at (default
// postgres://multica:multica@localhost:5432/multica?sslmode=disable),
// matching the harness pattern already used in
// server/internal/handler/handler_test.go and
// server/internal/metrics/business_sampler_pgsleep_test.go. If
// Postgres is unreachable the suite skips cleanly, the same way every
// other live-Postgres test in the repo skips, so CI without a database
// sees SKIP rather than failure.
//
// Each test isolates itself by creating a unique throwaway schema
// (migrate_test_<timestamp>_<rand>) and using a unique advisory-lock
// key per run. That means the test never touches the real
// schema_migrations table and never blocks behind a real production
// migration runner sharing the same database. The schema is dropped
// during cleanup.

const (
	// concurrentRunners is the goroutine count for the race tests. Set
	// large enough that a missing lock would reliably trip on a multi-
	// core box with -race, but small enough to keep the suite fast on a
	// single shared Postgres.
	concurrentRunners = 16
	// raceTestTimeout bounds every individual concurrent step; if the
	// lock implementation regresses into a deadlock we fail loudly
	// instead of hanging the suite.
	raceTestTimeout = 60 * time.Second
)

func openTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("could not connect to %s: %v", dbURL, err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database not reachable at %s: %v", dbURL, err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// fixture is the per-test sandbox: a private schema, a unique advisory
// lock key, and a temp directory full of deliberately non-idempotent
// migration SQL files.
type fixture struct {
	pool       *pgxpool.Pool
	schema     string
	tableFQN   string // e.g. "migrate_test_xyz"."schema_migrations"
	lockKey    int64
	files      []string // sorted .up.sql paths
	versions   []string // matching versions
	tableNames []string // distinct test tables each migration creates
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	pool := openTestPool(t)

	// Unique schema and lock key per test invocation. We salt with both
	// nanos and a process-local random so re-running with -count=N still
	// gets a distinct sandbox per iteration even if the wall clock has
	// not visibly advanced.
	suffix := fmt.Sprintf("%d_%d", time.Now().UnixNano(), rand.Uint32())
	schema := "migrate_test_" + suffix
	tableFQN := schema + ".schema_migrations"
	// Random non-zero positive int64. The high bit is masked off to
	// keep this in the same numeric range pg_advisory_lock expects, and
	// the OR with 1 guarantees we never end up at zero. Collision with
	// the production migrationAdvisoryLockKey constant is not strictly
	// impossible — both are int64 — but the probability is on the order
	// of 1 in 2^62, which is negligible for a unit-test sandbox.
	lockKey := int64(rand.Uint64()&0x7fffffffffffffff) | 1

	ctx := context.Background()
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %s`, pgx.Identifier{schema}.Sanitize())); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		// Use a fresh context so cleanup still runs if the test ctx was
		// cancelled. Drop CASCADE to take everything down even when a
		// half-applied migration left orphan tables behind.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := pool.Exec(ctx, fmt.Sprintf(`DROP SCHEMA IF EXISTS %s CASCADE`, pgx.Identifier{schema}.Sanitize())); err != nil {
			t.Logf("drop schema %s: %v", schema, err)
		}
	})

	// Build a small set of deliberately non-idempotent migrations. Each
	// one creates a distinct table inside our schema. Bare CREATE TABLE
	// (no IF NOT EXISTS) and ALTER TABLE ADD COLUMN (no IF NOT EXISTS)
	// guarantee that if two goroutines actually ran the same migration
	// in parallel, the second one would error with "relation already
	// exists" / "column already exists" — which is exactly the failure
	// signature we want the test to catch when the lock regresses.
	dir := t.TempDir()
	const numFiles = 5
	files := make([]string, 0, numFiles)
	versions := make([]string, 0, numFiles)
	tableNames := make([]string, 0, numFiles)
	for i := 0; i < numFiles; i++ {
		version := fmt.Sprintf("%03d_test_%s", i+1, suffix)
		tableName := fmt.Sprintf("t_%s_%d", suffix, i+1)
		// Reference both the schema and the table, then add a column in
		// a follow-up statement. Either statement run twice (i.e.
		// concurrent re-application by another goroutine that won the
		// race past the EXISTS check) would error.
		body := fmt.Sprintf(
			"CREATE TABLE %s.%s (id BIGSERIAL PRIMARY KEY);\n"+
				"ALTER TABLE %s.%s ADD COLUMN payload TEXT NOT NULL DEFAULT '';\n",
			pgx.Identifier{schema}.Sanitize(), pgx.Identifier{tableName}.Sanitize(),
			pgx.Identifier{schema}.Sanitize(), pgx.Identifier{tableName}.Sanitize(),
		)
		path := filepath.Join(dir, version+".up.sql")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write migration: %v", err)
		}
		files = append(files, path)
		versions = append(versions, version)
		tableNames = append(tableNames, tableName)
	}
	sort.Strings(files)

	return &fixture{
		pool:       pool,
		schema:     schema,
		tableFQN:   tableFQN,
		lockKey:    lockKey,
		files:      files,
		versions:   versions,
		tableNames: tableNames,
	}
}

func (f *fixture) opts() runOptions {
	return runOptions{
		Direction:             "up",
		Files:                 f.files,
		SchemaMigrationsTable: f.tableFQN,
		AdvisoryLockKey:       f.lockKey,
	}
}

// appliedVersions returns the versions recorded in the bookkeeping
// table, sorted ascending. Empty slice means the table is empty (or
// does not yet exist, which the helper reports as a fatal error).
func (f *fixture) appliedVersions(t *testing.T) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rows, err := f.pool.Query(ctx,
		fmt.Sprintf(`SELECT version FROM %s ORDER BY version`,
			pgx.Identifier{f.schema, "schema_migrations"}.Sanitize()))
	if err != nil {
		t.Fatalf("read schema_migrations: %v", err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan version: %v", err)
		}
		got = append(got, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}
	return got
}

// tableExists checks that a given table is present inside the fixture
// schema. Used to confirm migrations actually executed, not just that
// the bookkeeping rows landed.
func (f *fixture) tableExists(t *testing.T, name string) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var exists bool
	if err := f.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = $1 AND table_name = $2
		)`, f.schema, name).Scan(&exists); err != nil {
		t.Fatalf("check table %s.%s: %v", f.schema, name, err)
	}
	return exists
}

// TestRunMigrationsConcurrentPending fires N goroutines at runMigrations
// against a fresh schema where none of the migrations have been applied
// yet. The advisory lock must serialize them so that exactly one of
// them executes each CREATE TABLE / ALTER TABLE and exactly one row
// per migration lands in schema_migrations. If the lock is broken,
// either the SQL fails ("relation already exists") or the bookkeeping
// table picks up duplicate-key violations on the version primary key.
func TestRunMigrationsConcurrentPending(t *testing.T) {
	f := newFixture(t)

	ctx, cancel := context.WithTimeout(context.Background(), raceTestTimeout)
	defer cancel()

	g, gctx := errgroup.WithContext(ctx)
	// errgroup limits concurrency only when SetLimit is called; we want
	// every goroutine running at once so they all queue on the lock.
	for i := 0; i < concurrentRunners; i++ {
		g.Go(func() error {
			return runMigrations(gctx, f.pool, f.opts())
		})
	}
	if err := g.Wait(); err != nil {
		t.Fatalf("concurrent runMigrations(up) on pending schema returned error: %v", err)
	}

	got := f.appliedVersions(t)
	if want := f.versions; !equalStrings(got, want) {
		t.Fatalf("schema_migrations contents = %v, want %v", got, want)
	}
	for _, tbl := range f.tableNames {
		if !f.tableExists(t, tbl) {
			t.Fatalf("expected table %s.%s to exist after concurrent up, missing", f.schema, tbl)
		}
	}
}

// TestRunMigrationsConcurrentAlreadyApplied first applies the
// migrations once (single-threaded, to establish a clean baseline) and
// then fires N goroutines at runMigrations again. Every goroutine must
// hit the EXISTS no-op path and return nil, the bookkeeping table must
// stay exactly the way the baseline left it, and the underlying tables
// must not have been touched (no duplicate CREATE / ALTER blow-ups).
//
// This is the path that matters in production: most pod restarts find
// the database fully migrated and just need to confirm-and-skip.
func TestRunMigrationsConcurrentAlreadyApplied(t *testing.T) {
	f := newFixture(t)

	ctx, cancel := context.WithTimeout(context.Background(), raceTestTimeout)
	defer cancel()

	// Baseline single-threaded run.
	if err := runMigrations(ctx, f.pool, f.opts()); err != nil {
		t.Fatalf("baseline runMigrations: %v", err)
	}
	baseline := f.appliedVersions(t)
	if !equalStrings(baseline, f.versions) {
		t.Fatalf("baseline schema_migrations = %v, want %v", baseline, f.versions)
	}

	// Concurrent re-run: every goroutine should hit the EXISTS no-op
	// branch and return nil.
	g, gctx := errgroup.WithContext(ctx)
	for i := 0; i < concurrentRunners; i++ {
		g.Go(func() error {
			return runMigrations(gctx, f.pool, f.opts())
		})
	}
	if err := g.Wait(); err != nil {
		t.Fatalf("concurrent runMigrations(up) on already-applied schema returned error: %v", err)
	}

	got := f.appliedVersions(t)
	if !equalStrings(got, baseline) {
		t.Fatalf("schema_migrations changed after concurrent re-run: got %v, want %v", got, baseline)
	}
}

// TestRunMigrationsAdvisoryLockSerializes proves the lock genuinely
// blocks contenders. We acquire the same advisory key on a side
// connection BEFORE spawning any runMigrations goroutine, then start N
// goroutines and watch how many of them have made it past the lock
// acquire. The expectation:
//
//   - While the side connection holds the lock, zero goroutines have
//     completed (we observe via a small delay + count-check).
//   - The moment the side connection releases the lock, the goroutines
//     start unblocking and finish in well under the test timeout.
//
// If the advisory lock had regressed back to attaching to a random
// pooled connection (the original MUL-2923 bug), the side-held lock
// would not actually block a fresh pool.Acquire from grabbing its own
// connection without the lock, and the goroutines would all complete
// while the lock was still "held" — which is exactly what this test
// detects.
func TestRunMigrationsAdvisoryLockSerializes(t *testing.T) {
	f := newFixture(t)

	ctx, cancel := context.WithTimeout(context.Background(), raceTestTimeout)
	defer cancel()

	// Acquire the lock on a pinned side connection. We use a *pgx.Conn
	// (not pool.Acquire) so the lock holder is not reachable through
	// the pool the runMigrations goroutines draw from — the lock is
	// session-scoped and we want the behaviour to be "the next pool
	// connection that calls pg_advisory_lock blocks", not "the same
	// connection re-enters". (pg_advisory_lock is reentrant on the same
	// session, so re-acquiring on the same conn would not actually
	// prove serialization.)
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	holder, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatalf("side connect: %v", err)
	}
	defer holder.Close(context.Background())
	if _, err := holder.Exec(ctx, "SELECT pg_advisory_lock($1)", f.lockKey); err != nil {
		t.Fatalf("side acquire lock: %v", err)
	}

	var done int64
	var startedAt = time.Now()
	g, gctx := errgroup.WithContext(ctx)
	for i := 0; i < concurrentRunners; i++ {
		g.Go(func() error {
			err := runMigrations(gctx, f.pool, f.opts())
			atomic.AddInt64(&done, 1)
			return err
		})
	}

	// Watchdog: while the side holder still has the lock, no
	// runMigrations goroutine should have completed. We sample for a
	// generous window (1 s) — much longer than the trivial migration
	// set takes to apply on the unlocked path — to give a regressed
	// implementation room to incorrectly succeed.
	const observeWindow = 1 * time.Second
	const observeStep = 50 * time.Millisecond
	deadline := time.Now().Add(observeWindow)
	for time.Now().Before(deadline) {
		if n := atomic.LoadInt64(&done); n != 0 {
			t.Fatalf("advisory lock did not block: %d/%d goroutines finished while side connection held the lock for %s",
				n, concurrentRunners, time.Since(startedAt))
		}
		time.Sleep(observeStep)
	}

	// Release the side lock and wait for all goroutines to finish.
	if _, err := holder.Exec(ctx, "SELECT pg_advisory_unlock($1)", f.lockKey); err != nil {
		t.Fatalf("side release lock: %v", err)
	}
	if err := g.Wait(); err != nil {
		t.Fatalf("concurrent runMigrations after lock release returned error: %v", err)
	}

	// Sanity: state ended up as the pending case did — exactly one
	// application of every migration.
	if got, want := f.appliedVersions(t), f.versions; !equalStrings(got, want) {
		t.Fatalf("schema_migrations after lock-release race = %v, want %v", got, want)
	}
}

// TestRunMigrationsConcurrentMixedPoolStress runs the pending case
// against a deliberately under-sized pool to put pressure on the
// "every runner needs its own pinned connection for the lock" code
// path. If runMigrations ever regresses into using pool.Exec (which
// could give the lock and the migration steps different connections),
// this test will deadlock or produce SQL races. Pool size strictly
// less than runners is the interesting configuration.
func TestRunMigrationsConcurrentMixedPoolStress(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	cfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		t.Skipf("parse DATABASE_URL: %v", err)
	}
	// Small pool: half the runner count, minimum 2. This forces
	// runners to wait on pgxpool.Acquire AND on pg_advisory_lock,
	// exercising the same connection lifecycle a real multi-replica
	// startup would.
	cfg.MaxConns = int32(concurrentRunners / 2)
	if cfg.MaxConns < 2 {
		cfg.MaxConns = 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), raceTestTimeout)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Skipf("could not open small pool: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Skipf("small pool not reachable: %v", err)
	}

	// Reuse the standard fixture's schema/files/lock-key wiring but
	// substitute the small-pool *pgxpool.Pool so the test exercises a
	// different connection budget.
	big := newFixture(t)
	f := *big
	f.pool = pool

	g, gctx := errgroup.WithContext(ctx)
	var startedOnce sync.Once
	startedAt := time.Time{}
	for i := 0; i < concurrentRunners; i++ {
		g.Go(func() error {
			startedOnce.Do(func() { startedAt = time.Now() })
			return runMigrations(gctx, f.pool, f.opts())
		})
	}
	if err := g.Wait(); err != nil {
		t.Fatalf("small-pool concurrent runMigrations error after %s: %v", time.Since(startedAt), err)
	}
	if got, want := big.appliedVersions(t), big.versions; !equalStrings(got, want) {
		t.Fatalf("small-pool schema_migrations = %v, want %v", got, want)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestRunMigrationsRejectsInvalidDirection pins the direction
// whitelist contract: anything other than "up" or "down" must error
// before runMigrations touches the pool. This prevents the subtle bug
// where an empty or typo'd direction silently fell through to the
// "down" branch (`opts.Direction == "up"` is false → else branch
// handles it as a rollback).
//
// The check runs ahead of any pool/conn use, so passing nil is safe
// and lets this case execute without a live Postgres.
func TestRunMigrationsRejectsInvalidDirection(t *testing.T) {
	bad := []string{"", "UP", "DOWN", "rollback", "x", " up "}
	for _, dir := range bad {
		err := runMigrations(context.Background(), nil, runOptions{Direction: dir})
		if err == nil {
			t.Errorf("direction %q: want error, got nil", dir)
			continue
		}
		if !strings.Contains(err.Error(), "invalid direction") {
			t.Errorf("direction %q: error %q does not mention 'invalid direction'", dir, err)
		}
	}
}
