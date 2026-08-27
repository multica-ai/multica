package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAgentRuntimeLastSeenAtIndexRetirement(t *testing.T) {
	adminPool := openTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	schema, pool := createAgentRuntimeLastSeenAtIndexFixture(t, ctx, adminPool)
	options := runOptions{
		Direction:             "up",
		SchemaMigrationsTable: schema + ".schema_migrations",
		AdvisoryLockKey:       int64(rand.Uint64()&0x7fffffffffffffff) | 1,
		Hooks:                 hooksForDirection("up"),
	}

	options.Files = realMigrationFiles(t, []string{"115_agent_runtime_last_seen_at_index"}, "up")
	if err := runMigrations(ctx, pool, options); err != nil {
		t.Fatalf("apply historical runtime index migration: %v", err)
	}
	assertIndexValidity(t, pool, schema, "idx_agent_runtime_last_seen_at", true)

	const version = "437_drop_agent_runtime_last_seen_at_index"
	options.Files = realMigrationFiles(t, []string{version}, "up")
	if err := runMigrations(ctx, pool, options); err != nil {
		t.Fatalf("apply runtime index retirement migration: %v", err)
	}
	assertIndexExists(t, pool, schema, "idx_agent_runtime_last_seen_at", false)
	assertMigrationVersionRecorded(t, ctx, pool, schema, version, true)

	options.Direction = "down"
	options.Files = realMigrationFiles(t, []string{version}, "down")
	options.Hooks = hooksForDirection("down")
	if err := runMigrations(ctx, pool, options); err != nil {
		t.Fatalf("roll back runtime index retirement migration: %v", err)
	}
	assertIndexValidity(t, pool, schema, "idx_agent_runtime_last_seen_at", true)
	var indexDefinition string
	if err := pool.QueryRow(ctx,
		"SELECT pg_get_indexdef($1::regclass)",
		pgx.Identifier{schema, "idx_agent_runtime_last_seen_at"}.Sanitize(),
	).Scan(&indexDefinition); err != nil {
		t.Fatalf("read restored runtime index definition: %v", err)
	}
	if !strings.HasSuffix(indexDefinition, "USING btree (last_seen_at)") {
		t.Fatalf("restored runtime index definition differs from migration 115: %s", indexDefinition)
	}
	assertMigrationVersionRecorded(t, ctx, pool, schema, version, false)

	options.Direction = "up"
	options.Files = realMigrationFiles(t, []string{version}, "up")
	options.Hooks = hooksForDirection("up")
	if err := runMigrations(ctx, pool, options); err != nil {
		t.Fatalf("reapply runtime index retirement migration: %v", err)
	}
	assertIndexExists(t, pool, schema, "idx_agent_runtime_last_seen_at", false)
}

func createAgentRuntimeLastSeenAtIndexFixture(
	t *testing.T,
	ctx context.Context,
	adminPool *pgxpool.Pool,
) (string, *pgxpool.Pool) {
	t.Helper()
	suffix := fmt.Sprintf("%d_%d", time.Now().UnixNano(), rand.Uint32())
	schema := "migrate_runtime_last_seen_" + suffix
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
	if _, err := pool.Exec(ctx, `CREATE TABLE agent_runtime (
		id UUID PRIMARY KEY,
		last_seen_at TIMESTAMPTZ
	)`); err != nil {
		t.Fatalf("create agent_runtime fixture: %v", err)
	}
	return schema, pool
}
