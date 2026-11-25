package main

import (
	"context"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// 758-760 create task_code_change without foreign keys (teardown is
// application-owned) and build its two teardown indexes concurrently, one per
// file. Up, down and up again must all apply cleanly.
func TestTaskCodeChangeMigrationsUpDown(t *testing.T) {
	adminPool := openTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	schema := createScratchSchema(t, ctx, adminPool, "migrate_task_code_change_")
	pool := openTestPoolWithSearchPath(t, schema)
	versions := []string{"758_task_code_change", "759_task_code_change_workspace_idx", "760_task_code_change_issue_idx"}
	reversed := []string{versions[2], versions[1], versions[0]}
	options := runOptions{
		SchemaMigrationsTable: schema + ".schema_migrations",
		AdvisoryLockKey:       int64(rand.Uint64()&0x7fffffffffffffff) | 1,
	}
	migrate := func(direction string, files []string) {
		t.Helper()
		options.Direction = direction
		options.Files = realMigrationFiles(t, files, direction)
		options.Hooks = hooksForDirection(direction)
		if err := runMigrations(ctx, pool, options); err != nil {
			t.Fatalf("migrate %s: %v", direction, err)
		}
	}
	table := pgx.Identifier{schema, "task_code_change"}.Sanitize()

	for round := range 2 {
		migrate("up", versions)
		assertIndexValidity(t, pool, schema, "idx_task_code_change_workspace", true)
		assertIndexValidity(t, pool, schema, "idx_task_code_change_issue", true)
		var foreignKeys int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM pg_constraint WHERE conrelid = $1::regclass AND contype = 'f'`, table,
		).Scan(&foreignKeys); err != nil {
			t.Fatal(err)
		}
		if foreignKeys != 0 {
			t.Fatalf("round %d: task_code_change has %d foreign keys, want none", round, foreignKeys)
		}
		// No parent rows exist in this schema: without an FK the insert stands
		// on its own, and the defaults fill an empty, complete snapshot.
		var repos string
		var truncated bool
		if err := pool.QueryRow(ctx, `INSERT INTO `+table+` (task_id, workspace_id)
			VALUES ('00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000002')
			RETURNING repos::text, repos_truncated`).Scan(&repos, &truncated); err != nil {
			t.Fatalf("round %d: insert: %v", round, err)
		}
		if repos != "[]" || truncated {
			t.Fatalf("round %d: defaults = %s, %v", round, repos, truncated)
		}

		migrate("down", reversed)
		assertIndexExists(t, pool, schema, "idx_task_code_change_workspace", false)
		assertIndexExists(t, pool, schema, "idx_task_code_change_issue", false)
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Fatalf("round %d: task_code_change survived the down migration", round)
		}
		for _, version := range versions {
			assertMigrationVersionRecorded(t, ctx, pool, schema, version, false)
		}
	}
}
