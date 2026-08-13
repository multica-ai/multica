package main

import (
	"bytes"
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

var concurrentIndexNamePattern = regexp.MustCompile(
	`(?i)CREATE\s+(?:UNIQUE\s+)?INDEX\s+CONCURRENTLY\s+(?:IF\s+NOT\s+EXISTS\s+)?([a-z0-9_]+)`)

// stripSQLLineComments drops `--` comment lines so prose that mentions SQL is
// not mistaken for SQL.
func stripSQLLineComments(body []byte) []byte {
	var kept [][]byte
	for _, line := range bytes.Split(body, []byte("\n")) {
		if bytes.HasPrefix(bytes.TrimSpace(line), []byte("--")) {
			continue
		}
		kept = append(kept, line)
	}
	return bytes.Join(kept, []byte("\n"))
}

// TestConcurrentIndexCleanupsMatchTheirMigrations guards the mapping that wires
// invalid-index cleanup hooks to migrations. A hook that names an index no
// migration creates is a silent no-op: the retry then treats the INVALID
// leftover as success and the index stays unusable. Nothing at runtime would
// report that, so the names are checked against the migration files here.
func TestConcurrentIndexCleanupsMatchTheirMigrations(t *testing.T) {
	assertConcurrentIndexCleanupsMatchTheirMigrations(
		t,
		concurrentIndexCleanups,
		preMigrationHooks,
		"up",
	)
	assertConcurrentIndexCleanupsMatchTheirMigrations(
		t,
		concurrentDownIndexCleanups,
		preRollbackHooks,
		"down",
	)

	// The MUL-5999 batch specifically: every one of these builds an index the
	// new teardown queries depend on, so none of them may lose its hook.
	for _, version := range []string{
		"273_agent_task_queue_runtime_id_index",
		"274_task_token_workspace_id_index",
		"275_task_token_agent_id_index",
		"276_chat_draft_restore_task_id_index",
		"277_autopilot_run_task_id_index",
	} {
		if _, ok := concurrentIndexCleanups[version]; !ok {
			t.Errorf("%s: missing from concurrentIndexCleanups", version)
		}
	}

}

// TestEveryConcurrentDownBuildHasCleanup works in the opposite direction from
// TestConcurrentIndexCleanupsMatchTheirMigrations: every rollback migration
// that builds an index concurrently must be registered. This prevents a new or
// historical down migration from silently missing retry cleanup.
func TestEveryConcurrentDownBuildHasCleanup(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.down.sql"))
	if err != nil {
		t.Fatalf("glob down migrations: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("no down migrations found")
	}

	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s: read: %v", path, err)
			continue
		}
		matches := concurrentIndexNamePattern.FindAllSubmatch(stripSQLLineComments(body), -1)
		if len(matches) == 0 {
			continue
		}
		version := strings.TrimSuffix(filepath.Base(path), ".down.sql")
		if len(matches) != 1 {
			t.Errorf("%s: has %d concurrent index builds; cleanup registration supports exactly one", version, len(matches))
			continue
		}
		indexName := string(matches[0][1])
		registered, ok := concurrentDownIndexCleanups[version]
		if !ok {
			t.Errorf("%s: builds %q concurrently on rollback but has no down cleanup", version, indexName)
			continue
		}
		if registered != indexName {
			t.Errorf("%s: down cleanup registers %q, migration builds %q", version, registered, indexName)
		}
	}
}

func assertConcurrentIndexCleanupsMatchTheirMigrations(
	t *testing.T,
	cleanups map[string]string,
	hooks map[string]preMigrationHook,
	direction string,
) {
	t.Helper()
	for version, indexName := range cleanups {
		path := filepath.Join("..", "..", "migrations", version+"."+direction+".sql")
		body, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s: read migration: %v", version, err)
			continue
		}
		// The comment headers on these migrations mention CREATE INDEX
		// CONCURRENTLY in prose, so match statements only.
		match := concurrentIndexNamePattern.FindSubmatch(stripSQLLineComments(body))
		if match == nil {
			t.Errorf("%s: has a cleanup hook but builds no index concurrently", version)
			continue
		}
		if got := string(match[1]); got != indexName {
			t.Errorf("%s: hook cleans %q but the migration builds %q", version, indexName, got)
		}
		if hooks[version] == nil {
			t.Errorf("%s: no pre-migration hook registered", version)
		}
	}
}

