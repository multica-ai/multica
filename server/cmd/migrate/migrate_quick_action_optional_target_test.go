package main

import (
	"context"
	"errors"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestQuickActionOptionalTargetRollback(t *testing.T) {
	for _, tc := range []struct {
		name       string
		visibility string
		status     string
	}{
		{name: "targeted_only"},
		{name: "public_active", visibility: "public", status: "active"},
		{name: "private_active", visibility: "private", status: "active"},
		{name: "public_archived", visibility: "public", status: "archived"},
		{name: "private_archived", visibility: "private", status: "archived"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := openTestPool(t)
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			schema := createScratchSchema(t, ctx, base, "quick_action_rollback_")
			pool := openTestPoolWithSearchPath(t, schema)
			const version = "584_quick_action_optional_target"
			lockKey := int64(rand.Uint64()&0x7fffffffffffffff) | 1
			migrate := func(direction string, versions ...string) error {
				return runMigrations(ctx, pool, runOptions{
					Direction:             direction,
					Files:                 realMigrationFiles(t, versions, direction),
					SchemaMigrationsTable: schema + ".schema_migrations",
					AdvisoryLockKey:       lockKey,
					Hooks:                 hooksForDirection(direction),
					Conditions:            conditionsForDirection(direction),
				})
			}
			if err := migrate("up", "237_quick_action", version); err != nil {
				t.Fatalf("apply quick action migrations: %v", err)
			}
			if _, err := pool.Exec(ctx, `
				INSERT INTO quick_action (
					workspace_id, name, assignee_type, assignee_id, prompt, created_by_type, created_by_id
				)
				SELECT gen_random_uuid(), target, target, gen_random_uuid(), 'Review this issue', 'member', gen_random_uuid()
				FROM (VALUES ('agent'), ('squad')) AS targets(target)
			`); err != nil {
				t.Fatalf("insert targeted actions: %v", err)
			}
			const insertTargetless = `
				INSERT INTO quick_action (
					workspace_id, name, description, assignee_type, assignee_id, prompt,
					visibility, status, use_count, last_used_at, created_by_type, created_by_id
				) VALUES (
					gen_random_uuid(), 'No target', 'Saved instructions', '', NULL, 'Summarize this issue',
					$1, $2, 7, now(), 'member', gen_random_uuid()
				)`
			hasTargetless := tc.visibility != ""
			if hasTargetless {
				if _, err := pool.Exec(ctx, insertTargetless, tc.visibility, tc.status); err != nil {
					t.Fatalf("insert targetless action: %v", err)
				}
			}
			snapshot := func() string {
				t.Helper()
				var rows string
				if err := pool.QueryRow(ctx, `
					SELECT jsonb_agg(to_jsonb(q) ORDER BY id)::text FROM quick_action q
				`).Scan(&rows); err != nil {
					t.Fatalf("snapshot quick actions: %v", err)
				}
				return rows
			}
			type schemaState struct {
				nullable   string
				constraint string
				recorded   bool
			}
			readState := func() schemaState {
				t.Helper()
				var state schemaState
				if err := pool.QueryRow(ctx, `
					SELECT is_nullable,
						(SELECT pg_get_constraintdef(oid) FROM pg_constraint
						 WHERE conrelid = 'quick_action'::regclass AND conname = 'quick_action_assignee_type_check'),
						EXISTS (SELECT 1 FROM schema_migrations WHERE version = $2)
					FROM information_schema.columns
					WHERE table_schema = $1 AND table_name = 'quick_action' AND column_name = 'assignee_id'
				`, schema, version).Scan(&state.nullable, &state.constraint, &state.recorded); err != nil {
					t.Fatalf("read schema and migration state: %v", err)
				}
				return state
			}
			beforeRows, beforeState := snapshot(), readState()
			err := migrate("down", version)
			if hasTargetless {
				var pgErr *pgconn.PgError
				if !errors.As(err, &pgErr) || !strings.Contains(pgErr.Message, "cannot roll back 584") || pgErr.Hint == "" {
					t.Errorf("rollback error = %v, want explicit refusal with recovery hint", err)
				}
				if after := snapshot(); after != beforeRows {
					t.Errorf("refused rollback changed quick action data: before %s, after %s", beforeRows, after)
				}
				if after := readState(); after != beforeState {
					t.Errorf("refused rollback changed schema or migration ledger: before %+v, after %+v", beforeState, after)
				}
				if t.Failed() {
					return
				}
				if _, err := pool.Exec(ctx, `
					UPDATE quick_action SET assignee_type = 'agent', assignee_id = gen_random_uuid()
					WHERE assignee_type = ''
				`); err != nil {
					t.Fatalf("assign targets before retrying rollback: %v", err)
				}
				beforeRows = snapshot()
				err = migrate("down", version)
			}
			if err != nil {
				t.Fatalf("rollback with only targeted actions: %v", err)
			}
			if after := snapshot(); after != beforeRows {
				t.Fatal("successful rollback changed targeted action data")
			}
			if after := readState(); after.nullable != "NO" || after.recorded {
				t.Fatalf("rollback did not restore required targets and remove migration version: %+v", after)
			}
			if _, err := pool.Exec(ctx, insertTargetless, "public", "active"); err == nil {
				t.Fatal("rolled-back schema accepted a targetless action")
			}
			if _, err := pool.Exec(ctx, `UPDATE quick_action SET assignee_type = ''`); err == nil {
				t.Fatal("rolled-back schema accepted an empty target type with a non-null target")
			}
			if err := migrate("up", version); err != nil {
				t.Fatalf("reapply optional targets: %v", err)
			}
			if after := readState(); after != beforeState {
				t.Fatalf("reapplying migration did not restore optional targets: %+v", after)
			}
			if after := snapshot(); after != beforeRows {
				t.Fatal("reapplying migration changed targeted action data")
			}
			if _, err := pool.Exec(ctx, insertTargetless, "public", "active"); err != nil {
				t.Fatalf("reapplied schema rejected a targetless action: %v", err)
			}
		})
	}
}
