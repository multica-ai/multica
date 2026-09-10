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

	partialIndexes := []struct {
		version    string
		index      string
		keyColumns string
		predicate  string
	}{
		{
			version:    "438_agent_runtime_online_last_seen_index",
			index:      "idx_agent_runtime_online_last_seen",
			keyColumns: "last_seen_at",
			predicate:  "status = 'online'::text",
		},
		{
			version:    "439_agent_runtime_offline_last_seen_index",
			index:      "idx_agent_runtime_offline_last_seen",
			keyColumns: "last_seen_at, id",
			predicate:  "status = 'offline'::text",
		},
	}
	for _, partial := range partialIndexes {
		options.Files = realMigrationFiles(t, []string{partial.version}, "up")
		if err := runMigrations(ctx, pool, options); err != nil {
			t.Fatalf("apply %s: %v", partial.version, err)
		}
		assertRuntimeLastSeenPartialIndex(t, ctx, pool, schema, partial.index, partial.keyColumns, partial.predicate)
	}
	// The replacement indexes are deliberately partial: applying them must not
	// recreate migration 115's full-table index.
	assertIndexExists(t, pool, schema, "idx_agent_runtime_last_seen_at", false)

	options.Direction = "down"
	options.Hooks = hooksForDirection("down")
	for i := len(partialIndexes) - 1; i >= 0; i-- {
		partial := partialIndexes[i]
		options.Files = realMigrationFiles(t, []string{partial.version}, "down")
		if err := runMigrations(ctx, pool, options); err != nil {
			t.Fatalf("roll back %s: %v", partial.version, err)
		}
		assertIndexExists(t, pool, schema, partial.index, false)
	}
}

func assertRuntimeLastSeenPartialIndex(t *testing.T, ctx context.Context, pool *pgxpool.Pool, schema, index, keyColumns, predicate string) {
	t.Helper()
	assertIndexValidity(t, pool, schema, index, true)

	var definition, actualPredicate string
	if err := pool.QueryRow(ctx, `
		SELECT pg_get_indexdef(i.indexrelid), pg_get_expr(i.indpred, i.indrelid)
		FROM pg_index i
		WHERE i.indexrelid = $1::regclass
	`, pgx.Identifier{schema, index}.Sanitize()).Scan(&definition, &actualPredicate); err != nil {
		t.Fatalf("read %s definition: %v", index, err)
	}
	if !strings.Contains(definition, "USING btree ("+keyColumns+")") {
		t.Fatalf("%s key columns differ from query ordering: %s", index, definition)
	}
	if actualPredicate != "("+predicate+")" {
		t.Fatalf("%s predicate = %q, want %q", index, actualPredicate, "("+predicate+")")
	}
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
		last_seen_at TIMESTAMPTZ,
		status TEXT NOT NULL DEFAULT 'offline'
	)`); err != nil {
		t.Fatalf("create agent_runtime fixture: %v", err)
	}
	return schema, pool
}
