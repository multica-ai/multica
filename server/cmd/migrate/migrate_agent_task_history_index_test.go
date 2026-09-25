package main

import (
	"context"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAgentTaskHistoryIndexMigrationAndPagePlans(t *testing.T) {
	adminPool := openTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	schema := createScratchSchema(t, ctx, adminPool, "migrate_agent_history_")
	pool := openTestPoolWithSearchPath(t, schema)
	for _, statement := range []string{
		`CREATE TABLE agent_task_queue (
    id UUID PRIMARY KEY, agent_id UUID NOT NULL, created_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL, escalation_for_task_id UUID, started_at TIMESTAMPTZ,
    result TEXT
  )`,
		`INSERT INTO agent_task_queue (id, agent_id, created_at, status, result)
   SELECT md5('task-' || n)::uuid, md5('agent')::uuid,
     '2026-09-01T00:00:00Z'::timestamptz + n * interval '1 second',
     'completed', repeat('x', 128)
   FROM generate_series(1, 18165) AS n`,
		`ANALYZE agent_task_queue`,
	} {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	// Explain the actual sqlc source query, so a future predicate or ordering
	// change must still be served by this index without sorting all history.
	source, err := os.ReadFile(filepath.Join("..", "..", "pkg", "db", "queries", "agent.sql"))
	if err != nil {
		t.Fatal(err)
	}
	_, history, ok := strings.Cut(string(source), "-- name: ListAgentTasks :many\n")
	if !ok {
		t.Fatal("ListAgentTasks query missing")
	}
	history, _, ok = strings.Cut(history, ";")
	if !ok {
		t.Fatal("ListAgentTasks query terminator missing")
	}

	const version = "551_agent_task_history_page_index"
	const indexName = "idx_agent_task_queue_history_page"
	options := runOptions{
		Direction: "up", Files: realMigrationFiles(t, []string{version}, "up"),
		SchemaMigrationsTable: schema + ".schema_migrations",
		AdvisoryLockKey:       int64(rand.Uint64()&0x7fffffffffffffff) | 1,
		Hooks:                 hooksForDirection("up"),
	}
	if err := runMigrations(ctx, pool, options); err != nil {
		t.Fatal(err)
	}
	assertIndexValidity(t, pool, schema, indexName, true)
	for _, before := range []string{"'infinity'", "'2026-09-01T02:00:00Z'"} {
		query := strings.NewReplacer(
			"@agent_id", "md5('agent')::uuid",
			"@before_created_at", before,
			"@before_id", "'ffffffff-ffff-ffff-ffff-ffffffffffff'",
			"@page_limit", "201",
		).Replace(history)
		plan := explainAnalyze(t, ctx, pool, query)
		if !strings.Contains(plan, indexName) || strings.Contains(plan, "Sort") {
			t.Fatalf("page before %s must use the history index without sorting:\n%s", before, plan)
		}
		t.Logf("page before %s:\n%s", before, plan)
	}
	options.Direction = "down"
	options.Files = realMigrationFiles(t, []string{version}, "down")
	options.Hooks = hooksForDirection("down")
	if err := runMigrations(ctx, pool, options); err != nil {
		t.Fatal(err)
	}
	assertIndexExists(t, pool, schema, indexName, false)
	assertMigrationVersionRecorded(t, ctx, pool, schema, version, false)
	options.Direction = "up"
	options.Files = realMigrationFiles(t, []string{version}, "up")
	options.Hooks = hooksForDirection("up")
	if err := runMigrations(ctx, pool, options); err != nil {
		t.Fatal(err)
	}
	assertIndexValidity(t, pool, schema, indexName, true)
}
