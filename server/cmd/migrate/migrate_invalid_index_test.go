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
)

func TestRunMigrationsRepairsInvalidConcurrentIndexBeforeRetry(t *testing.T) {
	pool := openTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	suffix := fmt.Sprintf("%d_%d", time.Now().UnixNano(), rand.Uint32())
	schema := "migrate_invalid_index_" + suffix
	schemaIdent := pgx.Identifier{schema}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE SCHEMA "+schemaIdent); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DROP SCHEMA IF EXISTS "+schemaIdent+" CASCADE"); err != nil {
			t.Logf("drop schema %s: %v", schema, err)
		}
	})

	tableName := pgx.Identifier{schema, "agent_task_queue"}.Sanitize()
	v1Name := pgx.Identifier{"idx_one_pending_task_per_issue_agent"}.Sanitize()
	v2Name := pgx.Identifier{"idx_one_pending_task_per_issue_agent_v2"}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE TABLE "+tableName+` (
		id BIGSERIAL PRIMARY KEY,
		issue_id BIGINT NOT NULL,
		agent_id BIGINT NOT NULL,
		status TEXT NOT NULL,
		context JSONB NOT NULL DEFAULT '{}'
	)`); err != nil {
		t.Fatalf("create task table: %v", err)
	}
	if _, err := pool.Exec(ctx, "CREATE UNIQUE INDEX CONCURRENTLY "+v1Name+" ON "+tableName+" (issue_id, agent_id) WHERE status IN ('queued', 'dispatched')"); err != nil {
		t.Fatalf("create valid v1 index: %v", err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO "+tableName+` (issue_id, agent_id, status, context) VALUES
		(1, 1, 'deferred', '{"channel_issue_media_pending":true}'),
		(1, 1, 'deferred', '{"channel_issue_media_pending":true}')`); err != nil {
		t.Fatalf("seed duplicate deferred rows: %v", err)
	}

	createV2 := "CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS " + v2Name + " ON " + tableName + " (issue_id, agent_id) WHERE status IN ('queued', 'dispatched') OR (status = 'deferred' AND context->>'channel_issue_media_pending' = 'true')"
	if _, err := pool.Exec(ctx, createV2); err == nil {
		t.Fatal("initial v2 build unexpectedly succeeded with duplicate deferred rows")
	}
	assertIndexValidity(t, pool, schema, "idx_one_pending_task_per_issue_agent", true)
	assertIndexValidity(t, pool, schema, "idx_one_pending_task_per_issue_agent_v2", false)

	if _, err := pool.Exec(ctx, "DELETE FROM "+tableName+" WHERE id = (SELECT max(id) FROM "+tableName+")"); err != nil {
		t.Fatalf("remove conflicting row before retry: %v", err)
	}

	const version = "257_agent_task_queue_channel_media_pending_unique_v2"
	if preMigrationHooks[version] == nil {
		t.Fatalf("production hook is not registered for %s", version)
	}
	migrationPath := filepath.Join(t.TempDir(), version+".up.sql")
	if err := os.WriteFile(migrationPath, []byte(createV2+";\n"), 0o600); err != nil {
		t.Fatalf("write retry migration: %v", err)
	}
	indexRegclass := pgx.Identifier{schema, "idx_one_pending_task_per_issue_agent_v2"}.Sanitize()
	opts := runOptions{
		Direction:             "up",
		Files:                 []string{migrationPath},
		SchemaMigrationsTable: schema + ".schema_migrations",
		AdvisoryLockKey:       int64(rand.Uint64()&0x7fffffffffffffff) | 1,
		Hooks: map[string]preMigrationHook{
			version: cleanupInvalidConcurrentIndexHook(indexRegclass),
		},
	}
	if err := runMigrations(ctx, pool, opts); err != nil {
		t.Fatalf("retry migration with invalid-index cleanup: %v", err)
	}

	assertIndexValidity(t, pool, schema, "idx_one_pending_task_per_issue_agent", true)
	assertIndexValidity(t, pool, schema, "idx_one_pending_task_per_issue_agent_v2", true)
	var recorded bool
	if err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM "+pgx.Identifier{schema, "schema_migrations"}.Sanitize()+" WHERE version = $1)", version).Scan(&recorded); err != nil {
		t.Fatalf("read migration version: %v", err)
	}
	if !recorded {
		t.Fatal("repaired migration was not recorded")
	}
}

// TestRunMigrationsRepairsInvalidTerminalCompletedAtIndex is the MUL-5823
// counterpart of the test above, for the 261/262 partial-index swap.
//
// The hazard is identical but the blast radius is different: 261 builds the
// replacement terminal-task index and 262 drops the v1 it replaces. If an
// interrupted 261 leaves an INVALID v2, `CREATE INDEX ... IF NOT EXISTS`
// reports success on retry, the runner records 261, and 262 then drops the
// only valid index — putting all four dashboard rollups on a full table scan.
//
// Unlike migration 257's unique index, this one cannot be failed with a
// duplicate row, so the build is interrupted the way a real one is: a
// concurrent open transaction blocks the wait phase until statement_timeout
// cancels it, which is exactly what leaves indisvalid = false behind.
func TestRunMigrationsRepairsInvalidTerminalCompletedAtIndex(t *testing.T) {
	pool := openTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	suffix := fmt.Sprintf("%d_%d", time.Now().UnixNano(), rand.Uint32())
	schema := "migrate_terminal_idx_" + suffix
	schemaIdent := pgx.Identifier{schema}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE SCHEMA "+schemaIdent); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DROP SCHEMA IF EXISTS "+schemaIdent+" CASCADE"); err != nil {
			t.Logf("drop schema %s: %v", schema, err)
		}
	})

	const (
		v1Index = "idx_agent_task_queue_terminal_completed_at"
		v2Index = "idx_agent_task_queue_terminal_completed_at_v2"
	)
	tableName := pgx.Identifier{schema, "agent_task_queue"}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE TABLE "+tableName+` (
		id BIGSERIAL PRIMARY KEY,
		status TEXT NOT NULL,
		completed_at TIMESTAMPTZ
	)`); err != nil {
		t.Fatalf("create task table: %v", err)
	}
	if _, err := pool.Exec(ctx, "CREATE INDEX CONCURRENTLY "+pgx.Identifier{v1Index}.Sanitize()+
		" ON "+tableName+" (completed_at) WHERE status IN ('completed', 'failed')"); err != nil {
		t.Fatalf("create valid v1 index: %v", err)
	}

	createV2 := "CREATE INDEX CONCURRENTLY IF NOT EXISTS " + pgx.Identifier{v2Index}.Sanitize() +
		" ON " + tableName + " (completed_at) WHERE status IN ('completed', 'failed', 'cancelled')"

	// Blocker: an open transaction that has written to the table owns an xid
	// the concurrent build must wait on, so the build is guaranteed to reach
	// its wait phase rather than finishing first.
	blocker, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire blocker conn: %v", err)
	}
	blockerTx, err := blocker.Begin(ctx)
	if err != nil {
		blocker.Release()
		t.Fatalf("begin blocker tx: %v", err)
	}
	if _, err := blockerTx.Exec(ctx, "INSERT INTO "+tableName+" (status, completed_at) VALUES ('completed', now())"); err != nil {
		blocker.Release()
		t.Fatalf("blocker insert: %v", err)
	}

	builder, err := pool.Acquire(ctx)
	if err != nil {
		blocker.Release()
		t.Fatalf("acquire builder conn: %v", err)
	}
	// SET must ride the same connection as the build, and must be its own
	// protocol message — a multi-command string would put CREATE INDEX
	// CONCURRENTLY in an implicit transaction, which PostgreSQL rejects.
	if _, err := builder.Exec(ctx, "SET statement_timeout = '2s'"); err != nil {
		builder.Release()
		blocker.Release()
		t.Fatalf("set statement_timeout: %v", err)
	}
	_, buildErr := builder.Exec(ctx, createV2)
	// Clear the timeout before the connection goes back to the pool; pgxpool
	// does not reset session state, so leaving it set would arm a 2s fuse on
	// whichever later caller happens to draw this connection.
	if _, err := builder.Exec(ctx, "SET statement_timeout = DEFAULT"); err != nil {
		t.Logf("reset statement_timeout: %v", err)
	}
	builder.Release()
	if buildErr == nil {
		blocker.Release()
		t.Fatal("interrupted v2 build unexpectedly succeeded")
	}
	_ = blockerTx.Rollback(ctx)
	blocker.Release()

	assertIndexValidity(t, pool, schema, v1Index, true)
	assertIndexValidity(t, pool, schema, v2Index, false)

	// Guard the premise: without the hook, the retry is a silent no-op that
	// leaves the index invalid. This is what makes dropping v1 unsafe.
	if _, err := pool.Exec(ctx, createV2); err != nil {
		t.Fatalf("bare retry should report success: %v", err)
	}
	assertIndexValidity(t, pool, schema, v2Index, false)

	// The production hook names the index unqualified, which resolves against
	// the search_path — correct in production where it lives in public, but
	// invisible from this test's private schema. So assert the registration
	// exists, then run with a schema-qualified instance of the same hook.
	const version = "261_agent_task_queue_terminal_completed_at_v2"
	if preMigrationHooks[version] == nil {
		t.Fatalf("production hook is not registered for %s", version)
	}
	dir := t.TempDir()
	upPath := filepath.Join(dir, version+".up.sql")
	if err := os.WriteFile(upPath, []byte(createV2+";\n"), 0o600); err != nil {
		t.Fatalf("write retry migration: %v", err)
	}
	opts := runOptions{
		Direction:             "up",
		Files:                 []string{upPath},
		SchemaMigrationsTable: schema + ".schema_migrations",
		AdvisoryLockKey:       int64(rand.Uint64()&0x7fffffffffffffff) | 1,
		Hooks: map[string]preMigrationHook{
			version: cleanupInvalidConcurrentIndexHook(pgx.Identifier{schema, v2Index}.Sanitize()),
		},
	}
	if err := runMigrations(ctx, pool, opts); err != nil {
		t.Fatalf("retry migration with invalid-index cleanup: %v", err)
	}

	// v2 rebuilt for real, and v1 still standing to cover the window.
	assertIndexValidity(t, pool, schema, v1Index, true)
	assertIndexValidity(t, pool, schema, v2Index, true)

	// Only now is migration 262 safe: it drops v1 and the workspace is left
	// with a valid index rather than none.
	const dropVersion = "262_drop_agent_task_queue_terminal_completed_at_v1"
	dropPath := filepath.Join(dir, dropVersion+".up.sql")
	dropSQL := "DROP INDEX CONCURRENTLY IF EXISTS " + pgx.Identifier{schema, v1Index}.Sanitize() + ";\n"
	if err := os.WriteFile(dropPath, []byte(dropSQL), 0o600); err != nil {
		t.Fatalf("write drop migration: %v", err)
	}
	opts.Files = []string{dropPath}
	if err := runMigrations(ctx, pool, opts); err != nil {
		t.Fatalf("apply v1 drop: %v", err)
	}
	assertIndexValidity(t, pool, schema, v2Index, true)
	var v1Exists bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_class c
			JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = $1 AND c.relname = $2
		)
	`, schema, v1Index).Scan(&v1Exists); err != nil {
		t.Fatalf("check v1 existence: %v", err)
	}
	if v1Exists {
		t.Fatal("migration 262 should have dropped v1")
	}
}

func assertIndexValidity(t *testing.T, pool interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, schema, index string, want bool) {
	t.Helper()
	var valid bool
	if err := pool.QueryRow(context.Background(), `
		SELECT i.indisvalid
		FROM pg_index i
		JOIN pg_class c ON c.oid = i.indexrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relname = $2
	`, schema, index).Scan(&valid); err != nil {
		t.Fatalf("read validity for %s.%s: %v", schema, index, err)
	}
	if valid != want {
		t.Fatalf("index %s.%s validity = %v, want %v", schema, index, valid, want)
	}
}
