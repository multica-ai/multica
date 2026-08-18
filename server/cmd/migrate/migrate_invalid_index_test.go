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

func TestRunMigrationsRepairsDingTalkGroupRouteIndexesBeforeRetry(t *testing.T) {
	pool := openTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	tests := []struct {
		version    string
		index      string
		columns    string
		wantUnique bool
	}{
		{
			version:    "305_dingtalk_group_route_installation_conversation_unique",
			index:      "idx_dingtalk_group_route_installation_conversation",
			columns:    "installation_id, conversation_id",
			wantUnique: true,
		},
		{
			version: "306_dingtalk_group_route_workspace_index",
			index:   "idx_dingtalk_group_route_workspace",
			columns: "workspace_id",
		},
		{
			version:    "307_dingtalk_group_route_id_unique",
			index:      "idx_dingtalk_group_route_id_unique",
			columns:    "id",
			wantUnique: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			if preMigrationHooks[tt.version] == nil {
				t.Fatalf("production hook is not registered for %s", tt.version)
			}

			suffix := fmt.Sprintf("%d_%d", time.Now().UnixNano(), rand.Uint32())
			schema := "migrate_dingtalk_idx_" + suffix
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

			tableName := pgx.Identifier{schema, "dingtalk_group_route"}.Sanitize()
			indexName := pgx.Identifier{tt.index}.Sanitize()
			indexRegclass := pgx.Identifier{schema, tt.index}.Sanitize()
			if _, err := pool.Exec(ctx, "CREATE TABLE "+tableName+` (
				id TEXT NOT NULL,
				workspace_id TEXT NOT NULL,
				installation_id TEXT NOT NULL,
				conversation_id TEXT NOT NULL
			)`); err != nil {
				t.Fatalf("create DingTalk route table: %v", err)
			}
			if _, err := pool.Exec(ctx, "INSERT INTO "+tableName+` (
				id, workspace_id, installation_id, conversation_id
			) VALUES
				('route-1', 'workspace-1', 'installation-1', 'conversation-1'),
				('route-1', 'workspace-1', 'installation-1', 'conversation-1')`); err != nil {
				t.Fatalf("seed duplicate DingTalk routes: %v", err)
			}

			// Duplicate keys make the concurrent UNIQUE build fail after Postgres
			// has registered the index relation, reproducing the INVALID leftover
			// an interrupted production build leaves behind. For the workspace
			// index the retry intentionally rebuilds the intended non-unique form.
			if _, err := pool.Exec(ctx, "CREATE UNIQUE INDEX CONCURRENTLY "+indexName+" ON "+tableName+" ("+tt.columns+")"); err == nil {
				t.Fatal("failure injection unexpectedly built the index")
			}
			assertIndexReadyAndValid(t, pool, schema, tt.index, false)

			if _, err := pool.Exec(ctx, "DELETE FROM "+tableName+" WHERE ctid = (SELECT max(ctid) FROM "+tableName+")"); err != nil {
				t.Fatalf("remove duplicate route before retry: %v", err)
			}

			unique := ""
			if tt.wantUnique {
				unique = "UNIQUE "
			}
			migrationSQL := "CREATE " + unique + "INDEX CONCURRENTLY IF NOT EXISTS " + indexName +
				" ON " + tableName + " (" + tt.columns + ");\n"
			migrationPath := filepath.Join(t.TempDir(), tt.version+".up.sql")
			if err := os.WriteFile(migrationPath, []byte(migrationSQL), 0o600); err != nil {
				t.Fatalf("write retry migration: %v", err)
			}

			opts := runOptions{
				Direction:             "up",
				Files:                 []string{migrationPath},
				SchemaMigrationsTable: schema + ".schema_migrations",
				AdvisoryLockKey:       int64(rand.Uint64()&0x7fffffffffffffff) | 1,
				Hooks: map[string]preMigrationHook{
					tt.version: cleanupInvalidConcurrentIndexHook(indexRegclass),
				},
			}
			if err := runMigrations(ctx, pool, opts); err != nil {
				t.Fatalf("retry migration with invalid-index cleanup: %v", err)
			}

			assertIndexReadyAndValid(t, pool, schema, tt.index, true)
			var recorded bool
			migrationsTable := pgx.Identifier{schema, "schema_migrations"}.Sanitize()
			if err := pool.QueryRow(ctx,
				"SELECT EXISTS (SELECT 1 FROM "+migrationsTable+" WHERE version = $1)",
				tt.version,
			).Scan(&recorded); err != nil {
				t.Fatalf("read migration version: %v", err)
			}
			if !recorded {
				t.Fatal("repaired migration was not recorded")
			}

			// Reproduce the other non-transactional boundary: CREATE INDEX
			// CONCURRENTLY committed, then the process exited before recording the
			// version. A valid leftover must be a safe no-op on the next run, and
			// the retry must still record the migration.
			if _, err := pool.Exec(ctx, "DELETE FROM "+migrationsTable+" WHERE version = $1", tt.version); err != nil {
				t.Fatalf("simulate missing migration record: %v", err)
			}
			if err := runMigrations(ctx, pool, opts); err != nil {
				t.Fatalf("retry migration with valid index already present: %v", err)
			}
			assertIndexReadyAndValid(t, pool, schema, tt.index, true)
			if err := pool.QueryRow(ctx,
				"SELECT EXISTS (SELECT 1 FROM "+migrationsTable+" WHERE version = $1)",
				tt.version,
			).Scan(&recorded); err != nil {
				t.Fatalf("read retried migration version: %v", err)
			}
			if !recorded {
				t.Fatal("valid-index retry did not record the migration")
			}
		})
	}
}

