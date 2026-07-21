package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MUL-4809 §4.1 P0-2 — migration 212 re-run safety.
//
// 212 builds a UNIQUE index CONCURRENTLY on agent_task_queue.retry_of_task_id, and
// CreateRetryTask's idempotent ON CONFLICT resolves against it. Real PostgreSQL leaves an
// INVALID index behind when a concurrent unique build fails on pre-existing duplicates,
// and `CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS` then SUCCEEDS on the re-run — which
// would record 212 as applied while every auto-retry still fails with 42P10. These tests
// pin the pre-migration hook that makes the sequence safe.
//
// The hook references agent_task_queue unqualified, so the sandbox gives it a private
// schema on the search_path rather than touching the real table.

// newRetryOfHookSandbox creates an isolated schema holding a minimal agent_task_queue and
// returns a pool whose search_path resolves to it.
func newRetryOfHookSandbox(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	admin := openTestPool(t)
	ctx := context.Background()
	schema := fmt.Sprintf("mul4809_hook_%d_%d", time.Now().UnixNano(), rand.Uint32())
	if _, err := admin.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %s`, pgx.Identifier{schema}.Sanitize())); err != nil {
		t.Fatalf("create sandbox schema: %v", err)
	}
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := admin.Exec(c, fmt.Sprintf(`DROP SCHEMA IF EXISTS %s CASCADE`, pgx.Identifier{schema}.Sanitize())); err != nil {
			t.Logf("drop sandbox schema %s: %v", schema, err)
		}
	})

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	cfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		t.Fatalf("parse sandbox config: %v", err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("open sandbox pool: %v", err)
	}
	t.Cleanup(pool.Close)

	if _, err := pool.Exec(ctx, `
		CREATE TABLE agent_task_queue (
			id               BIGSERIAL PRIMARY KEY,
			retry_of_task_id BIGINT
		)`); err != nil {
		t.Fatalf("create sandbox agent_task_queue: %v", err)
	}
	return pool, schema
}

// retryOfMigrationOpts writes the real 212 body to a temp dir and wires runOptions to the
// sandbox bookkeeping table plus the PRODUCTION hook map.
func retryOfMigrationOpts(t *testing.T, schema string) runOptions {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "212_agent_task_retry_of_unique.up.sql")
	body := "CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS " + retryOfUniqueIndexName + "\n" +
		"    ON agent_task_queue (retry_of_task_id)\n" +
		"    WHERE retry_of_task_id IS NOT NULL;\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write 212 migration: %v", err)
	}
	return runOptions{
		Direction:             "up",
		Files:                 []string{path},
		SchemaMigrationsTable: schema + ".schema_migrations",
		AdvisoryLockKey:       int64(rand.Uint64()&0x7fffffffffffffff) | 1,
		Hooks:                 preMigrationHooks,
	}
}

// retryOfIndexState reports whether the sandbox index exists and whether it is valid,
// resolved through the pool's search_path (same resolution the hook uses).
func retryOfIndexState(t *testing.T, pool *pgxpool.Pool) (exists, valid bool) {
	t.Helper()
	var v *bool
	if err := pool.QueryRow(context.Background(),
		`SELECT (SELECT i.indisvalid FROM pg_index i WHERE i.indexrelid = to_regclass($1))`,
		retryOfUniqueIndexName).Scan(&v); err != nil {
		t.Fatalf("inspect index state: %v", err)
	}
	if v == nil {
		return false, false
	}
	return true, *v
}

func retryOfAppliedCount(t *testing.T, pool *pgxpool.Pool, table string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		fmt.Sprintf(`SELECT count(*) FROM %s WHERE version = $1`, table),
		"212_agent_task_retry_of_unique").Scan(&n); err != nil {
		t.Fatalf("count applied versions: %v", err)
	}
	return n
}

// TestRetryOfUniqueHookBlocksPreexistingDuplicates: nothing constrained retry_of_task_id
// before 212, so a rolling deploy could carry duplicates. The hook must hard-fail the run
// rather than let a silently-unenforced index through, and the failed migration must NOT be
// recorded as applied.
func TestRetryOfUniqueHookBlocksPreexistingDuplicates(t *testing.T) {
	pool, schema := newRetryOfHookSandbox(t)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `INSERT INTO agent_task_queue (retry_of_task_id) VALUES (1), (1)`); err != nil {
		t.Fatalf("seed duplicates: %v", err)
	}

	opts := retryOfMigrationOpts(t, schema)
	if err := runMigrations(ctx, pool, opts); err == nil {
		t.Fatal("migration must fail while duplicate retry_of_task_id rows exist")
	}
	if n := retryOfAppliedCount(t, pool, opts.SchemaMigrationsTable); n != 0 {
		t.Fatalf("a blocked migration must not be recorded as applied, got %d row(s)", n)
	}
	if exists, valid := retryOfIndexState(t, pool); exists && !valid {
		t.Fatal("a blocked migration must not leave an invalid index behind")
	}
}

// TestRetryOfUniqueHookClearsLeftoverInvalidIndexAndRebuilds reproduces the exact
// PostgreSQL sequence: a first concurrent unique build fails on duplicates and leaves an
// INVALID index. Once the duplicates are resolved, re-running the migration must NOT be
// masked by `IF NOT EXISTS` — the hook drops the leftover invalid index and the migration
// rebuilds a VALID one, so ON CONFLICT keeps working.
func TestRetryOfUniqueHookClearsLeftoverInvalidIndexAndRebuilds(t *testing.T) {
	pool, schema := newRetryOfHookSandbox(t)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `INSERT INTO agent_task_queue (retry_of_task_id) VALUES (1), (1)`); err != nil {
		t.Fatalf("seed duplicates: %v", err)
	}
	// The first build fails and leaves an invalid index behind — real PG behaviour.
	if _, err := pool.Exec(ctx, `CREATE UNIQUE INDEX CONCURRENTLY `+retryOfUniqueIndexName+
		` ON agent_task_queue (retry_of_task_id) WHERE retry_of_task_id IS NOT NULL`); err == nil {
		t.Fatal("expected the first concurrent unique build to fail on duplicates")
	}
	if exists, valid := retryOfIndexState(t, pool); !exists || valid {
		t.Fatalf("precondition: expected a leftover INVALID index, exists=%v valid=%v", exists, valid)
	}

	// The operator resolves the duplicates, keeping the earliest successor.
	if _, err := pool.Exec(ctx, `
		DELETE FROM agent_task_queue
		WHERE id NOT IN (SELECT min(id) FROM agent_task_queue GROUP BY retry_of_task_id)`); err != nil {
		t.Fatalf("resolve duplicates: %v", err)
	}

	opts := retryOfMigrationOpts(t, schema)
	if err := runMigrations(ctx, pool, opts); err != nil {
		t.Fatalf("migration after duplicates were resolved: %v", err)
	}
	exists, valid := retryOfIndexState(t, pool)
	if !exists || !valid {
		t.Fatalf("expected a VALID unique index after rebuild, exists=%v valid=%v", exists, valid)
	}
	if n := retryOfAppliedCount(t, pool, opts.SchemaMigrationsTable); n != 1 {
		t.Fatalf("successful migration should be recorded exactly once, got %d", n)
	}
}
