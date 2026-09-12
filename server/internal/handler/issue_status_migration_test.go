package handler

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/issuestatus"
)

// Exercise the actual migration against the previous catalog schema. The
// isolated transactional schema cannot change application tables or enqueue work.
func TestIssueStatusLifecycleMigrationPreservesIdentity(t *testing.T) {
	ctx := context.Background()
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	schema := "status_migration_" + uuid.NewString()
	if _, err := tx.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "SET LOCAL search_path TO "+pgx.Identifier{schema}.Sanitize()+", public"); err != nil {
		t.Fatal(err)
	}
	old, err := os.ReadFile("../../migrations/332_issue_status.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, string(old)); err != nil {
		t.Fatal(err)
	}
	for _, key := range issuestatus.Canonical() {
		for _, system := range []bool{false, true} {
			storedKey := key
			if !system {
				storedKey = "custom_" + key
			}
			if _, err := tx.Exec(ctx, `INSERT INTO issue_status
				(workspace_id, key, name, description, category, color, is_system, position, archived_at)
				VALUES ($1, $2, 'Original name', 'Original description', $3, '#123456', $4, 7,
				CASE WHEN $4 THEN NULL ELSE '2026-01-01'::timestamptz END)`,
				testWorkspaceID, storedKey, key, system); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := tx.Exec(ctx, "CREATE TEMP TABLE catalog_before AS SELECT * FROM issue_status"); err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../migrations/468_issue_status_lifecycle_categories.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	// A replay after an uncertain migration acknowledgement must be harmless.
	for range 2 {
		if _, err := tx.Exec(ctx, string(migration)); err != nil {
			t.Fatal(err)
		}
	}
	var changed int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM catalog_before b FULL JOIN issue_status s USING(id)
		WHERE (to_jsonb(b) - 'category') IS DISTINCT FROM (to_jsonb(s) - 'category')`).Scan(&changed); err != nil {
		t.Fatal(err)
	}
	if changed != 0 {
		t.Fatalf("migration changed %d identities or non-category fields", changed)
	}
	for _, key := range issuestatus.Canonical() {
		wantCategory, _ := issuestatus.CategoryForBehavior(key)
		for _, system := range []bool{false, true} {
			storedKey, wantBehavior := key, key
			if !system {
				storedKey = "custom_" + key
				if key != "done" && key != "cancelled" {
					wantBehavior = storedKey
				}
			}
			var category, behavior string
			if err := tx.QueryRow(ctx, `SELECT category, issue_effective_status(workspace_id, key)
				FROM issue_status WHERE workspace_id = $1 AND key = $2`, testWorkspaceID, storedKey).Scan(&category, &behavior); err != nil {
				t.Fatal(err)
			}
			if category != wantCategory || behavior != wantBehavior {
				t.Errorf("%s = %s / %s, want %s / %s", storedKey, category, behavior, wantCategory, wantBehavior)
			}
		}
	}
	// Tenant isolation and unknown-key fail-safe remain true in the SQL helper.
	var unknown string
	if err := tx.QueryRow(ctx, "SELECT issue_effective_status($1, 'custom_done')", uuid.NewString()).Scan(&unknown); err != nil {
		t.Fatal(err)
	}
	if unknown != "custom_done" {
		t.Fatalf("cross-workspace resolution: %q", unknown)
	}
	// Adding icon must not rewrite old statuses, and replay must not erase a
	// shape saved after the first application (fix-forward migration recovery).
	if _, err := tx.Exec(ctx, "CREATE TEMP TABLE catalog_before_icon AS SELECT * FROM issue_status"); err != nil {
		t.Fatal(err)
	}
	iconMigration, err := os.ReadFile("../../migrations/469_issue_status_icon.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, string(iconMigration)); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM catalog_before_icon b FULL JOIN issue_status s USING(id)
		WHERE to_jsonb(b) IS DISTINCT FROM (to_jsonb(s) - 'icon') OR s.icon != ''`).Scan(&changed); err != nil {
		t.Fatal(err)
	}
	if changed != 0 {
		t.Fatalf("icon migration changed %d existing statuses", changed)
	}
	if _, err := tx.Exec(ctx, "UPDATE issue_status SET icon = 'slash' WHERE key = 'custom_blocked'"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, string(iconMigration)); err != nil {
		t.Fatal(err)
	}
	var icon string
	if err := tx.QueryRow(ctx, "SELECT icon FROM issue_status WHERE key = 'custom_blocked'").Scan(&icon); err != nil {
		t.Fatal(err)
	}
	if icon != "slash" {
		t.Fatalf("migration replay erased saved icon: %q", icon)
	}
}
