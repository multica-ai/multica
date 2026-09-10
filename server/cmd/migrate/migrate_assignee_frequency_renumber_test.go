package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestAssigneeFrequencyRenumberPreservesExistingIndex(t *testing.T) {
	const version = "466_activity_log_member_assignee_frequency_index"
	const legacyVersion = "458_activity_log_member_assignee_frequency_index"
	const index = "idx_activity_log_member_assignee_frequency"

	for _, previouslyApplied := range []bool{false, true} {
		t.Run(fmt.Sprintf("previously_applied_%t", previouslyApplied), func(t *testing.T) {
			admin := openTestPool(t)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			schema := fmt.Sprintf("migrate_assignee_renumber_%d_%d", time.Now().UnixNano(), rand.Uint32())
			ident := pgx.Identifier{schema}.Sanitize()
			if _, err := admin.Exec(ctx, "CREATE SCHEMA "+ident); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				if _, err := admin.Exec(cleanupCtx, "DROP SCHEMA "+ident+" CASCADE"); err != nil {
					t.Errorf("cleanup schema: %v", err)
				}
			})
			pool := openTestPoolWithSearchPath(t, schema)
			if _, err := pool.Exec(ctx, `CREATE TABLE activity_log (
				workspace_id UUID, actor_id UUID, actor_type TEXT, action TEXT, details JSONB
			);
			CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ DEFAULT now())`); err != nil {
				t.Fatal(err)
			}
			files := realMigrationFiles(t, []string{version}, "up")
			var originalOID uint32
			if previouslyApplied {
				// Simulate an installation that applied the identical index SQL
				// before its migration filename was renumbered.
				sql, err := os.ReadFile(files[0])
				if err != nil {
					t.Fatal(err)
				}
				if _, err := pool.Exec(ctx, string(sql)); err != nil {
					t.Fatal(err)
				}
				if _, err := pool.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", legacyVersion); err != nil {
					t.Fatal(err)
				}
				if err := pool.QueryRow(ctx, "SELECT $1::regclass::oid", schema+"."+index).Scan(&originalOID); err != nil {
					t.Fatal(err)
				}
			}
			options := runOptions{
				Direction: "up", Files: files,
				SchemaMigrationsTable: schema + ".schema_migrations",
				AdvisoryLockKey:       int64(rand.Uint64()&0x7fffffffffffffff) | 1,
				Hooks:                 hooksForDirection("up"),
			}
			if preMigrationHooks[version] == nil {
				t.Fatal("renumbered migration lost its interrupted-index repair hook")
			}
			for range 2 {
				if err := runMigrations(ctx, pool, options); err != nil {
					t.Fatal(err)
				}
			}
			assertIndexValidity(t, pool, schema, index, true)
			var recorded bool
			if err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)", version).Scan(&recorded); err != nil || !recorded {
				t.Fatalf("new version recorded = %t, error = %v", recorded, err)
			}
			if previouslyApplied {
				var currentOID uint32
				if err := pool.QueryRow(ctx, "SELECT $1::regclass::oid", schema+"."+index).Scan(&currentOID); err != nil {
					t.Fatal(err)
				}
				if currentOID != originalOID {
					t.Fatal("upgrade rebuilt the existing valid index")
				}
				if err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)", legacyVersion).Scan(&recorded); err != nil || !recorded {
					t.Fatalf("historical version preserved = %t, error = %v", recorded, err)
				}
			}
			options.Direction = "down"
			options.Files = realMigrationFiles(t, []string{version}, "down")
			options.Hooks = hooksForDirection("down")
			if err := runMigrations(ctx, pool, options); err != nil {
				t.Fatal(err)
			}
			assertIndexExists(t, pool, schema, index, false)
			options.Direction, options.Files, options.Hooks = "up", files, hooksForDirection("up")
			if err := runMigrations(ctx, pool, options); err != nil {
				t.Fatal(err)
			}
			assertIndexValidity(t, pool, schema, index, true)
		})
	}
}
