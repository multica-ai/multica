package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/issuestatus"
)

var categoryContractVersions = []string{
	"491_issue_status_category_backfill", "492_issue_status_category_contract",
	"493_issue_status_category_validate", "494_issue_status_category_read_contract",
}

// Exercise historical data through the real runner, rather than inserting old
// spellings into the current (strict) application schema.
func TestStatusCategoryContractUpgradePaths(t *testing.T) {
	for _, path := range []string{"fresh", "applied_469", "skipped_469", "mixed", "backfilled"} {
		t.Run(path, func(t *testing.T) {
			f := newFixture(t)
			pool := openTestPoolWithSearchPath(t, f.schema)
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
			defer cancel()
			execFile := func(version string) {
				t.Helper()
				b, err := os.ReadFile(realMigrationFiles(t, []string{version}, "up")[0])
				if err != nil {
					t.Fatal(err)
				}
				if _, err := pool.Exec(ctx, string(b)); err != nil {
					t.Fatal(err)
				}
			}
			execFile("332_issue_status")
			if path != "fresh" {
				for _, key := range issuestatus.Canonical() {
					if _, err := pool.Exec(ctx, `INSERT INTO issue_status(workspace_id,key,name,category,color,is_system,position,archived_at)
      VALUES ('00000000-0000-0000-0000-000000000001',$1,'System',$1,'#123456',true,42,NULL),
      ('00000000-0000-0000-0000-000000000001','custom_' || $1,'Custom',$1,'#123456',false,17,now())`, key); err != nil {
						t.Fatal(err)
					}
				}
			}
			opts := f.opts()
			opts.Files = realMigrationFiles(t, []string{"469_issue_status_lifecycle_categories"}, "up")
			if path == "applied_469" {
				if err := runMigrations(ctx, pool, opts); err != nil {
					t.Fatal(err)
				}
			}
			opts.Conditions = conditionsForDirection("up")
			opts.Files = realMigrationFiles(t, []string{"469_issue_status_lifecycle_categories", "478_issue_status_category_expand"}, "up")
			if err := runMigrations(ctx, pool, opts); err != nil {
				t.Fatal(err)
			}
			if path == "mixed" || path == "backfilled" {
				if _, err := pool.Exec(ctx, `UPDATE issue_status SET category=issue_status_category(category)
     WHERE $1 OR key IN ('todo','custom_in_review','custom_cancelled')`, path == "backfilled"); err != nil {
					t.Fatal(err)
				}
			}
			// Include the now-valid custom key triage and an archived terminal entry.
			if _, err := pool.Exec(ctx, `INSERT INTO issue_status(workspace_id,key,name,category,color)
    VALUES ('00000000-0000-0000-0000-000000000001','triage','Triage','closed','#123456')`); err != nil {
				t.Fatal(err)
			}
			var before string
			if err := pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(s)-'category' ORDER BY key)::text FROM issue_status s`).Scan(&before); err != nil {
				t.Fatal(err)
			}
			opts.Files = realMigrationFiles(t, categoryContractVersions, "up")
			if path == "skipped_469" {
				reader, err := pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer reader.Rollback(context.Background())
				if _, err := reader.Exec(ctx, "SELECT * FROM issue_status"); err != nil {
					t.Fatal(err)
				}
				// A reader allows the UPDATE to commit, but blocks constraint replacement.
				err = runMigrations(ctx, pool, opts)
				if err == nil || !strings.Contains(err.Error(), "lock timeout") {
					t.Fatalf("expected bounded DDL failure: %v", err)
				}
				assertMigrationVersionRecorded(t, ctx, pool, f.schema, categoryContractVersions[0], true)
				assertMigrationVersionRecorded(t, ctx, pool, f.schema, categoryContractVersions[1], false)
				var remaining int
				if err := pool.QueryRow(ctx, `SELECT count(*) FROM issue_status WHERE category<>issue_status_category(category)`).Scan(&remaining); err != nil || remaining != 0 {
					t.Fatalf("data transaction did not commit independently: %d %v", remaining, err)
				}
				if err := reader.Rollback(ctx); err != nil {
					t.Fatal(err)
				}
			}
			if path == "mixed" {
				// An old writer between data and DDL must be caught by validation.
				opts.Files = realMigrationFiles(t, categoryContractVersions[:1], "up")
				if err := runMigrations(ctx, pool, opts); err != nil {
					t.Fatal(err)
				}
				if _, err := pool.Exec(ctx, `UPDATE issue_status SET category='cancelled' WHERE key='custom_cancelled'`); err != nil {
					t.Fatal(err)
				}
				opts.Files = realMigrationFiles(t, categoryContractVersions, "up")
				if err := runMigrations(ctx, pool, opts); err == nil || !strings.Contains(err.Error(), "violated") {
					t.Fatalf("expected validation failure: %v", err)
				}
				assertMigrationVersionRecorded(t, ctx, pool, f.schema, categoryContractVersions[1], true)
				assertMigrationVersionRecorded(t, ctx, pool, f.schema, categoryContractVersions[2], false)
				assertMigrationVersionRecorded(t, ctx, pool, f.schema, categoryContractVersions[3], false)
				// Fix forward after eliminating the old writer; do not erase 491.
				execFile(categoryContractVersions[0])
			}
			for range 2 {
				if err := runMigrations(ctx, pool, opts); err != nil {
					t.Fatal(err)
				}
			}
			// Lost ledger acknowledgement: every SQL file is safe to replay.
			for _, version := range categoryContractVersions {
				execFile(version)
			}
			var after string
			if err := pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(s)-'category' ORDER BY key)::text FROM issue_status s`).Scan(&after); err != nil {
				t.Fatal(err)
			}
			if before != after {
				t.Fatal("identity, labels, positions, timestamps or archival changed")
			}
			var validated int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_constraint WHERE conrelid='issue_status'::regclass
    AND conname IN ('issue_status_category_check','issue_status_system_is_canonical') AND convalidated`).Scan(&validated); err != nil || validated != 2 {
				t.Fatalf("validated=%d: %v", validated, err)
			}
			for _, key := range issuestatus.Canonical() {
				category, _ := issuestatus.CategoryForBehavior(key)
				if path != "fresh" {
					var count int
					if err := pool.QueryRow(ctx, `SELECT count(*) FROM issue_status WHERE key IN ($1,'custom_' || $1) AND category=$2`, key, category).Scan(&count); err != nil || count != 2 {
						t.Fatalf("mapping %s: %d %v", key, count, err)
					}
				}
				if key != "done" {
					if _, err := pool.Exec(ctx, `INSERT INTO issue_status(workspace_id,key,name,category,color) VALUES (gen_random_uuid(),'invalid','Invalid',$1,'#123456')`, key); err == nil {
						t.Fatalf("accepted old category %s", key)
					}
				}
				var got string
				if err := pool.QueryRow(ctx, `SELECT issue_effective_status('00000000-0000-0000-0000-000000000001',$1)`, key).Scan(&got); err != nil || got != key {
					t.Fatalf("built-in %s: %s %v", key, got, err)
				}
				if path != "fresh" {
					want := "custom_" + key
					if key == "done" || key == "cancelled" {
						want = key
					}
					if err := pool.QueryRow(ctx, `SELECT issue_effective_status('00000000-0000-0000-0000-000000000001',$1)`, "custom_"+key).Scan(&got); err != nil || got != want {
						t.Fatalf("custom %s: %s %v", key, got, err)
					}
				}
			}
			var effective string
			if err := pool.QueryRow(ctx, `SELECT issue_effective_status('00000000-0000-0000-0000-000000000001','triage')`).Scan(&effective); err != nil || effective != "cancelled" {
				t.Fatalf("triage custom resolution: %s %v", effective, err)
			}
			if _, err := pool.Exec(ctx, `INSERT INTO issue_status(workspace_id,key,name,category,color,is_system) VALUES (gen_random_uuid(),'cancelled','Invalid','started','#123456',true)`); err == nil {
				t.Fatal("accepted noncanonical system pair")
			}
		})
	}
}