// TestRunMigrationsRepairsInvalidRuntimeIDIndex is the MUL-5999 counterpart of
// the 257 / 261 repair tests, run against migration 273's real SQL and its real
// registered hook.
//
// 273 is the representative case: it uses `CREATE INDEX CONCURRENTLY IF NOT
// EXISTS` on agent_task_queue, so an interrupted build leaves an INVALID index
// that the retry would otherwise skip past, recording the migration as applied
// while every all-status runtime_id lookup — teardown's runtime path and the
// FK's own cascade probe — stays on a full table scan.
func TestRunMigrationsRepairsInvalidRuntimeIDIndex(t *testing.T) {
	pool := openTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	suffix := fmt.Sprintf("%d_%d", time.Now().UnixNano(), rand.Uint32())
	schema := "migrate_mul5999_" + suffix
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

	const indexName = "idx_agent_task_queue_runtime_id"
	const version = "273_agent_task_queue_runtime_id_index"
	tableName := pgx.Identifier{schema, "agent_task_queue"}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE TABLE "+tableName+` (
		id BIGSERIAL PRIMARY KEY,
		runtime_id UUID
	)`); err != nil {
		t.Fatalf("create task table: %v", err)
	}

	qualifiedIndex := pgx.Identifier{schema, indexName}.Sanitize()
	createIndex := "CREATE INDEX CONCURRENTLY IF NOT EXISTS " + pgx.Identifier{indexName}.Sanitize() +
		" ON " + tableName + " (runtime_id)"

	// Interrupt the build the way a real one gets interrupted: an open
	// transaction that has written to the table owns an xid the concurrent
	// build must wait for, and statement_timeout cancels the wait.
	blocker, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire blocker conn: %v", err)
	}
	blockerTx, err := blocker.Begin(ctx)
	if err != nil {
		blocker.Release()
		t.Fatalf("begin blocker tx: %v", err)
	}
	if _, err := blockerTx.Exec(ctx, "INSERT INTO "+tableName+" (runtime_id) VALUES (gen_random_uuid())"); err != nil {
		blocker.Release()
		t.Fatalf("blocker insert: %v", err)
	}

	builder, err := pool.Acquire(ctx)
	if err != nil {
		blocker.Release()
		t.Fatalf("acquire builder conn: %v", err)
	}
	if _, err := builder.Exec(ctx, "SET statement_timeout = '2s'"); err != nil {
		builder.Release()
		blocker.Release()
		t.Fatalf("set statement_timeout: %v", err)
	}
	_, buildErr := builder.Exec(ctx, createIndex)
	// pgxpool does not reset session state, so clear the fuse before the
	// connection goes back to the pool.
	if _, err := builder.Exec(ctx, "SET statement_timeout = DEFAULT"); err != nil {
		t.Logf("reset statement_timeout: %v", err)
	}
	builder.Release()
	if buildErr == nil {
		blocker.Release()
		t.Fatal("interrupted build unexpectedly succeeded")
	}
	_ = blockerTx.Rollback(ctx)
	blocker.Release()

	assertIndexValidity(t, pool, schema, indexName, false)

	migrationPath := filepath.Join(t.TempDir(), version+".up.sql")
	if err := os.WriteFile(migrationPath, []byte(createIndex+";\n"), 0o600); err != nil {
		t.Fatalf("write retry migration: %v", err)
	}
	opts := runOptions{
		Direction:             "up",
		Files:                 []string{migrationPath},
		SchemaMigrationsTable: schema + ".schema_migrations",
		AdvisoryLockKey:       int64(rand.Uint64()&0x7fffffffffffffff) | 1,
	}

	// Without the hook the retry is a silent no-op: IF NOT EXISTS sees the
	// invalid relation, reports success, and the migration is recorded.
	if err := runMigrations(ctx, pool, opts); err != nil {
		t.Fatalf("retry without hook: %v", err)
	}
	assertIndexValidity(t, pool, schema, indexName, false)

	// With the production hook — schema-qualified so it resolves inside the
	// test schema — the leftover is dropped and the index is rebuilt.
	if preMigrationHooks[version] == nil {
		t.Fatalf("production hook is not registered for %s", version)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM "+pgx.Identifier{schema, "schema_migrations"}.Sanitize()+" WHERE version = $1", version); err != nil {
		t.Fatalf("reset recorded version: %v", err)
	}
	opts.Hooks = map[string]preMigrationHook{
		version: cleanupInvalidConcurrentIndexHook(qualifiedIndex),
	}
	if err := runMigrations(ctx, pool, opts); err != nil {
		t.Fatalf("retry migration with invalid-index cleanup: %v", err)
	}
	assertIndexValidity(t, pool, schema, indexName, true)
}
