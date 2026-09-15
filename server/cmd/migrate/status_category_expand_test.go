package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/issuestatus"
)

func TestStatusCategoryExpandUpgradePaths(t *testing.T) {
	for _, applied := range []bool{false, true} {
		name := "pending_469"
		if applied {
			name = "applied_469"
		}
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			pool := openTestPoolWithSearchPath(t, f.schema)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			execFile := func(version string) {
				t.Helper()
				file := realMigrationFiles(t, []string{version}, "up")[0]
				body, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := pool.Exec(ctx, string(body)); err != nil {
					t.Fatal(err)
				}
			}
			execFile("332_issue_status")
			for _, key := range issuestatus.Canonical() {
				_, err := pool.Exec(ctx, `INSERT INTO issue_status(workspace_id,key,name,category,color,is_system,archived_at)
      VALUES ('00000000-0000-0000-0000-000000000001',$1,'System',$1,'#123456',true,NULL),
             ('00000000-0000-0000-0000-000000000001','custom_' || $1,'Custom',$1,'#123456',false,now())`, key)
				if err != nil {
					t.Fatal(err)
				}
			}
			opts := f.opts()
			opts.Files = realMigrationFiles(t, []string{"469_issue_status_lifecycle_categories"}, "up")
			if applied {
				if err := runMigrations(ctx, pool, opts); err != nil {
					t.Fatal(err)
				}
			}
			var before string
			if err := pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(s) ORDER BY key)::text FROM issue_status s`).Scan(&before); err != nil {
				t.Fatal(err)
			}
			opts.Files = realMigrationFiles(t, []string{"469_issue_status_lifecycle_categories", "477_issue_effective_status_triage", "478_issue_status_category_expand"}, "up")
			opts.Conditions = conditionsForDirection("up")
			if !applied {
				// A normal reader prevents the expand DDL. The real runner must time out,
				// leave all rows intact, and not record 478 as applied.
				reader, err := pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer reader.Rollback(context.Background())
				if _, err := reader.Exec(ctx, "SELECT * FROM issue_status"); err != nil {
					t.Fatal(err)
				}
				start := time.Now()
				err = runMigrations(ctx, pool, opts)
				if err == nil || !strings.Contains(err.Error(), "lock timeout") {
					t.Fatalf("expected bounded lock timeout, got %v", err)
				}
				t.Logf("reader contention stopped expand after %s", time.Since(start))
				assertMigrationVersionRecorded(t, ctx, pool, f.schema, "478_issue_status_category_expand", false)
				if err := reader.Rollback(ctx); err != nil {
					t.Fatal(err)
				}
			}
			for range 2 {
				if err := runMigrations(ctx, pool, opts); err != nil {
					t.Fatal(err)
				}
			}
			// Also replay the actual SQL after an uncertain ledger acknowledgement.
			execFile("478_issue_status_category_expand")
			var after string
			if err := pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(s) ORDER BY key)::text FROM issue_status s`).Scan(&after); err != nil {
				t.Fatal(err)
			}
			if after != before {
				t.Fatal("expand rewrote catalog rows, identity or archived data")
			}
			for _, key := range issuestatus.Canonical() {
				want := "custom_" + key
				if key == "done" || key == "cancelled" {
					want = key
				}
				var got string
				if err := pool.QueryRow(ctx, `SELECT issue_effective_status('00000000-0000-0000-0000-000000000001',$1)`, "custom_"+key).Scan(&got); err != nil {
					t.Fatal(err)
				}
				if got != want {
					t.Fatalf("%s behavior = %s, want %s", key, got, want)
				}
			}
			var locks int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE relation = 'issue_status'::regclass AND mode = 'AccessExclusiveLock'`).Scan(&locks); err != nil {
				t.Fatal(err)
			}
			if locks != 0 {
				t.Fatalf("expand retained %d exclusive locks", locks)
			}
			t.Log("14 system/archived rows unchanged; no exclusive locks remain; replay passed")
			// Both old and new writes remain legal while readers are upgraded.
			if _, err := pool.Exec(ctx, `UPDATE issue_status SET category = 'cancelled' WHERE key = 'custom_cancelled';
      UPDATE issue_status SET category = 'closed' WHERE key = 'custom_cancelled'`); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE issue_status SET category = 'started' WHERE key = 'cancelled'`); err == nil {
				t.Fatal("noncanonical built-in pair accepted")
			}
			assertMigrationVersionRecorded(t, ctx, pool, f.schema, "469_issue_status_lifecycle_categories", true)
			assertMigrationVersionRecorded(t, ctx, pool, f.schema, "478_issue_status_category_expand", true)
		})
	}
}
