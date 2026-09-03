package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestDelegatedFailurePendingIndexRetirement(t *testing.T) {
	adminPool := openTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	suffix := fmt.Sprintf("%d_%d", time.Now().UnixNano(), rand.Uint32())
	schema := "migrate_delegated_failure_idx_" + suffix
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
	for _, statement := range []string{
		`CREATE TABLE schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE comment (
			id UUID PRIMARY KEY,
			created_at TIMESTAMPTZ NOT NULL,
			author_type TEXT NOT NULL,
			type TEXT NOT NULL,
			source_task_id UUID
		)`,
	} {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("apply fixture statement: %v", err)
		}
	}

	options := runOptions{
		Direction:             "up",
		SchemaMigrationsTable: schema + ".schema_migrations",
		AdvisoryLockKey:       int64(rand.Uint64()&0x7fffffffffffffff) | 1,
		Hooks:                 hooksForDirection("up"),
	}
	options.Files = realMigrationFiles(t, []string{
		"343_comment_delegated_failure_pending_index",
		"444_comment_recovery_settled_at",
	}, "up")
	if err := runMigrations(ctx, pool, options); err != nil {
		t.Fatalf("apply historical delegated-failure migrations: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO comment (
			id, created_at, author_type, type, source_task_id, recovery_settled_at
		)
		SELECT
			gen_random_uuid(),
			now() - make_interval(secs => n),
			'system',
			'progress_update',
			gen_random_uuid(),
			CASE WHEN n % 10 = 0 THEN NULL ELSE now() END
		FROM generate_series(1, 5000) AS n
	`); err != nil {
		t.Fatalf("seed delegated-failure comments: %v", err)
	}

	options.Files = realMigrationFiles(t, []string{
		"445_comment_delegated_failure_unsettled_index",
	}, "up")
	if err := runMigrations(ctx, pool, options); err != nil {
		t.Fatalf("apply replacement delegated-failure index migration: %v", err)
	}
	assertIndexValidity(t, pool, schema, "idx_comment_delegated_failure_pending", true)
	assertIndexValidity(t, pool, schema, "idx_comment_delegated_failure_unsettled", true)

	const version = "450_drop_comment_delegated_failure_pending_index"
	options.Files = realMigrationFiles(t, []string{version}, "up")
	if err := runMigrations(ctx, pool, options); err != nil {
		t.Fatalf("apply delegated-failure index retirement migration: %v", err)
	}
	assertIndexExists(t, pool, schema, "idx_comment_delegated_failure_pending", false)
	assertIndexValidity(t, pool, schema, "idx_comment_delegated_failure_unsettled", true)
	assertMigrationVersionRecorded(t, ctx, pool, schema, version, true)
	assertPlanUsesIndex(t, pool, `
		SELECT id
		FROM comment
		WHERE author_type = 'system'
		  AND type = 'progress_update'
		  AND source_task_id IS NOT NULL
		  AND recovery_settled_at IS NULL
		ORDER BY created_at, id
		LIMIT 100
	`, "idx_comment_delegated_failure_unsettled")

	options.Direction = "down"
	options.Files = realMigrationFiles(t, []string{version}, "down")
	options.Hooks = hooksForDirection("down")
	if err := runMigrations(ctx, pool, options); err != nil {
		t.Fatalf("roll back delegated-failure index retirement migration: %v", err)
	}
	assertIndexValidity(t, pool, schema, "idx_comment_delegated_failure_pending", true)
	assertIndexValidity(t, pool, schema, "idx_comment_delegated_failure_unsettled", true)
	assertMigrationVersionRecorded(t, ctx, pool, schema, version, false)
}
