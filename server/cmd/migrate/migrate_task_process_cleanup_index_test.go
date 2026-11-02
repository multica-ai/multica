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

func TestTaskProcessCleanupIndexMigrationAndPlans(t *testing.T) {
	adminPool := openTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	schema := createScratchSchema(t, ctx, adminPool, "migrate_task_cleanup_")
	pool := openTestPoolWithSearchPath(t, schema)
	for _, statement := range []string{
		`CREATE TABLE agent_task_queue (
    id UUID PRIMARY KEY, agent_id UUID NOT NULL, runtime_id UUID NOT NULL,
    status TEXT NOT NULL, failure_reason TEXT, result JSONB,
    created_at TIMESTAMPTZ NOT NULL, completed_at TIMESTAMPTZ,
    rerun_of_task_id UUID, retry_of_task_id UUID
  )`,
		`CREATE INDEX idx_agent_task_queue_agent ON agent_task_queue (agent_id, status)`,
		`INSERT INTO agent_task_queue (id, agent_id, runtime_id, status, created_at, completed_at)
   SELECT md5('history-' || n)::uuid, md5('agent')::uuid, md5('runtime')::uuid,
     'completed', now() - interval '1 day', now() - interval '1 day'
   FROM generate_series(1, 30000) AS n`,
		`INSERT INTO agent_task_queue (id, agent_id, runtime_id, status, created_at)
   VALUES (md5('candidate')::uuid, md5('agent')::uuid, md5('runtime')::uuid, 'dispatched', now()),
     (md5('unsafe')::uuid, md5('agent')::uuid, md5('runtime')::uuid, 'completed', now())`,
		`ANALYZE agent_task_queue`,
	} {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	// Exercise the real guard predicates: a failure-only partial index misses
	// the pending-ack branch of the OR and leaves ordinary history scans intact.
	source, err := os.ReadFile(filepath.Join("..", "..", "pkg", "db", "queries", "task_cleanup.sql"))
	if err != nil {
		t.Fatal(err)
	}
	queries := make(map[string]string)
	for _, name := range []string{"TaskBlockedByProcessCleanup", "TaskWaitingForProcessCleanup"} {
		_, query, ok := strings.Cut(string(source), "-- name: "+name+" :one\n")
		if !ok {
			t.Fatalf("missing query %s", name)
		}
		lines := strings.Split(query, "\n")
		for i, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "--") {
				lines[i] = ""
			}
		}
		query = strings.Join(lines, "\n")
		query, _, ok = strings.Cut(query, ";")
		if !ok {
			t.Fatalf("missing query terminator for %s", name)
		}
		query = strings.NewReplacer("@task_id", "md5('candidate')::uuid", "@id", "md5('candidate')::uuid").Replace(query)
		queries[name] = query
		t.Logf("before index, %s:\n%s", name, explainAnalyze(t, ctx, pool, query))
	}
	const version = "594_agent_task_process_cleanup_index"
	const indexName = "idx_agent_task_queue_process_cleanup"
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
	for _, tc := range []struct {
		name, status, reason, result, age string
		blocked, waiting                  bool
	}{
		{name: "ordinary history", status: "completed", result: `{}`, age: "-1 minute"},
		{name: "cleanup failure", status: "failed", reason: "cleanup_unconfirmed", result: `{}`, age: "-1 minute", blocked: true},
		{name: "expired acknowledgement", status: "cancelled", result: `{"process_cleanup_pending":true}`, age: "-1 minute", blocked: true},
		{name: "recent acknowledgement", status: "cancelled", result: `{"process_cleanup_pending":true}`, age: "0 seconds", waiting: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status=$1, failure_reason=$2,
        result=$3::jsonb, completed_at=now()+$4::interval WHERE id=md5('unsafe')::uuid`, tc.status, tc.reason, tc.result, tc.age); err != nil {
				t.Fatal(err)
			}
			for name, query := range queries {
				var got bool
				if err := pool.QueryRow(ctx, query).Scan(&got); err != nil {
					t.Fatal(err)
				}
				want := tc.blocked
				if name == "TaskWaitingForProcessCleanup" {
					want = tc.waiting
				}
				if got != want {
					t.Fatalf("%s = %v, want %v", name, got, want)
				}
				plan := explainAnalyze(t, ctx, pool, query)
				if !strings.Contains(plan, indexName) {
					t.Fatalf("%s must use cleanup index instead of filtering ordinary history:\n%s", name, plan)
				}
				t.Logf("%s:\n%s", name, plan)
			}
		})
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
	for range 2 { // Rebuild after rollback, then replay the recorded migration.
		if err := runMigrations(ctx, pool, options); err != nil {
			t.Fatal(err)
		}
		assertIndexValidity(t, pool, schema, indexName, true)
	}
}
