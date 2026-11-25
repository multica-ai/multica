package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// TestTaskUsageHourlySquadMigrationsUpDownUp exercises migrations 735–742 in
// both directions against an isolated schema seeded with the pre-735
// task_usage_hourly schema, proving:
//   - UP (735–742) creates the concurrent squad indexes, switches the rollup
//     pipeline, and drops the legacy 7-column unique constraints.
//   - DOWN (742–735) merges split squad buckets back into squad_id = NULL,
//     folds pending squad dirty rows into their legacy 7-column key so the
//     restored legacy rollup still recomputes them,
//     rebuilds uq_task_usage_hourly_key and uq_task_usage_hourly_dirty_key
//     CONCURRENTLY (registered in concurrentDownIndexCleanups), attaches them
//     via ADD CONSTRAINT ... UNIQUE USING INDEX as UNIQUE NULLS NOT DISTINCT,
//     and drops the squad indexes and columns cleanly.
//   - Re-applying UP (735–742) succeeds cleanly on the rolled-back schema.
func TestTaskUsageHourlySquadMigrationsUpDownUp(t *testing.T) {
	adminPool := openTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

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

	versions := []string{
		"735_task_usage_hourly_squad_col",
		"736_task_usage_hourly_squad_uq_idx",
		"737_task_usage_hourly_dirty_squad_uq_idx",
		"738_task_usage_hourly_workspace_squad_idx",
		"739_task_usage_hourly_squad_pipeline",
		"740_task_usage_hourly_drop_legacy_key",
		"741_task_usage_hourly_dirty_drop_legacy_key",
		"742_task_usage_hourly_squad_rollback_merge",
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

	// 1b. UP remaining 740..742 (forward no-ops paired with concurrent down rebuilds).
	runDir("up", versions[5:])

	// Detail rows (no project) for two legacy 7-column buckets:
	//   - 10:00: a standalone task (500 tokens), a squad-1 task (1000) and two
	//     squad-2 tasks (1000 each), split across three squad-aware rows.
	//   - 11:00: a single squad-1 task (700), so its only pending dirty row
	//     after the delete below carries a squad_id.
	// The hourly table holds exactly those totals.
	const (
		wsID  = "00000000-0000-0000-0000-000000000001"
		rtID  = "00000000-0000-0000-0000-000000000002"
		agID  = "00000000-0000-0000-0000-000000000003"
		sq1ID = "00000000-0000-0000-0000-000000000011"
		sq2ID = "00000000-0000-0000-0000-000000000012"
		t0ID  = "00000000-0000-0000-0000-000000000020"
		t1ID  = "00000000-0000-0000-0000-000000000021"
		t2aID = "00000000-0000-0000-0000-000000000022"
		t2bID = "00000000-0000-0000-0000-000000000023"
		t3ID  = "00000000-0000-0000-0000-000000000024"
	)
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		// The production task_usage delete trigger (migrations 102/243) is not
		// part of the pre-735 seed; attach it to the 739 function here.
		{`CREATE TRIGGER trg_tu_dirty_hourly
			BEFORE DELETE ON task_usage
			FOR EACH ROW EXECUTE FUNCTION enqueue_task_usage_hourly_dirty_for_tu()`, nil},
		{`INSERT INTO agent (id, workspace_id) VALUES ($1, $2)`, []any{agID, wsID}},
		{`INSERT INTO agent_task_queue (id, agent_id, runtime_id, issue_id, squad_id) VALUES
			($1, $5, $6, NULL, NULL),
			($2, $5, $6, NULL, $7),
			($3, $5, $6, NULL, $8),
			($4, $5, $6, NULL, $8),
			($9, $5, $6, NULL, $7)`, []any{t0ID, t1ID, t2aID, t2bID, agID, rtID, sq1ID, sq2ID, t3ID}},
		{`INSERT INTO task_usage (id, task_id, provider, model, input_tokens, created_at) VALUES
			(gen_random_uuid(), $1, 'claude', 'claude-sonnet-4-6', 500,  '2026-11-23 10:15:00+00'),
			(gen_random_uuid(), $2, 'claude', 'claude-sonnet-4-6', 1000, '2026-11-23 10:20:00+00'),
			(gen_random_uuid(), $3, 'claude', 'claude-sonnet-4-6', 1000, '2026-11-23 10:25:00+00'),
			(gen_random_uuid(), $4, 'claude', 'claude-sonnet-4-6', 1000, '2026-11-23 10:30:00+00'),
			(gen_random_uuid(), $5, 'claude', 'claude-sonnet-4-6', 700,  '2026-11-23 11:10:00+00')`,
			[]any{t0ID, t1ID, t2aID, t2bID, t3ID}},
		{`INSERT INTO task_usage_hourly (
			bucket_hour, workspace_id, runtime_id, agent_id, project_id, squad_id,
			provider, model, input_tokens, cost_usd_ticks, task_count, event_count
		) VALUES
			('2026-11-23 10:00:00+00', $1, $2, $3, NULL, $4, 'claude', 'claude-sonnet-4-6', 1000, 10, 1, 1),
			('2026-11-23 10:00:00+00', $1, $2, $3, NULL, $5, 'claude', 'claude-sonnet-4-6', 2000, 20, 2, 2),
			('2026-11-23 10:00:00+00', $1, $2, $3, NULL, NULL, 'claude', 'claude-sonnet-4-6', 500, 5, 1, 1),
			('2026-11-23 11:00:00+00', $1, $2, $3, NULL, $4, 'claude', 'claude-sonnet-4-6', 700, 7, 1, 1)`,
			[]any{wsID, rtID, agID, sq1ID, sq2ID}},
		// Older pending dirty rows on the 10:00 7-column key: a squad-2 row
		// (deduped into the fold) and a standalone row (ON CONFLICT target).
		{`INSERT INTO task_usage_hourly_dirty (
			bucket_hour, workspace_id, runtime_id, agent_id, project_id, squad_id, provider, model, enqueued_at
		) VALUES
			('2026-11-23 10:00:00+00', $1, $2, $3, NULL, $4,   'claude', 'claude-sonnet-4-6', now() - interval '2 hours'),
			('2026-11-23 10:00:00+00', $1, $2, $3, NULL, NULL, 'claude', 'claude-sonnet-4-6', now() - interval '1 hour')`,
			[]any{wsID, rtID, agID, sq2ID}},
		// Delete the squad-1 usage while the rollup worker is paused: the
		// trigger enqueues squad-1 dirty rows for 10:00 and 11:00. For 11:00
		// that is the only signal the bucket is stale, since a deleted row can
		// never surface through the updated_at watermark.
		{`DELETE FROM task_usage WHERE task_id IN ($1, $2)`, []any{t1ID, t3ID}},
	} {
		if _, err := pool.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatalf("seed squad usage fixture: %v\n%s", err, stmt.sql)
		}
	}

	var squadDirtyRows int
	var wantDirtyEnqueuedAt time.Time
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE squad_id = $1), MAX(enqueued_at)
		  FROM task_usage_hourly_dirty
	`, sq1ID).Scan(&squadDirtyRows, &wantDirtyEnqueuedAt); err != nil {
		t.Fatalf("query pre-down dirty rows: %v", err)
	}
	if squadDirtyRows != 2 {
		t.Fatalf("task_usage delete enqueued %d squad-1 dirty rows, want 2", squadDirtyRows)
	}

	workspaceTotals := func() map[string][2]int64 {
		t.Helper()
		rows, err := pool.Query(ctx, `
			SELECT workspace_id::text, SUM(input_tokens)::bigint, SUM(cost_usd_ticks)::bigint
			  FROM task_usage_hourly
			 GROUP BY workspace_id
		`)
		if err != nil {
			t.Fatalf("query workspace totals: %v", err)
		}
		defer rows.Close()
		out := map[string][2]int64{}
		for rows.Next() {
			var ws string
			var tokens, cost int64
			if err := rows.Scan(&ws, &tokens, &cost); err != nil {
				t.Fatalf("scan workspace totals: %v", err)
			}
			out[ws] = [2]int64{tokens, cost}
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("iterate workspace totals: %v", err)
		}
		return out
	}
	totalsBeforeDown := workspaceTotals()

	// 2. DOWN: 742..735
	runDir("down", reversed)

	// The 10:00 split rows must have merged into one row totaling 3500 tokens
	// & 4 tasks, with per-workspace token / cost totals unchanged by the merge.
	var rowCount int
	var sumTokens, sumTasks int64
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*), COALESCE(SUM(input_tokens), 0), COALESCE(SUM(task_count), 0)
		  FROM task_usage_hourly
		 WHERE bucket_hour = '2026-11-23 10:00:00+00'
	`).Scan(&rowCount, &sumTokens, &sumTasks); err != nil {
		t.Fatalf("query merged hourly rows: %v", err)
	}
	if rowCount != 1 || sumTokens != 3500 || sumTasks != 4 {
		t.Fatalf("post-down merged hourly = rows:%d tokens:%d tasks:%d, want 1/3500/4", rowCount, sumTokens, sumTasks)
	}
	if got := workspaceTotals(); fmt.Sprint(got) != fmt.Sprint(totalsBeforeDown) {
		t.Fatalf("workspace totals changed by 742 down merge: before %v, after %v", totalsBeforeDown, got)
	}

	// Pending squad dirty rows must survive as one legacy 7-column row per
	// bucket that keeps the latest enqueued_at.
	var dirtyCount int
	for _, bucket := range []string{"2026-11-23 10:00:00+00", "2026-11-23 11:00:00+00"} {
		var gotDirtyEnqueuedAt time.Time
		if err := pool.QueryRow(ctx, `
			SELECT COUNT(*), COALESCE(MAX(enqueued_at), 'epoch')
			  FROM task_usage_hourly_dirty
			 WHERE bucket_hour = $1
			   AND workspace_id = $2 AND runtime_id = $3 AND agent_id = $4
			   AND project_id IS NULL
			   AND provider = 'claude' AND model = 'claude-sonnet-4-6'
		`, bucket, wsID, rtID, agID).Scan(&dirtyCount, &gotDirtyEnqueuedAt); err != nil {
			t.Fatalf("query post-down dirty rows for %s: %v", bucket, err)
		}
		if dirtyCount != 1 || !gotDirtyEnqueuedAt.Equal(wantDirtyEnqueuedAt) {
			t.Fatalf("post-down dirty rows for %s = %d (enqueued_at %v), want 1 (enqueued_at %v)",
				bucket, dirtyCount, gotDirtyEnqueuedAt, wantDirtyEnqueuedAt)
		}
	}

	// The restored legacy rollup must consume the folded dirty rows and drop
	// the deleted squad-1 usage: 10:00 shrinks to the remaining tasks and
	// 11:00 disappears. The window excludes every task_usage created_at, so
	// only the dirty queue can trigger these recomputes.
	if _, err := pool.Exec(ctx, `
		SELECT rollup_task_usage_hourly_window(now() - interval '1 minute', now() + interval '1 minute')
	`); err != nil {
		t.Fatalf("run legacy rollup after down: %v", err)
	}
	var recomputedTokens, recomputedTasks, recomputedEvents int64
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*), COALESCE(SUM(input_tokens), 0), COALESCE(SUM(task_count), 0), COALESCE(SUM(event_count), 0)
		  FROM task_usage_hourly
		 WHERE bucket_hour = '2026-11-23 10:00:00+00'
	`).Scan(&rowCount, &recomputedTokens, &recomputedTasks, &recomputedEvents); err != nil {
		t.Fatalf("query recomputed hourly rows: %v", err)
	}
	if rowCount != 1 || recomputedTokens != 2500 || recomputedTasks != 3 || recomputedEvents != 3 {
		t.Fatalf("post-rollup hourly = rows:%d tokens:%d tasks:%d events:%d, want 1/2500/3/3",
			rowCount, recomputedTokens, recomputedTasks, recomputedEvents)
	}
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM task_usage_hourly WHERE bucket_hour = '2026-11-23 11:00:00+00'
	`).Scan(&rowCount); err != nil {
		t.Fatalf("query recomputed 11:00 hourly rows: %v", err)
	}
	if rowCount != 0 {
		t.Fatalf("post-rollup 11:00 hourly rows = %d, want 0 (its only usage was deleted)", rowCount)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM task_usage_hourly_dirty`).Scan(&dirtyCount); err != nil {
		t.Fatalf("query drained dirty rows: %v", err)
	}
	if dirtyCount != 0 {
		t.Fatalf("legacy rollup left %d dirty rows, want 0", dirtyCount)
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

	// 3. Re-UP: 735..742 must succeed cleanly on the rolled-back schema.
	runDir("up", versions)
	assertIndexValidity(t, pool, schema, "uq_task_usage_hourly_squad_key_idx", true)
	assertIndexValidity(t, pool, schema, "uq_task_usage_hourly_dirty_squad_key_idx", true)
	assertIndexValidity(t, pool, schema, "idx_task_usage_hourly_workspace_squad_time", true)
}
