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

// TestTaskUsageHourlySquadMigrationsUpDownUp exercises migrations 735–742 and 755 in
// both directions against an isolated schema seeded with the pre-735
// task_usage_hourly schema, proving:
//   - UP (735–742) creates the concurrent squad indexes, switches the rollup
//     pipeline, and drops the legacy 7-column unique constraints.
//   - DOWN (742–735) merges split squad buckets back into squad_id = NULL,
//     rebuilds uq_task_usage_hourly_key and uq_task_usage_hourly_dirty_key
//     CONCURRENTLY (registered in concurrentDownIndexCleanups), attaches them
//     via ADD CONSTRAINT ... UNIQUE USING INDEX as UNIQUE NULLS NOT DISTINCT,
//     and drops the squad indexes and columns cleanly.
//   - Re-applying UP (735–742, 755) succeeds cleanly on the rolled-back schema.
func TestTaskUsageHourlySquadMigrationsUpDownUp(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool, schema := newPre735TaskUsageSchema(ctx, t)

	versions := []string{
		"735_task_usage_hourly_squad_col",
		"736_task_usage_hourly_squad_uq_idx",
		"737_task_usage_hourly_dirty_squad_uq_idx",
		"738_task_usage_hourly_workspace_squad_idx",
		"739_task_usage_hourly_squad_pipeline",
		"740_task_usage_hourly_drop_legacy_key",
		"741_task_usage_hourly_dirty_drop_legacy_key",
		"742_task_usage_hourly_squad_rollback_merge",
		"755_task_usage_hourly_rollup_hour_key",
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

	// 1a. UP through 739: legacy 7-column constraints must ALREADY be dropped
	// atomically in 739 up (before 740/741 run) so there is zero intermediate
	// window where 8-column ON CONFLICT statements collide with legacy constraints.
	runDir("up", versions[:5])
	assertIndexValidity(t, pool, schema, "uq_task_usage_hourly_squad_key_idx", true)
	assertIndexValidity(t, pool, schema, "uq_task_usage_hourly_dirty_squad_key_idx", true)
	assertIndexValidity(t, pool, schema, "idx_task_usage_hourly_workspace_squad_time", true)
	assertIndexExists(t, pool, schema, "uq_task_usage_hourly_key", false)
	assertIndexExists(t, pool, schema, "uq_task_usage_hourly_dirty_key", false)

	// 1b. UP remaining 740..742 (forward no-ops paired with concurrent down
	// rebuilds) and 755.
	runDir("up", versions[5:])

	// Insert two split squad rows + one standalone row sharing the same legacy
	// 7-column key, plus a pending squad dirty row, to verify 742 down merges
	// them cleanly before 741/740 down rebuild legacy unique indexes CONCURRENTLY.
	const (
		wsID  = "00000000-0000-0000-0000-000000000001"
		rtID  = "00000000-0000-0000-0000-000000000002"
		agID  = "00000000-0000-0000-0000-000000000003"
		sq1ID = "00000000-0000-0000-0000-000000000011"
		sq2ID = "00000000-0000-0000-0000-000000000012"
	)
	if _, err := pool.Exec(ctx, `
		INSERT INTO task_usage_hourly (
			bucket_hour, workspace_id, runtime_id, agent_id, project_id, squad_id,
			provider, model, input_tokens, task_count, event_count
		) VALUES
			('2026-11-23 10:00:00+00', $1, $2, $3, NULL, $4, 'claude', 'claude-sonnet-4-6', 1000, 1, 1),
			('2026-11-23 10:00:00+00', $1, $2, $3, NULL, $5, 'claude', 'claude-sonnet-4-6', 2000, 2, 2),
			('2026-11-23 10:00:00+00', $1, $2, $3, NULL, NULL, 'claude', 'claude-sonnet-4-6', 500, 1, 1)
	`, wsID, rtID, agID, sq1ID, sq2ID); err != nil {
		t.Fatalf("seed split hourly rows: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO task_usage_hourly_dirty (
			bucket_hour, workspace_id, runtime_id, agent_id, project_id, squad_id, provider, model
		) VALUES
			('2026-11-23 10:00:00+00', $1, $2, $3, NULL, $4, 'claude', 'claude-sonnet-4-6')
	`, wsID, rtID, agID, sq1ID); err != nil {
		t.Fatalf("seed squad dirty row: %v", err)
	}

	// 2. DOWN: 755, 742..735
	runDir("down", reversed)

	// Split rows must have merged into one row totaling 3500 tokens & 4 tasks.
	var rowCount int
	var sumTokens, sumTasks int64
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*), COALESCE(SUM(input_tokens), 0), COALESCE(SUM(task_count), 0)
		  FROM task_usage_hourly
	`).Scan(&rowCount, &sumTokens, &sumTasks); err != nil {
		t.Fatalf("query merged hourly rows: %v", err)
	}
	if rowCount != 1 || sumTokens != 3500 || sumTasks != 4 {
		t.Fatalf("post-down merged hourly = rows:%d tokens:%d tasks:%d, want 1/3500/4", rowCount, sumTokens, sumTasks)
	}

	// Legacy UNIQUE NULLS NOT DISTINCT constraints must be restored with valid backing indexes.
	for _, pair := range [][2]string{
		{"task_usage_hourly", "uq_task_usage_hourly_key"},
		{"task_usage_hourly_dirty", "uq_task_usage_hourly_dirty_key"},
	} {
		assertIndexValidity(t, pool, schema, pair[1], true)
		var conDef string
		if err := pool.QueryRow(ctx, `
			SELECT pg_get_constraintdef(c.oid)
			  FROM pg_constraint c
			  JOIN pg_class r ON r.oid = c.conrelid
			  JOIN pg_namespace n ON n.oid = r.relnamespace
			 WHERE n.nspname = $1 AND r.relname = $2 AND c.conname = $3
		`, schema, pair[0], pair[1]).Scan(&conDef); err != nil {
			t.Fatalf("legacy constraint %s.%s missing after down: %v", pair[0], pair[1], err)
		}
		if !strings.Contains(conDef, "UNIQUE NULLS NOT DISTINCT") {
			t.Fatalf("constraint %s.%s def = %q, want UNIQUE NULLS NOT DISTINCT", pair[0], pair[1], conDef)
		}
	}

	// Squad indexes must be gone.
	assertIndexExists(t, pool, schema, "uq_task_usage_hourly_squad_key_idx", false)
	assertIndexExists(t, pool, schema, "uq_task_usage_hourly_dirty_squad_key_idx", false)
	assertIndexExists(t, pool, schema, "idx_task_usage_hourly_workspace_squad_time", false)

	// 3. Re-UP: 735..742 and 755 must succeed cleanly on the rolled-back schema.
	runDir("up", versions)
	assertIndexValidity(t, pool, schema, "uq_task_usage_hourly_squad_key_idx", true)
	assertIndexValidity(t, pool, schema, "uq_task_usage_hourly_dirty_squad_key_idx", true)
	assertIndexValidity(t, pool, schema, "idx_task_usage_hourly_workspace_squad_time", true)
}

// newPre735TaskUsageSchema creates an isolated schema holding the tables the
// task_usage_hourly squad migrations read, in their pre-735 shape, and returns
// a pool whose search_path points at it.
func newPre735TaskUsageSchema(ctx context.Context, t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	adminPool := openTestPool(t)
	suffix := fmt.Sprintf("%d_%d", time.Now().UnixNano(), rand.Uint32())
	schema := "migrate_task_usage_squad_" + suffix
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
			project_id UUID
		)`,
		`CREATE TABLE agent_task_queue (
			id UUID PRIMARY KEY,
			agent_id UUID NOT NULL,
			runtime_id UUID,
			issue_id UUID,
			squad_id UUID
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
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			CONSTRAINT uq_task_usage_hourly_key
				UNIQUE NULLS NOT DISTINCT
				(bucket_hour, workspace_id, runtime_id, agent_id, project_id, provider, model)
		)`,
		`CREATE TABLE task_usage_hourly_dirty (
			bucket_hour TIMESTAMPTZ NOT NULL,
			workspace_id UUID NOT NULL,
			runtime_id UUID NOT NULL,
			agent_id UUID NOT NULL,
			project_id UUID,
			provider TEXT NOT NULL,
			model TEXT NOT NULL,
			enqueued_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			CONSTRAINT uq_task_usage_hourly_dirty_key
				UNIQUE NULLS NOT DISTINCT
				(bucket_hour, workspace_id, runtime_id, agent_id, project_id, provider, model)
		)`,
	} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatalf("seed pre-735 schema: %v", err)
		}
	}
	return pool, schema
}
