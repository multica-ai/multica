package main

import (
	"context"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIssueDescriptionSearchIndexesRetirement(t *testing.T) {
	adminPool := openTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	if _, err := adminPool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS pg_trgm"); err != nil {
		t.Fatalf("install pg_trgm test dependency: %v", err)
	}
	pgBigmUsable := installExtensionIfAvailable(t, ctx, adminPool, "pg_bigm")
	schema := createScratchSchema(t, ctx, adminPool, "migrate_issue_description_indexes_retirement_")
	pool := openTestPoolWithSearchPath(t, schema+", public")
	applyFallback, reason, err := upMigrationConditions["139_issue_description_trgm_index"](ctx, nil)
	if err != nil {
		t.Fatalf("evaluate retired fallback gate: %v", err)
	}
	if applyFallback || reason == "" {
		t.Fatalf("historical fallback gate = (%v, %q), want a documented skip", applyFallback, reason)
	}

	for _, statement := range []string{
		`CREATE TABLE schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE issue (
			id BIGSERIAL PRIMARY KEY,
			description TEXT
		)`,
	} {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("apply fixture statement: %v", err)
		}
	}

	if pgBigmUsable {
		if _, err := pool.Exec(ctx, `CREATE INDEX idx_issue_description_bigm
			ON issue USING gin (LOWER(COALESCE(description, '')) gin_bigm_ops)`); err != nil {
			t.Fatalf("create pg_bigm fixture index: %v", err)
		}
	} else {
		// The up migration retires any same-named index defensively. A B-tree
		// stand-in keeps that path covered when CI does not provide pg_bigm.
		if _, err := pool.Exec(ctx, `CREATE INDEX idx_issue_description_bigm
			ON issue (LOWER(COALESCE(description, '')))`); err != nil {
			t.Fatalf("create stand-in fixture index: %v", err)
		}
	}
	if _, err := pool.Exec(ctx, `CREATE INDEX idx_issue_description_trgm
		ON issue USING gin (LOWER(COALESCE(description, '')) gin_trgm_ops)`); err != nil {
		t.Fatalf("create pg_trgm fixture index: %v", err)
	}

	versions := []string{
		"461_drop_issue_description_bigm_index",
		"462_drop_issue_description_trgm_index",
	}
	options := runOptions{
		Direction:             "up",
		Files:                 realMigrationFiles(t, versions, "up"),
		SchemaMigrationsTable: schema + ".schema_migrations",
		AdvisoryLockKey:       int64(rand.Uint64()&0x7fffffffffffffff) | 1,
		Hooks:                 hooksForDirection("up"),
		Conditions:            conditionsForDirection("up"),
	}
	if err := runMigrations(ctx, pool, options); err != nil {
		t.Fatalf("apply issue description index retirement: %v", err)
	}
	assertIndexExists(t, pool, schema, "idx_issue_description_bigm", false)
	assertIndexExists(t, pool, schema, "idx_issue_description_trgm", false)
	for _, version := range versions {
		assertMigrationVersionRecorded(t, ctx, pool, schema, version, true)
	}

	if err := runMigrations(ctx, pool, options); err != nil {
		t.Fatalf("repeat issue description index retirement: %v", err)
	}
	assertIndexExists(t, pool, schema, "idx_issue_description_bigm", false)
	assertIndexExists(t, pool, schema, "idx_issue_description_trgm", false)

	options.Direction = "down"
	options.Files = realMigrationFiles(t, reversed(versions), "down")
	options.Hooks = hooksForDirection("down")
	options.Conditions = conditionsForDirection("down")
	if err := runMigrations(ctx, pool, options); err != nil {
		t.Fatalf("roll back issue description index retirement: %v", err)
	}
	assertIndexExists(t, pool, schema, "idx_issue_description_bigm", pgBigmUsable)
	assertIndexExists(t, pool, schema, "idx_issue_description_trgm", true)
	if pgBigmUsable {
		assertUsableIssueDescriptionIndex(t, ctx, pool, schema, "idx_issue_description_bigm", "gin_bigm_ops", "pg_bigm")
	}
	assertUsableIssueDescriptionIndex(t, ctx, pool, schema, "idx_issue_description_trgm", "gin_trgm_ops", "pg_trgm")
	for _, version := range versions {
		assertMigrationVersionRecorded(t, ctx, pool, schema, version, false)
	}
}

func assertUsableIssueDescriptionIndex(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	schema string,
	indexName string,
	opclass string,
	extension string,
) {
	t.Helper()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}
	defer conn.Release()

	usable, err := indexIsUsable(ctx, conn, usableIndexRequirement{
		IndexRegclass: schema + "." + indexName,
		TableRegclass: schema + ".issue",
		AccessMethod:  "gin",
		OperatorClass: opclass,
		Expression:    "lower(COALESCE(description, ''::text))",
		Extension:     extension,
	})
	if err != nil {
		t.Fatalf("inspect %s: %v", indexName, err)
	}
	if !usable {
		t.Fatalf("restored index %s does not match the historical search index shape", indexName)
	}
}
