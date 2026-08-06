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
