package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIssueLastActivityIndexRetirement(t *testing.T) {
	adminPool := openTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	t.Run("fresh install builds then retires historical index", func(t *testing.T) {
		schema, pool := createIssueLastActivityIndexFixture(t, ctx, adminPool)
		migrationsTable := schema + ".schema_migrations"
		options := runOptions{
			Direction:             "up",
			SchemaMigrationsTable: migrationsTable,
			AdvisoryLockKey:       int64(rand.Uint64()&0x7fffffffffffffff) | 1,
			Hooks:                 hooksForDirection("up"),
			Conditions:            conditionsForDirection("up"),
		}

		options.Files = realMigrationFiles(t, []string{"361_issue_last_activity_index"}, "up")
		if err := runMigrations(ctx, pool, options); err != nil {
			t.Fatalf("apply historical index migration: %v", err)
		}
		assertIndexValidity(t, pool, schema, "idx_issue_workspace_last_activity", true)
		assertMigrationVersionRecorded(t, ctx, pool, schema, "361_issue_last_activity_index", true)

		options.Files = realMigrationFiles(t, []string{"375_drop_issue_last_activity_index"}, "up")
		if err := runMigrations(ctx, pool, options); err != nil {
			t.Fatalf("apply index retirement migration: %v", err)
		}
		assertIndexExists(t, pool, schema, "idx_issue_workspace_last_activity", false)
		assertMigrationVersionRecorded(t, ctx, pool, schema, "375_drop_issue_last_activity_index", true)

		if err := runMigrations(ctx, pool, options); err != nil {
			t.Fatalf("repeat applied index retirement migration: %v", err)
		}
		assertIndexExists(t, pool, schema, "idx_issue_workspace_last_activity", false)
	})

	t.Run("existing deployment drops index and rollback restores it", func(t *testing.T) {
		schema, pool := createIssueLastActivityIndexFixture(t, ctx, adminPool)
		migrationsTable := schema + ".schema_migrations"
		options := runOptions{
			Direction:             "up",
			SchemaMigrationsTable: migrationsTable,
			AdvisoryLockKey:       int64(rand.Uint64()&0x7fffffffffffffff) | 1,
			Hooks:                 hooksForDirection("up"),
		}

		options.Files = realMigrationFiles(t, []string{"361_issue_last_activity_index"}, "up")
		if err := runMigrations(ctx, pool, options); err != nil {
			t.Fatalf("apply existing index migration: %v", err)
		}
		assertIndexValidity(t, pool, schema, "idx_issue_workspace_last_activity", true)

		options.Files = realMigrationFiles(t, []string{"375_drop_issue_last_activity_index"}, "up")
		options.Conditions = conditionsForDirection("up")
		if err := runMigrations(ctx, pool, options); err != nil {
			t.Fatalf("apply index retirement migration: %v", err)
		}
		assertIndexExists(t, pool, schema, "idx_issue_workspace_last_activity", false)

		options.Direction = "down"
		options.Files = realMigrationFiles(t, []string{"375_drop_issue_last_activity_index"}, "down")
		options.Hooks = hooksForDirection("down")
		options.Conditions = conditionsForDirection("down")
		if err := runMigrations(ctx, pool, options); err != nil {
			t.Fatalf("roll back index retirement migration: %v", err)
		}
		assertIndexValidity(t, pool, schema, "idx_issue_workspace_last_activity", true)
		assertMigrationVersionRecorded(t, ctx, pool, schema, "375_drop_issue_last_activity_index", false)
	})
}

func createIssueLastActivityIndexFixture(
	t *testing.T,
	ctx context.Context,
	adminPool *pgxpool.Pool,
) (string, *pgxpool.Pool) {
	t.Helper()
	suffix := fmt.Sprintf("%d_%d", time.Now().UnixNano(), rand.Uint32())
	schema := "migrate_issue_activity_" + suffix
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
	if _, err := pool.Exec(ctx, `CREATE TABLE issue (
		id UUID PRIMARY KEY,
		workspace_id UUID NOT NULL,
		last_activity_at TIMESTAMPTZ
	)`); err != nil {
		t.Fatalf("create issue fixture: %v", err)
	}
	return schema, pool
}