// TestRunMigrationsRepairsInvalidConcurrentIndexDuringRollback proves that
// direction-specific hooks also protect CREATE INDEX CONCURRENTLY in down
// migrations. Without the hook, an interrupted rollback leaves an INVALID
// relation that IF NOT EXISTS accepts on retry, and the migration version is
// removed even though the restored index remains unusable.
func TestRunMigrationsRepairsInvalidConcurrentIndexDuringRollback(t *testing.T) {
	pool := openTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	suffix := fmt.Sprintf("%d_%d", time.Now().UnixNano(), rand.Uint32())
	schema := "migrate_down_invalid_idx_" + suffix
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
		version   = "300_drop_redundant_issue_workspace_number_index"
		indexName = "idx_issue_workspace_number"
	)
	tableName := pgx.Identifier{schema, "issue"}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE TABLE "+tableName+` (
		id BIGSERIAL PRIMARY KEY,
		workspace_id UUID NOT NULL,
		number INTEGER NOT NULL
	)`); err != nil {
		t.Fatalf("create issue table: %v", err)
	}

	qualifiedIndex := pgx.Identifier{schema, indexName}.Sanitize()
	createIndex := "CREATE INDEX CONCURRENTLY IF NOT EXISTS " + pgx.Identifier{indexName}.Sanitize() +
		" ON " + tableName + " (workspace_id, number)"

	blocker, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire blocker conn: %v", err)
	}
	blockerTx, err := blocker.Begin(ctx)
	if err != nil {
		blocker.Release()
		t.Fatalf("begin blocker tx: %v", err)
	}
	if _, err := blockerTx.Exec(ctx, "INSERT INTO "+tableName+" (workspace_id, number) VALUES (gen_random_uuid(), 1)"); err != nil {
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
	if _, err := builder.Exec(ctx, "SET statement_timeout = DEFAULT"); err != nil {
		t.Logf("reset statement_timeout: %v", err)
	}
	builder.Release()
	if buildErr == nil {
		blocker.Release()
		t.Fatal("interrupted rollback build unexpectedly succeeded")
	}
	_ = blockerTx.Rollback(ctx)
	blocker.Release()

	assertIndexValidity(t, pool, schema, indexName, false)

	migrationsTable := pgx.Identifier{schema, "schema_migrations"}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE TABLE "+migrationsTable+` (
		version TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		t.Fatalf("create migrations table: %v", err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO "+migrationsTable+" (version) VALUES ($1)", version); err != nil {
		t.Fatalf("record applied migration: %v", err)
	}

	downPath := filepath.Join(t.TempDir(), version+".down.sql")
	if err := os.WriteFile(downPath, []byte(createIndex+";\n"), 0o600); err != nil {
		t.Fatalf("write rollback migration: %v", err)
	}
	if preRollbackHooks[version] == nil {
		t.Fatalf("production rollback hook is not registered for %s", version)
	}
	opts := runOptions{
		Direction:             "down",
		Files:                 []string{downPath},
		SchemaMigrationsTable: schema + ".schema_migrations",
		AdvisoryLockKey:       int64(rand.Uint64()&0x7fffffffffffffff) | 1,
		Hooks: map[string]preMigrationHook{
			version: cleanupInvalidConcurrentIndexHook(qualifiedIndex),
		},
	}
	if err := runMigrations(ctx, pool, opts); err != nil {
		t.Fatalf("retry rollback with invalid-index cleanup: %v", err)
	}

	assertIndexValidity(t, pool, schema, indexName, true)
	var recorded bool
	if err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM "+migrationsTable+" WHERE version = $1)", version).Scan(&recorded); err != nil {
		t.Fatalf("read migration version: %v", err)
	}
	if recorded {
		t.Fatal("rolled-back migration version is still recorded")
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

// TestRunMigrationsRepairsShareLinkIndexesBeforeRetry covers the MUL-6288 batch
// (migrations 327–331) against the real migration files and the real registered
// hooks, run inside a private schema so the unqualified index names in
// production resolve through search_path.
//
// Two retry windows matter here, and the share-link chain is where both bite:
//
//  1. Interrupted build. 328 backs workspace_share_link's primary key, so an
//     INVALID leftover does not just cost a lookup — 329 cannot attach the
//     constraint at all, and the migrator stops on a half-created table.
//  2. Build committed, version never recorded. Before this batch got
//     IF NOT EXISTS, the retry failed on "already exists" every single time,
//     which wedges the release rather than degrading it.
func TestRunMigrationsRepairsShareLinkIndexesBeforeRetry(t *testing.T) {
	adminPool := openTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	suffix := fmt.Sprintf("%d_%d", time.Now().UnixNano(), rand.Uint32())
	schema := "migrate_share_link_" + suffix
	schemaIdent := pgx.Identifier{schema}.Sanitize()
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+schemaIdent); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if _, err := adminPool.Exec(cleanupCtx, "DROP SCHEMA IF EXISTS "+schemaIdent+" CASCADE"); err != nil {
			t.Logf("drop schema %s: %v", schema, err)
		}
	})

	pool := openTestPoolWithSearchPath(t, schema)
	migrationsTable := pgx.Identifier{schema, "schema_migrations"}.Sanitize()
	opts := runOptions{
		Direction:             "up",
		SchemaMigrationsTable: schema + ".schema_migrations",
		AdvisoryLockKey:       int64(rand.Uint64()&0x7fffffffffffffff) | 1,
		// The production hook map, not a test-local one: this is the
		// registration the batch was missing.
		Hooks: hooksForDirection("up"),
	}

	opts.Files = realMigrationFiles(t, []string{"327_workspace_share_link"}, "up")
	if err := runMigrations(ctx, pool, opts); err != nil {
		t.Fatalf("create share link table: %v", err)
	}

	// Duplicate ids make the concurrent UNIQUE build fail after Postgres has
	// registered the relation — the same INVALID leftover an interrupted
	// production build leaves behind.
	if _, err := pool.Exec(ctx, `INSERT INTO workspace_share_link (id, workspace_id, code, created_by) VALUES
		('00000000-0000-0000-0000-0000000000a1', gen_random_uuid(), 'code-1', gen_random_uuid()),
		('00000000-0000-0000-0000-0000000000a1', gen_random_uuid(), 'code-2', gen_random_uuid())`); err != nil {
		t.Fatalf("seed duplicate share links: %v", err)
	}
	if _, err := pool.Exec(ctx, "CREATE UNIQUE INDEX CONCURRENTLY workspace_share_link_pkey_uidx ON workspace_share_link (id)"); err == nil {
		t.Fatal("failure injection unexpectedly built the primary key index")
	}
	assertIndexReadyAndValid(t, adminPool, schema, "workspace_share_link_pkey_uidx", false)
	if _, err := pool.Exec(ctx, "DELETE FROM workspace_share_link WHERE ctid = (SELECT max(ctid) FROM workspace_share_link)"); err != nil {
		t.Fatalf("remove duplicate share link before retry: %v", err)
	}

	// The hook has to drop the leftover before 328 retries, otherwise 329 has
	// no usable index to promote.
	opts.Files = realMigrationFiles(t, []string{
		"328_workspace_share_link_id_uidx",
		"329_workspace_share_link_primary_key",
	}, "up")
	if err := runMigrations(ctx, pool, opts); err != nil {
		t.Fatalf("retry share link primary key with invalid-index cleanup: %v", err)
	}
	// ADD CONSTRAINT ... USING INDEX renames the backing index to the
	// constraint name, so the promoted index is what must be valid now.
	assertIndexReadyAndValid(t, adminPool, schema, "workspace_share_link_pkey", true)
	var hasPrimaryKey bool
	if err := adminPool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_constraint c
			JOIN pg_class t ON t.oid = c.conrelid
			JOIN pg_namespace n ON n.oid = t.relnamespace
			WHERE n.nspname = $1 AND t.relname = 'workspace_share_link' AND c.contype = 'p'
		)
	`, schema).Scan(&hasPrimaryKey); err != nil {
		t.Fatalf("check share link primary key: %v", err)
	}
	if !hasPrimaryKey {
		t.Fatal("migration 329 did not attach the primary key")
	}

	// Second window: 330 and 331 build successfully, then the runner dies
	// before recording them. The next run must be a clean no-op, not a wedge.
	versions := []string{
		"330_workspace_share_link_active_ws_uidx",
		"331_workspace_share_link_code_uidx",
	}
	opts.Files = realMigrationFiles(t, versions, "up")
	if err := runMigrations(ctx, pool, opts); err != nil {
		t.Fatalf("apply share link unique indexes: %v", err)
	}
	assertIndexReadyAndValid(t, adminPool, schema, "workspace_share_link_active_ws_uidx", true)
	assertIndexReadyAndValid(t, adminPool, schema, "workspace_share_link_code_uidx", true)

	if _, err := adminPool.Exec(ctx, "DELETE FROM "+migrationsTable+" WHERE version = ANY($1)", versions); err != nil {
		t.Fatalf("simulate missing migration records: %v", err)
	}
	if err := runMigrations(ctx, pool, opts); err != nil {
		t.Fatalf("retry share link unique indexes with valid indexes present: %v", err)
	}
	assertIndexReadyAndValid(t, adminPool, schema, "workspace_share_link_active_ws_uidx", true)
	assertIndexReadyAndValid(t, adminPool, schema, "workspace_share_link_code_uidx", true)
	for _, version := range versions {
		var recorded bool
		if err := adminPool.QueryRow(ctx,
			"SELECT EXISTS (SELECT 1 FROM "+migrationsTable+" WHERE version = $1)",
			version,
		).Scan(&recorded); err != nil {
			t.Fatalf("read retried migration version: %v", err)
		}
		if !recorded {
			t.Fatalf("%s was not recorded on retry", version)
		}
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

func assertIndexReadyAndValid(t *testing.T, pool interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, schema, index string, want bool) {
	t.Helper()
	var ready, valid bool
	if err := pool.QueryRow(context.Background(), `
		SELECT i.indisready, i.indisvalid
		FROM pg_index i
		JOIN pg_class c ON c.oid = i.indexrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relname = $2
	`, schema, index).Scan(&ready, &valid); err != nil {
		t.Fatalf("read readiness/validity for %s.%s: %v", schema, index, err)
	}
	if ready != want || valid != want {
		t.Fatalf(
			"index %s.%s readiness/validity = %v/%v, want %v/%v",
			schema,
			index,
			ready,
			valid,
			want,
			want,
		)
	}
}
