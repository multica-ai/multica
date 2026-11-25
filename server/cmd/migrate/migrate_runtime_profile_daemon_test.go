package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"testing"
	"time"
)

func TestRuntimeProfileDaemonRenumberPreservesPreviewBindings(t *testing.T) {
	const version = "674_runtime_profile_daemon"
	const legacyVersion = "665_runtime_profile_daemon"
	for _, previouslyApplied := range []bool{false, true} {
		t.Run(fmt.Sprintf("previously_applied_%t", previouslyApplied), func(t *testing.T) {
			base := openTestPool(t)
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			schema := createScratchSchema(t, ctx, base, "runtime_profile_daemon_")
			pool := openTestPoolWithSearchPath(t, schema)
			if _, err := pool.Exec(ctx, `
				CREATE TABLE runtime_profile (id TEXT PRIMARY KEY, display_name TEXT NOT NULL);
				INSERT INTO runtime_profile VALUES ('legacy', 'Keep this profile');
				CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ DEFAULT now())
			`); err != nil {
				t.Fatal(err)
			}
			if previouslyApplied {
				if _, err := pool.Exec(ctx, `
					ALTER TABLE runtime_profile ADD COLUMN daemon_id TEXT;
					UPDATE runtime_profile SET daemon_id = 'studio-mac' WHERE id = 'legacy'
				`); err != nil {
					t.Fatal(err)
				}
				if _, err := pool.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", legacyVersion); err != nil {
					t.Fatal(err)
				}
			}
			options := runOptions{
				Direction: "up", Files: realMigrationFiles(t, []string{version}, "up"),
				SchemaMigrationsTable: schema + ".schema_migrations",
				AdvisoryLockKey:       int64(rand.Uint64()&0x7fffffffffffffff) | 1,
				Hooks:                 hooksForDirection("up"),
			}
			for range 2 {
				if err := runMigrations(ctx, pool, options); err != nil {
					t.Fatalf("apply renumbered migration: %v", err)
				}
			}
			var name string
			var daemonID *string
			if err := pool.QueryRow(ctx, "SELECT display_name, daemon_id FROM runtime_profile WHERE id = 'legacy'").Scan(&name, &daemonID); err != nil {
				t.Fatal(err)
			}
			if name != "Keep this profile" || (daemonID != nil) != previouslyApplied {
				t.Fatalf("profile changed: name=%q daemon_id=%v", name, daemonID)
			}
			if daemonID != nil && *daemonID != "studio-mac" {
				t.Fatalf("preview machine binding changed: %q", *daemonID)
			}
			for _, expected := range []struct {
				version string
				present bool
			}{{version, true}, {legacyVersion, previouslyApplied}} {
				var recorded bool
				if err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)", expected.version).Scan(&recorded); err != nil || recorded != expected.present {
					t.Fatalf("migration %s recorded=%v, want %v: %v", expected.version, recorded, expected.present, err)
				}
			}
			options.Direction, options.Files = "down", realMigrationFiles(t, []string{version}, "down")
			options.Hooks = hooksForDirection("down")
			if err := runMigrations(ctx, pool, options); err != nil {
				t.Fatalf("rollback: %v", err)
			}
			var columns int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns
				WHERE table_schema = $1 AND table_name = 'runtime_profile' AND column_name = 'daemon_id'`, schema).Scan(&columns); err != nil || columns != 0 {
				t.Fatalf("daemon_id columns after rollback=%d: %v", columns, err)
			}
			options.Direction, options.Files = "up", realMigrationFiles(t, []string{version}, "up")
			options.Hooks = hooksForDirection("up")
			if err := runMigrations(ctx, pool, options); err != nil {
				t.Fatalf("reapply after rollback: %v", err)
			}
			if err := pool.QueryRow(ctx, "SELECT display_name, daemon_id FROM runtime_profile WHERE id = 'legacy'").Scan(&name, &daemonID); err != nil || name != "Keep this profile" || daemonID != nil {
				t.Fatalf("profile after reapply: name=%q daemon_id=%v: %v", name, daemonID, err)
			}
		})
	}
}
