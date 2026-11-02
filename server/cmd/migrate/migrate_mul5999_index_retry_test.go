package main

import (
	"bytes"
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/sync/errgroup"
)

var concurrentIndexNamePattern = regexp.MustCompile(
	`(?i)CREATE\s+(?:UNIQUE\s+)?INDEX\s+CONCURRENTLY\s+(?:IF\s+NOT\s+EXISTS\s+)?([a-z0-9_]+)`)

var pgBigmConcurrentIndexPattern = regexp.MustCompile(
	`(?is)CREATE\s+(?:UNIQUE\s+)?INDEX\s+CONCURRENTLY\b[^;]*\bgin_bigm_ops\b`)

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

// migrationSQL is what the audits below read out of one migration file, with
// its comment lines dropped first: the name of every index it builds
// concurrently, in file order, and whether one of those builds uses pg_bigm.
type migrationSQL struct {
	builds     []string
	buildsBigm bool
}

// migrationCorpus is every migration file, keyed by file name
// ("273_agent_task_queue_runtime_id_index.up.sql"), read and parsed once per
// package run. Four tests audit the directory; each used to read and strip all
// of it on its own, and on a slow filesystem that was most of this package's
// time. The files are read concurrently for the same reason: there are about a
// thousand, and on a bind-mounted checkout every read is a round trip.
var migrationCorpus = sync.OnceValues(func() (map[string]migrationSQL, error) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.sql"))
	if err != nil {
		return nil, err
	}
	parsed := make([]migrationSQL, len(paths))
	var g errgroup.Group
	g.SetLimit(16)
	for i, path := range paths {
		g.Go(func() error {
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			body := stripSQLLineComments(raw)
			var builds []string
			for _, match := range concurrentIndexNamePattern.FindAllSubmatch(body, -1) {
				builds = append(builds, string(match[1]))
			}
			parsed[i] = migrationSQL{
				builds:     builds,
				buildsBigm: pgBigmConcurrentIndexPattern.Match(body),
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	corpus := make(map[string]migrationSQL, len(paths))
	for i, path := range paths {
		corpus[filepath.Base(path)] = parsed[i]
	}
	return corpus, nil
})

// migrationsInDirection returns the corpus and, sorted, the names of its files
// for one direction.
func migrationsInDirection(t *testing.T, direction string) (map[string]migrationSQL, []string) {
	t.Helper()
	corpus, err := migrationCorpus()
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	suffix := "." + direction + ".sql"
	var names []string
	for name := range corpus {
		if strings.HasSuffix(name, suffix) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return corpus, names
}

// TestConcurrentIndexCleanupsMatchTheirMigrations guards the mapping that wires
// invalid-index cleanup hooks to migrations. A hook that names an index no
// migration creates is a silent no-op: the retry then treats the INVALID
// leftover as success and the index stays unusable. Nothing at runtime would
// report that, so the names are checked against the migration files here.
func TestConcurrentIndexCleanupsMatchTheirMigrations(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	assertEveryConcurrentBuildHasCleanup(t, "down", concurrentDownIndexCleanups)
}

// TestEveryConcurrentUpBuildHasCleanup is the up-direction counterpart, added
// for MUL-6288. Only the down direction was covered before, so registration for
// up migrations was effectively opt-in: 316, 317, 326, 328, 330 and 331 all
// shipped without a hook and nothing failed. An unregistered build is invisible
// until a real interrupted migration turns into either a permanently INVALID
// index recorded as success (`IF NOT EXISTS`) or a wedged migrator (bare
// `CREATE`), so the check belongs here rather than in review.
func TestEveryConcurrentUpBuildHasCleanup(t *testing.T) {
	t.Parallel()
	assertEveryConcurrentBuildHasCleanup(t, "up", concurrentIndexCleanups)
}

// pg_bigm is optional, so every rollback that names its operator class in a
// concurrent build must be gated. Otherwise a pg_bigm-less self-hosted database
// can fail during startup merely because an operator class is unavailable.
func TestEveryPGBigmConcurrentDownBuildHasCondition(t *testing.T) {
	t.Parallel()
	corpus, names := migrationsInDirection(t, "down")
	for _, name := range names {
		if !corpus[name].buildsBigm {
			continue
		}
		version := strings.TrimSuffix(name, ".down.sql")
		if downMigrationConditions[version] == nil {
			t.Errorf("%s: builds a pg_bigm index concurrently on down but has no down condition", version)
		}
	}
}

func assertEveryConcurrentBuildHasCleanup(t *testing.T, direction string, cleanups map[string]string) {
	t.Helper()
	suffix := "." + direction + ".sql"
	corpus, names := migrationsInDirection(t, direction)
	if len(names) == 0 {
		t.Fatalf("no %s migrations found", direction)
	}

	for _, name := range names {
		builds := corpus[name].builds
		if len(builds) == 0 {
			continue
		}
		version := strings.TrimSuffix(name, suffix)
		if len(builds) != 1 {
			t.Errorf("%s: has %d concurrent index builds; cleanup registration supports exactly one", version, len(builds))
			continue
		}
		indexName := builds[0]
		registered, ok := cleanups[version]
		if !ok {
			t.Errorf("%s: builds %q concurrently on %s but has no %s cleanup", version, indexName, direction, direction)
			continue
		}
		if registered != indexName {
			t.Errorf("%s: %s cleanup registers %q, migration builds %q", version, direction, registered, indexName)
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
	corpus, _ := migrationsInDirection(t, direction)
	for version, indexName := range cleanups {
		migration, ok := corpus[version+"."+direction+".sql"]
		if !ok {
			t.Errorf("%s: has a cleanup hook but no %s migration file", version, direction)
			continue
		}
		// The comment headers on these migrations mention CREATE INDEX
		// CONCURRENTLY in prose, so the corpus matches statements only.
		if len(migration.builds) == 0 {
			t.Errorf("%s: has a cleanup hook but builds no index concurrently", version)
			continue
		}
		if got := migration.builds[0]; got != indexName {
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
	t.Parallel()
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
