package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// TestTaskUsageHourlyAutopilotMigrationsUpDownUp exercises migrations 757–763
// against an isolated schema seeded with the post-742 task_usage_hourly
// schema, proving:
//   - UP builds the 9-column autopilot unique indexes, drops the 8-column
//     squad ones, and the rollup splits a bucket by autopilot: a task's
//     autopilot run wins, then the autopilot that created its issue.
//   - Deleting an autopilot run (which nulls the task's autopilot_run_id after
//     the run row is gone) moves its usage back to manual on the next rollup
//     without leaving the old autopilot bucket behind.
//   - DOWN merges split autopilot buckets into autopilot_id = NULL, rebuilds
//     the 8-column squad unique indexes CONCURRENTLY, and drops the column.
//   - Re-applying UP succeeds on the rolled-back schema.
func TestTaskUsageHourlyAutopilotMigrationsUpDownUp(t *testing.T) {
	adminPool := openTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	suffix := fmt.Sprintf("%d_%d", time.Now().UnixNano(), rand.Uint32())
	schema := "migrate_task_usage_autopilot_" + suffix
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
	for _, stmt := range []string{
		`CREATE TABLE schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE FUNCTION task_usage_hour_bucket(ts TIMESTAMPTZ) RETURNS TIMESTAMPTZ LANGUAGE SQL IMMUTABLE AS $$
			SELECT date_trunc('hour', ts AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'
		$$`,
		`CREATE TABLE agent (
			id UUID PRIMARY KEY,
			workspace_id UUID NOT NULL
		)`,
		`CREATE TABLE issue (
			id UUID PRIMARY KEY,
			workspace_id UUID NOT NULL,
			project_id UUID,
			origin_type TEXT,
			origin_id UUID
		)`,
		`CREATE TABLE autopilot_run (
			id UUID PRIMARY KEY,
			autopilot_id UUID NOT NULL
		)`,
		`CREATE TABLE agent_task_queue (
			id UUID PRIMARY KEY,
			agent_id UUID NOT NULL,
			runtime_id UUID,
			issue_id UUID,
			squad_id UUID,
			usage_squad_id UUID,
			autopilot_run_id UUID REFERENCES autopilot_run(id) ON DELETE SET NULL
		)`,
		`CREATE TABLE task_usage (
			id UUID PRIMARY KEY,
			task_id UUID NOT NULL,
			provider TEXT NOT NULL,
			model TEXT NOT NULL,
			input_tokens BIGINT NOT NULL DEFAULT 0,
			output_tokens BIGINT NOT NULL DEFAULT 0,
			cache_read_tokens BIGINT NOT NULL DEFAULT 0,
			cache_write_tokens BIGINT NOT NULL DEFAULT 0,
			cost_usd_ticks BIGINT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ
		)`,
		`CREATE TABLE task_usage_hourly (
			bucket_hour TIMESTAMPTZ NOT NULL,
			workspace_id UUID NOT NULL,
			runtime_id UUID NOT NULL,
			agent_id UUID NOT NULL,
			project_id UUID,
			squad_id UUID,
			provider TEXT NOT NULL,
			model TEXT NOT NULL,
			input_tokens BIGINT NOT NULL DEFAULT 0,
			output_tokens BIGINT NOT NULL DEFAULT 0,
			cache_read_tokens BIGINT NOT NULL DEFAULT 0,
			cache_write_tokens BIGINT NOT NULL DEFAULT 0,
			cost_usd_ticks BIGINT NOT NULL DEFAULT 0,
			uncosted_input_tokens BIGINT,
			uncosted_output_tokens BIGINT,
			uncosted_cache_read_tokens BIGINT,
			uncosted_cache_write_tokens BIGINT,
			task_count BIGINT NOT NULL DEFAULT 0,
			event_count BIGINT NOT NULL DEFAULT 0,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE task_usage_hourly_dirty (
			bucket_hour TIMESTAMPTZ NOT NULL,
			workspace_id UUID NOT NULL,
			runtime_id UUID NOT NULL,
			agent_id UUID NOT NULL,
			project_id UUID,
			squad_id UUID,
			provider TEXT NOT NULL,
			model TEXT NOT NULL,
			enqueued_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE UNIQUE INDEX uq_task_usage_hourly_squad_key_idx
			ON task_usage_hourly (bucket_hour, workspace_id, runtime_id, agent_id, project_id, squad_id, provider, model)
			NULLS NOT DISTINCT`,
		`CREATE UNIQUE INDEX uq_task_usage_hourly_dirty_squad_key_idx
			ON task_usage_hourly_dirty (bucket_hour, workspace_id, runtime_id, agent_id, project_id, squad_id, provider, model)
			NULLS NOT DISTINCT`,
	} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatalf("seed post-742 schema: %v", err)
		}
	}

	versions := []string{
		"757_task_usage_hourly_autopilot_col",
		"758_task_usage_hourly_autopilot_uq_idx",
		"759_task_usage_hourly_dirty_autopilot_uq_idx",
		"760_task_usage_hourly_autopilot_pipeline",
		"761_task_usage_hourly_drop_squad_key",
		"762_task_usage_hourly_dirty_drop_squad_key",
		"763_task_usage_hourly_autopilot_rollback_merge",
	}
	reversed := make([]string, len(versions))
	for i := range versions {
		reversed[i] = versions[len(versions)-1-i]
	}
	runDir := func(dir string, list []string) {
		t.Helper()
		if err := runMigrations(ctx, pool, runOptions{
			Direction:             dir,
			Files:                 realMigrationFiles(t, list, dir),
			SchemaMigrationsTable: schema + ".schema_migrations",
			AdvisoryLockKey:       int64(rand.Uint64()&0x7fffffffffffffff) | 1,
			Hooks:                 hooksForDirection(dir),
		}); err != nil {
			t.Fatalf("run %s migrations: %v", dir, err)
		}
	}

	// 1. UP through 760: the 8-column squad indexes are gone the moment the
	// pipeline switches, so 9-column ON CONFLICT never meets them.
	runDir("up", versions[:4])
	assertIndexValidity(t, pool, schema, "uq_task_usage_hourly_autopilot_key_idx", true)
	assertIndexValidity(t, pool, schema, "uq_task_usage_hourly_dirty_autopilot_key_idx", true)
	assertIndexExists(t, pool, schema, "uq_task_usage_hourly_squad_key_idx", false)
	assertIndexExists(t, pool, schema, "uq_task_usage_hourly_dirty_squad_key_idx", false)
	runDir("up", versions[4:])

	const (
		wsID        = "00000000-0000-0000-0000-000000000001"
		rtID        = "00000000-0000-0000-0000-000000000002"
		agID        = "00000000-0000-0000-0000-000000000003"
		runOnlyAP   = "00000000-0000-0000-0000-0000000000a1"
		createIssAP = "00000000-0000-0000-0000-0000000000a2"
		runID       = "00000000-0000-0000-0000-0000000000b1"
		apIssueID   = "00000000-0000-0000-0000-0000000000c1"
		plainIssue  = "00000000-0000-0000-0000-0000000000c2"
	)
	for _, stmt := range []string{
		`INSERT INTO agent (id, workspace_id) VALUES ('` + agID + `', '` + wsID + `')`,
		`INSERT INTO autopilot_run (id, autopilot_id) VALUES ('` + runID + `', '` + runOnlyAP + `')`,
		`INSERT INTO issue (id, workspace_id, origin_type, origin_id) VALUES
			('` + apIssueID + `', '` + wsID + `', 'autopilot', '` + createIssAP + `'),
			('` + plainIssue + `', '` + wsID + `', NULL, NULL)`,
		// Run-only autopilot task, a task on an autopilot-created issue, and a manual one.
		`INSERT INTO agent_task_queue (id, agent_id, runtime_id, issue_id, autopilot_run_id) VALUES
			('00000000-0000-0000-0000-0000000000d1', '` + agID + `', '` + rtID + `', NULL, '` + runID + `'),
			('00000000-0000-0000-0000-0000000000d2', '` + agID + `', '` + rtID + `', '` + apIssueID + `', NULL),
			('00000000-0000-0000-0000-0000000000d3', '` + agID + `', '` + rtID + `', '` + plainIssue + `', NULL)`,
		`INSERT INTO task_usage (id, task_id, provider, model, input_tokens, created_at) VALUES
			(gen_random_uuid(), '00000000-0000-0000-0000-0000000000d1', 'claude', 'm', 100, '2026-11-23 10:05:00+00'),
			(gen_random_uuid(), '00000000-0000-0000-0000-0000000000d2', 'claude', 'm', 20,  '2026-11-23 10:10:00+00'),
			(gen_random_uuid(), '00000000-0000-0000-0000-0000000000d3', 'claude', 'm', 3,   '2026-11-23 10:15:00+00')`,
	} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatalf("seed usage: %v", err)
		}
	}
	rollup := func() {
		t.Helper()
		if _, err := pool.Exec(ctx, `SELECT rollup_task_usage_hourly_window('2026-11-23 00:00:00+00', now() + interval '1 hour')`); err != nil {
			t.Fatalf("rollup: %v", err)
		}
	}
	bySource := func() map[string]int64 {
		t.Helper()
		rows, err := pool.Query(ctx, `SELECT COALESCE(autopilot_id::text, 'manual'), SUM(input_tokens) FROM task_usage_hourly GROUP BY 1`)
		if err != nil {
			t.Fatalf("query hourly: %v", err)
		}
		defer rows.Close()
		out := map[string]int64{}
		for rows.Next() {
			var k string
			var v int64
			if err := rows.Scan(&k, &v); err != nil {
				t.Fatal(err)
			}
			out[k] = v
		}
		return out
	}

	rollup()
	if got := bySource(); got[runOnlyAP] != 100 || got[createIssAP] != 20 || got["manual"] != 3 || len(got) != 3 {
		t.Fatalf("split by autopilot = %v, want run-only 100, create-issue 20, manual 3", got)
	}

	// Deleting the run nulls the task's autopilot_run_id after the run is gone.
	if _, err := pool.Exec(ctx, `DELETE FROM autopilot_run WHERE id = $1`, runID); err != nil {
		t.Fatalf("delete run: %v", err)
	}
	rollup()
	if got := bySource(); got[runOnlyAP] != 0 || got[createIssAP] != 20 || got["manual"] != 103 {
		t.Fatalf("after run delete = %v, want create-issue 20, manual 103 and no run-only bucket", got)
	}

	// Leave a split bucket and a pending autopilot dirty key for 763 down.
	if _, err := pool.Exec(ctx, `
		INSERT INTO task_usage_hourly_dirty (bucket_hour, workspace_id, runtime_id, agent_id, project_id, squad_id, autopilot_id, provider, model)
		VALUES ('2026-11-23 10:00:00+00', $1, $2, $3, NULL, NULL, $4, 'claude', 'm')
	`, wsID, rtID, agID, createIssAP); err != nil {
		t.Fatalf("seed dirty: %v", err)
	}

	// 2. DOWN: split buckets merge into one manual row per 8-column key.
	runDir("down", reversed)
	var rowCount int
	var sumTokens, sumTasks int64
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*), COALESCE(SUM(input_tokens), 0), COALESCE(SUM(task_count), 0) FROM task_usage_hourly
	`).Scan(&rowCount, &sumTokens, &sumTasks); err != nil {
		t.Fatalf("query merged rows: %v", err)
	}
	if rowCount != 1 || sumTokens != 123 || sumTasks != 3 {
		t.Fatalf("post-down hourly = rows:%d tokens:%d tasks:%d, want 1/123/3", rowCount, sumTokens, sumTasks)
	}
	assertIndexValidity(t, pool, schema, "uq_task_usage_hourly_squad_key_idx", true)
	assertIndexValidity(t, pool, schema, "uq_task_usage_hourly_dirty_squad_key_idx", true)
	assertIndexExists(t, pool, schema, "uq_task_usage_hourly_autopilot_key_idx", false)
	assertIndexExists(t, pool, schema, "uq_task_usage_hourly_dirty_autopilot_key_idx", false)

	// The squad-only pipeline works again.
	rollup()

	// 3. Re-UP succeeds on the rolled-back schema.
	runDir("up", versions)
	assertIndexValidity(t, pool, schema, "uq_task_usage_hourly_autopilot_key_idx", true)
	assertIndexExists(t, pool, schema, "uq_task_usage_hourly_squad_key_idx", false)
}
