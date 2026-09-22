package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestTaskHistoryIndexesPreserveDataAndRollback(t *testing.T) {
	admin := openTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	schema := fmt.Sprintf("history_indexes_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE") })
	pool := openTestPoolWithSearchPath(t, schema)
	for _, sql := range []string{
		"CREATE TABLE agent_task_queue (id uuid PRIMARY KEY, agent_id uuid, issue_id uuid, created_at timestamptz NOT NULL)",
		"CREATE TABLE task_message (id uuid PRIMARY KEY, task_id uuid, seq int NOT NULL)",
		`INSERT INTO agent_task_queue SELECT gen_random_uuid(), '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000002', '2026-01-01'::timestamptz + make_interval(secs=>n) FROM generate_series(1,5000) n`,
		`INSERT INTO task_message SELECT gen_random_uuid(), '00000000-0000-0000-0000-000000000003', n/2 FROM generate_series(1,5000) n`,
	} {
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	versions := []string{"536_agent_task_history", "537_issue_task_history", "538_task_message_history"}
	opts := runOptions{Direction: "up", Files: realMigrationFiles(t, versions, "up"), SchemaMigrationsTable: schema + ".schema_migrations", AdvisoryLockKey: time.Now().UnixNano(), Hooks: hooksForDirection("up")}
	for range 2 {
		if err := runMigrations(ctx, pool, opts); err != nil {
			t.Fatal(err)
		}
	}
	indexes := []string{"idx_agent_task_queue_agent_history", "idx_agent_task_queue_issue_history", "idx_task_message_history"}
	shapes := []string{"USING btree (agent_id, created_at DESC, id DESC)", "USING btree (issue_id, created_at DESC, id DESC)", "USING btree (task_id, seq, id)"}
	for i, name := range indexes {
		assertIndexValidity(t, pool, schema, name, true)
		assertMigrationIndexShape(t, pool, schema, name, shapes[i], "")
	}
	if _, err := pool.Exec(ctx, "ANALYZE agent_task_queue; ANALYZE task_message"); err != nil {
		t.Fatal(err)
	}
	for i, scope := range []string{"agent", "issue"} {
		query := fmt.Sprintf(`SELECT id FROM agent_task_queue WHERE %s_id='00000000-0000-0000-0000-00000000000%d' AND (created_at,id) < ('2026-01-01T00:10:00Z','ffffffff-ffff-ffff-ffff-ffffffffffff') ORDER BY created_at DESC,id DESC LIMIT 32`, scope, i+1)
		assertPlanUsesIndex(t, pool, query, indexes[i])
	}
	assertPlanUsesIndex(t, pool, `SELECT id FROM task_message WHERE task_id='00000000-0000-0000-0000-000000000003' AND (seq,id) > (2400,'00000000-0000-0000-0000-000000000000') ORDER BY seq,id LIMIT 32`, indexes[2])
	reversed := []string{versions[2], versions[1], versions[0]}
	opts.Direction = "down"
	opts.Files = realMigrationFiles(t, reversed, "down")
	opts.Hooks = hooksForDirection("down")
	if err := runMigrations(ctx, pool, opts); err != nil {
		t.Fatal(err)
	}
	for _, name := range indexes {
		assertIndexExists(t, pool, schema, name, false)
	}
	for _, table := range []string{"agent_task_queue", "task_message"} {
		var n int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 5000 {
			t.Fatalf("%s lost rows on rollback: %d", table, n)
		}
	}
	opts.Direction = "up"
	opts.Files = realMigrationFiles(t, versions, "up")
	opts.Hooks = hooksForDirection("up")
	if err := runMigrations(ctx, pool, opts); err != nil {
		t.Fatal(err)
	}
	for _, name := range indexes {
		assertIndexValidity(t, pool, schema, name, true)
	}
}
