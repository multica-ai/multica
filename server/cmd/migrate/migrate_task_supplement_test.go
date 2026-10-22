package main

import (
	"context"
	"math/rand/v2"
	"slices"
	"testing"
	"time"
)

func TestTaskSupplementMigrationsUpDownUpInIsolatedSchema(t *testing.T) {
	base := openTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	schema := createScratchSchema(t, ctx, base, "task_supplement_")
	pool := openTestPoolWithSearchPath(t, schema)
	if _, err := pool.Exec(ctx, `CREATE TABLE agent_task_queue (id UUID PRIMARY KEY, status TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}

	for _, direction := range []string{"up", "down", "up"} {
		versions := []string{
			"538_task_supplement",
			"539_task_supplement_request_index",
			"540_task_supplement_capability_index",
			"541_task_supplement_comment_index",
			"542_task_supplement_primary_key",
			"543_task_supplement_teardown_guard",
			"544_task_supplement_application_settlement",
			"548_task_supplement_comment_task_index",
			"549_task_supplement_comment_task_primary_key",
		}
		if direction == "down" {
			slices.Reverse(versions)
		}
		if err := runMigrations(ctx, pool, runOptions{
			Direction:             direction,
			Files:                 realMigrationFiles(t, versions, direction),
			SchemaMigrationsTable: schema + ".schema_migrations",
			AdvisoryLockKey:       int64(rand.Uint64()&0x7fffffffffffffff) | 1,
			Hooks:                 hooksForDirection(direction),
			Conditions:            conditionsForDirection(direction),
		}); err != nil {
			t.Fatalf("migrate %s: %v", direction, err)
		}
		for _, table := range []string{"task_supplement", "task_supplement_capability"} {
			var exists bool
			if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, schema+"."+table).Scan(&exists); err != nil || exists != (direction == "up") {
				t.Fatalf("after %s, table %s exists=%v: %v", direction, table, exists, err)
			}
		}
		if direction == "up" {
			var indexes int
			err := pool.QueryRow(ctx, `
				SELECT count(*) FROM pg_index i
				JOIN pg_class c ON c.oid = i.indexrelid
				JOIN pg_namespace n ON n.oid = c.relnamespace
				WHERE n.nspname = $1 AND i.indisvalid AND c.relname = ANY($2::text[])
			`, schema, []string{"task_supplement_task_request_uidx", "task_supplement_capability_task_uidx", "task_supplement_pkey"}).Scan(&indexes)
			if err != nil || indexes != 3 {
				t.Fatalf("valid supplement indexes = %d, want 3: %v", indexes, err)
			}
			var primary bool
			if err := pool.QueryRow(ctx, `SELECT indisprimary FROM pg_index WHERE indexrelid = 'task_supplement_pkey'::regclass`).Scan(&primary); err != nil || !primary {
				t.Fatalf("receipt primary key missing: %v", err)
			}
			var triggerExists bool
			if err := pool.QueryRow(ctx, `
				SELECT EXISTS (
					SELECT 1 FROM pg_trigger
					WHERE tgname = 'trg_settle_terminal_task_supplements'
					  AND NOT tgisinternal
				)
			`).Scan(&triggerExists); err != nil || triggerExists {
				t.Fatalf("terminal supplement trigger exists=%v: %v", triggerExists, err)
			}

			// A bare task update no longer has hidden cross-table effects. The
			// application owns receipt settlement in the surrounding transaction.
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			_, err = tx.Exec(ctx, `
				INSERT INTO agent_task_queue VALUES ('00000000-0000-0000-0000-000000000001', 'running');
				INSERT INTO task_supplement (task_id, workspace_id, issue_id, comment_id, author_id, client_request_id, status)
				VALUES ('00000000-0000-0000-0000-000000000001', gen_random_uuid(), gen_random_uuid(), gen_random_uuid(), gen_random_uuid(), gen_random_uuid(), 'pending');
				UPDATE agent_task_queue SET status='cancelled';`)
			var status string
			if err == nil {
				err = tx.QueryRow(ctx, `SELECT status FROM task_supplement`).Scan(&status)
			}
			_ = tx.Rollback(ctx)
			if err != nil || status != "pending" {
				t.Fatalf("task update had hidden receipt settlement: %s, %v", status, err)
			}
			// One comment may steer several runs, but never the same run twice.
			var receipts int
			tx, err = pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			_, err = tx.Exec(ctx, `
				INSERT INTO task_supplement (task_id, workspace_id, issue_id, comment_id, author_id, client_request_id, status)
				SELECT task_id, gen_random_uuid(), gen_random_uuid(), '00000000-0000-0000-0000-0000000000c1', gen_random_uuid(), gen_random_uuid(), 'pending'
				FROM (VALUES ('00000000-0000-0000-0000-00000000000a'::uuid), ('00000000-0000-0000-0000-00000000000b'::uuid)) AS runs(task_id)`)
			if err == nil {
				err = tx.QueryRow(ctx, `SELECT count(*) FROM task_supplement WHERE comment_id = '00000000-0000-0000-0000-0000000000c1'`).Scan(&receipts)
			}
			if err != nil || receipts != 2 {
				_ = tx.Rollback(ctx)
				t.Fatalf("one comment bound to two runs: %d receipts, %v", receipts, err)
			}
			_, err = tx.Exec(ctx, `
				INSERT INTO task_supplement (task_id, workspace_id, issue_id, comment_id, author_id, client_request_id, status)
				VALUES ('00000000-0000-0000-0000-00000000000a', gen_random_uuid(), gen_random_uuid(), '00000000-0000-0000-0000-0000000000c1', gen_random_uuid(), gen_random_uuid(), 'pending')`)
			_ = tx.Rollback(ctx)
			if err == nil {
				t.Fatal("the same comment was bound to the same run twice")
			}
		}
	}
}
