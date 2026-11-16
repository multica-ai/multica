package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestAutopilotScheduleLimitRenumber(t *testing.T) {
	const version = "660_autopilot_schedule_limit"
	for _, legacyVersion := range []string{"", "600_autopilot_schedule_limit", "615_autopilot_schedule_limit"} {
		t.Run("legacy_"+legacyVersion, func(t *testing.T) {
			admin := openTestPool(t)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			schema := fmt.Sprintf("migrate_schedule_limit_%d_%d", time.Now().UnixNano(), rand.Uint32())
			ident := pgx.Identifier{schema}.Sanitize()
			if _, err := admin.Exec(ctx, "CREATE SCHEMA "+ident); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := admin.Exec(context.Background(), "DROP SCHEMA "+ident+" CASCADE"); err != nil {
					t.Errorf("cleanup: %v", err)
				}
			})
			pool := openTestPoolWithSearchPath(t, schema)
			if _, err := pool.Exec(ctx, `CREATE TABLE autopilot_trigger (kind TEXT NOT NULL);
				INSERT INTO autopilot_trigger VALUES ('schedule'), ('webhook');
				CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ DEFAULT now())`); err != nil {
				t.Fatal(err)
			}
			files := realMigrationFiles(t, []string{version}, "up")
			if legacyVersion != "" {
				// The old migration's schema, with a finite schedule already exhausted.
				if _, err := pool.Exec(ctx, `ALTER TABLE autopilot_trigger
					ADD COLUMN execution_limit integer,
					ADD COLUMN execution_count bigint NOT NULL DEFAULT 0,
					ADD CONSTRAINT autopilot_trigger_execution_limit_check CHECK
						(execution_limit IS NULL OR (kind = 'schedule' AND execution_limit > 0)),
					ADD CONSTRAINT autopilot_trigger_execution_count_check CHECK (execution_count >= 0);
					UPDATE autopilot_trigger SET execution_limit=3, execution_count=3 WHERE kind='schedule'`); err != nil {
					t.Fatal(err)
				}
				if _, err := pool.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", legacyVersion); err != nil {
					t.Fatal(err)
				}
				if _, err := pool.Exec(ctx, `CREATE TABLE attachment (width integer, height integer);
					INSERT INTO attachment VALUES (640, 480);
					INSERT INTO schema_migrations (version) VALUES ('600_wecom_install_session'), ('615_attachment_image_dimensions')`); err != nil {
					t.Fatal(err)
				}
				// A pending earlier migration must not execute before the historical
				// schedule conflict is reported, including during rollback.
				probe := filepath.Join(t.TempDir(), "614_schedule_rename_probe.up.sql")
				if err := os.WriteFile(probe, []byte("CREATE TABLE schedule_rename_probe (id integer)"), 0o600); err != nil {
					t.Fatal(err)
				}
				files = append([]string{probe}, files...)
			}
			opts := runOptions{
				Direction: "up", Files: files, SchemaMigrationsTable: schema + ".schema_migrations",
				AdvisoryLockKey: int64(rand.Uint64()&0x7fffffffffffffff) | 1,
				Hooks:           hooksForDirection("up"),
			}
			if legacyVersion != "" {
				for range 2 {
					err := runMigrations(ctx, pool, opts)
					if err == nil || !strings.Contains(err.Error(), "refusing to reapply the renamed migration") {
						t.Fatalf("expected explicit legacy upgrade stop, got %v", err)
					}
				}
				down := opts
				down.Direction, down.Files = "down", realMigrationFiles(t, []string{version}, "down")
				if err := runMigrations(ctx, pool, down); err == nil || !strings.Contains(err.Error(), "refusing to reapply the renamed migration") {
					t.Fatalf("expected explicit legacy rollback stop, got %v", err)
				}
				var probeExists bool
				if err := pool.QueryRow(ctx, "SELECT to_regclass('schedule_rename_probe') IS NOT NULL").Scan(&probeExists); err != nil || probeExists {
					t.Fatalf("legacy guard ran after DDL: probe exists=%t, %v", probeExists, err)
				}
				var limit, count int64
				if err := pool.QueryRow(ctx, "SELECT execution_limit, execution_count FROM autopilot_trigger WHERE kind='schedule'").Scan(&limit, &count); err != nil || limit != 3 || count != 3 {
					t.Fatalf("legacy counters changed: %d/%d, %v", count, limit, err)
				}
				var versions []string
				rows, err := pool.Query(ctx, "SELECT version FROM schema_migrations ORDER BY version")
				if err != nil {
					t.Fatal(err)
				}
				versions, err = pgx.CollectRows(rows, pgx.RowTo[string])
				expected := []string{legacyVersion, "600_wecom_install_session", "615_attachment_image_dimensions"}
				slices.Sort(expected)
				if err != nil || !slices.Equal(versions, expected) {
					t.Fatalf("legacy history changed: %v, %v", versions, err)
				}
				var triggers, attachments int
				if err := pool.QueryRow(ctx, "SELECT count(*) FROM autopilot_trigger").Scan(&triggers); err != nil || triggers != 2 {
					t.Fatalf("legacy trigger rows changed: %d, %v", triggers, err)
				}
				if err := pool.QueryRow(ctx, "SELECT count(*) FROM attachment WHERE width=640 AND height=480").Scan(&attachments); err != nil || attachments != 1 {
					t.Fatalf("attachment dimensions changed: %d, %v", attachments, err)
				}
				return
			}
			for pass := 0; pass < 2; pass++ {
				for range 2 {
					if err := runMigrations(ctx, pool, opts); err != nil {
						t.Fatal(err)
					}
				}
				var defaults int
				if err := pool.QueryRow(ctx, "SELECT count(*) FROM autopilot_trigger WHERE execution_limit IS NULL AND execution_count=0").Scan(&defaults); err != nil || defaults != 2 {
					t.Fatalf("upgrade defaults: %d, %v", defaults, err)
				}
				down := opts
				down.Direction, down.Files, down.Hooks = "down", realMigrationFiles(t, []string{version}, "down"), hooksForDirection("down")
				if err := runMigrations(ctx, pool, down); err != nil {
					t.Fatal(err)
				}
				var rows int
				if err := pool.QueryRow(ctx, "SELECT count(*) FROM autopilot_trigger").Scan(&rows); err != nil || rows != 2 {
					t.Fatalf("rollback lost rows: %d, %v", rows, err)
				}
			}
		})
	}
}
