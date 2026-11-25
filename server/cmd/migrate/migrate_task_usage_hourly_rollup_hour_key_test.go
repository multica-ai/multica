package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"testing"
	"time"
)

// TestTaskUsageHourlyRollupConservesLegacyHourTotals upgrades a pre-735 hour
// whose single hourly row combines standalone and squad usage, re-dirties one
// run's usage, and recomputes: the rollup must still total the raw usage and
// split it per squad, whichever side was touched.
func TestTaskUsageHourlyRollupConservesLegacyHourTotals(t *testing.T) {
	const (
		squadA = "00000000-0000-0000-0000-00000000000a"
		squadB = "00000000-0000-0000-0000-00000000000b"
	)
	type run struct {
		squadID string // "" for a standalone run
		tokens  int64
	}
	for _, tc := range []struct {
		name    string
		runs    []run
		touched int
	}{
		{"standalone touched", []run{{"", 10}, {squadA, 20}}, 0},
		{"squad touched", []run{{"", 10}, {squadA, 20}}, 1},
		{"standalone touched with two squads", []run{{"", 10}, {squadA, 20}, {squadB, 40}}, 0},
		{"one of two squads touched", []run{{"", 10}, {squadA, 20}, {squadB, 40}}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			pool, schema := newPre735TaskUsageSchema(ctx, t)

			const (
				wsID    = "00000000-0000-0000-0000-000000000001"
				rtID    = "00000000-0000-0000-0000-000000000002"
				agentID = "00000000-0000-0000-0000-000000000003"
			)
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, err := pool.Exec(ctx, sql, args...); err != nil {
					t.Fatalf("exec %q: %v", sql, err)
				}
			}

			// Pre-735 state: every run's usage sits in one hourly row without
			// a squad dimension.
			exec(`INSERT INTO agent (id, workspace_id) VALUES ($1, $2)`, agentID, wsID)
			var rawTotal int64
			want := map[string]int64{}
			usageIDs := make([]string, len(tc.runs))
			for i, r := range tc.runs {
				taskID := fmt.Sprintf("00000000-0000-0000-0001-%012d", i)
				usageIDs[i] = fmt.Sprintf("00000000-0000-0000-0002-%012d", i)
				var squad any
				if r.squadID != "" {
					squad = r.squadID
				}
				exec(`INSERT INTO agent_task_queue (id, agent_id, runtime_id, squad_id) VALUES ($1, $2, $3, $4)`,
					taskID, agentID, rtID, squad)
				exec(`INSERT INTO task_usage (id, task_id, provider, model, input_tokens, created_at, updated_at)
					VALUES ($1, $2, 'claude', 'claude-sonnet-4-6', $3, '2026-11-21 11:30+00', '2026-11-21 11:30+00')`,
					usageIDs[i], taskID, r.tokens)
				rawTotal += r.tokens
				want[r.squadID] = r.tokens
			}
			exec(`INSERT INTO task_usage_hourly (
					bucket_hour, workspace_id, runtime_id, agent_id, project_id,
					provider, model, input_tokens, task_count, event_count
				) VALUES ('2026-11-21 11:00+00', $1, $2, $3, NULL, 'claude', 'claude-sonnet-4-6', $4, $5, $5)`,
				wsID, rtID, agentID, rawTotal, len(tc.runs))

			if err := runMigrations(ctx, pool, runOptions{
				Direction: "up",
				Files: realMigrationFiles(t, []string{
					"735_task_usage_hourly_squad_col",
					"736_task_usage_hourly_squad_uq_idx",
					"737_task_usage_hourly_dirty_squad_uq_idx",
					"738_task_usage_hourly_workspace_squad_idx",
					"739_task_usage_hourly_squad_pipeline",
					"740_task_usage_hourly_drop_legacy_key",
					"741_task_usage_hourly_dirty_drop_legacy_key",
					"742_task_usage_hourly_squad_rollback_merge",
					"755_task_usage_hourly_rollup_hour_key",
				}, "up"),
				SchemaMigrationsTable: schema + ".schema_migrations",
				AdvisoryLockKey:       int64(rand.Uint64()&0x7fffffffffffffff) | 1,
				Hooks:                 hooksForDirection("up"),
			}); err != nil {
				t.Fatalf("run up migrations: %v", err)
			}

			exec(`UPDATE task_usage SET updated_at = '2026-11-21 12:01+00' WHERE id = $1`, usageIDs[tc.touched])
			exec(`SELECT rollup_task_usage_hourly_window('2026-11-21 12:00+00', '2026-11-21 12:02+00')`)

			rows, err := pool.Query(ctx, `
				SELECT COALESCE(squad_id::text, ''), input_tokens, task_count, event_count
				  FROM task_usage_hourly
				 WHERE bucket_hour = '2026-11-21 11:00+00'`)
			if err != nil {
				t.Fatalf("query hourly rollup: %v", err)
			}
			got := map[string]int64{}
			var gotTotal int64
			for rows.Next() {
				var squad string
				var tokens, tasks, events int64
				if err := rows.Scan(&squad, &tokens, &tasks, &events); err != nil {
					t.Fatalf("scan hourly row: %v", err)
				}
				if tasks != 1 || events != 1 {
					t.Errorf("bucket %q task/event count = %d/%d, want 1/1", squad, tasks, events)
				}
				got[squad] = tokens
				gotTotal += tokens
			}
			if err := rows.Err(); err != nil {
				t.Fatalf("iterate hourly rows: %v", err)
			}
			if gotTotal != rawTotal {
				t.Fatalf("hourly rollup total = %d, want raw usage total %d (buckets %v)", gotTotal, rawTotal, got)
			}
			if len(got) != len(want) {
				t.Fatalf("hourly buckets = %v, want %v", got, want)
			}
			for squad, tokens := range want {
				if got[squad] != tokens {
					t.Fatalf("hourly buckets = %v, want %v", got, want)
				}
			}
		})
	}
}
