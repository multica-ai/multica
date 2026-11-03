package main

import (
	"context"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

func TestLabrastroEngineeringMigrationsUpDownAndInterruptedIndex(t *testing.T) {
	admin := openTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	schema := "engineering_" + strings.ReplaceAll(util.UUIDToString(dbid.NewV7()), "-", "")
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, table := range []string{"project_baseline_revision", "project_baseline_doc", "project_engineering", "schema_migrations"} {
			if _, err := admin.Exec(context.Background(), "DROP TABLE IF EXISTS "+quoted+"."+table); err != nil {
				t.Error(err)
			}
		}
		if _, err := admin.Exec(context.Background(), "DROP SCHEMA "+quoted); err != nil {
			t.Error(err)
		}
	})
	pool := openTestPoolWithSearchPath(t, schema)
	versions := []string{
		"9039_labrastro_engineering_tables",
		"9040_labrastro_engineering_config_identity_index",
		"9041_labrastro_engineering_doc_identity_index",
		"9042_labrastro_engineering_doc_chapter_index",
		"9043_labrastro_engineering_revision_identity_index",
		"9044_labrastro_engineering_revision_number_index",
		"9045_labrastro_engineering_candidate_index",
	}

	opts := runOptions{Direction: "up", Files: realMigrationFiles(t, versions[:1], "up"), SchemaMigrationsTable: schema + ".schema_migrations", AdvisoryLockKey: int64(rand.Uint64()&0x7fffffffffffffff) | 1, Hooks: hooksForDirection("up")}
	if err := runMigrations(ctx, pool, opts); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO project_engineering (workspace_id,project_id,updated_by_type,updated_by_id)
SELECT '00000000-0000-0000-0000-000000000001','00000000-0000-0000-0000-000000000002','member',gen_random_uuid() FROM generate_series(1,2)`); err != nil {
		t.Fatal(err)
	}
	opts.Files = realMigrationFiles(t, versions, "up")
	if err := runMigrations(ctx, pool, opts); err == nil {
		t.Fatal("duplicate project configurations did not stop unique index build")
	}
	assertIndexValidity(t, pool, schema, "idx_project_engineering_identity", false)
	if _, err := pool.Exec(ctx, "DELETE FROM project_engineering WHERE ctid IN (SELECT ctid FROM project_engineering LIMIT 1)"); err != nil {
		t.Fatal(err)
	}
	if err := runMigrations(ctx, pool, opts); err != nil {
		t.Fatal(err)
	}
	assertIndexValidity(t, pool, schema, "idx_project_engineering_identity", true)
	// Upgrade real legacy audit rows without rewriting their content or identity.
	if _, err := pool.Exec(ctx, `UPDATE project_engineering SET enabled=true, agent_decisions_enabled=true;
INSERT INTO project_baseline_revision (workspace_id,project_id,chapter,revision_number,status,
content_markdown,change_summary,anchors_diff,created_by_type,created_by_id,
decided_by_type,decided_by_id,decided_by_task_id,decided_at,decision_credential)
SELECT gen_random_uuid(),gen_random_uuid(),'overview',n,'adopted','Historical body','Original summary','{}',
'agent',gen_random_uuid(),'agent',gen_random_uuid(),gen_random_uuid(),now(),credential
FROM unnest(ARRAY['session','pat','task',NULL]) WITH ORDINALITY AS c(credential,n)`); err != nil {
		t.Fatal(err)
	}
	var before string
	if err := pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(r)-'decision_credential' ORDER BY revision_number)::text FROM project_baseline_revision r`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	upgrade := "9046_labrastro_engineering_decisions"
	for _, direction := range []string{"up", "down", "up"} {
		opts.Direction, opts.Files, opts.Hooks = direction, realMigrationFiles(t, []string{upgrade}, direction), hooksForDirection(direction)
		if err := runMigrations(ctx, pool, opts); err != nil {
			t.Fatal(err)
		}
		var after, credentials string
		var hasToggle, enabled bool
		if err := pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(r)-'decision_credential' ORDER BY revision_number)::text,
jsonb_agg(decision_credential ORDER BY revision_number)::text FROM project_baseline_revision r`).Scan(&after, &credentials); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT to_jsonb(e) ? 'agent_decisions_enabled',enabled FROM project_engineering e`).Scan(&hasToggle, &enabled); err != nil {
			t.Fatal(err)
		}
		want := `["session", "pat", "agent_task", null]`
		if direction == "down" {
			want = `["session", "pat", "task", null]`
		}
		if after != before || credentials != want || !enabled || hasToggle != (direction == "down") {
			t.Fatalf("%s lost audit/configuration: credentials=%s toggle=%v enabled=%v unchanged=%v", direction, credentials, hasToggle, enabled, after == before)
		}
		invalidCredential := "task"
		if direction == "down" {
			invalidCredential = "agent_task"
			var restored bool
			if err := pool.QueryRow(ctx, `SELECT agent_decisions_enabled FROM project_engineering`).Scan(&restored); err != nil || restored {
				t.Fatalf("restored switch must default to false: %v, %v", restored, err)
			}
		}
		for _, invalid := range []string{invalidCredential, "future"} {
			if _, err := pool.Exec(ctx, `UPDATE project_baseline_revision SET decision_credential=$1 WHERE revision_number=3`, invalid); err == nil {
				t.Fatalf("%s CHECK accepted invalid credential %s", direction, invalid)
			}
		}
	}
	versions = append(versions, upgrade)
	stagesMigration := "9047_labrastro_engineering_stages"
	for _, direction := range []string{"up", "down", "up"} {
		opts.Direction, opts.Files, opts.Hooks = direction, realMigrationFiles(t, []string{stagesMigration}, direction), hooksForDirection(direction)
		if err := runMigrations(ctx, pool, opts); err != nil {
			t.Fatal(err)
		}
		var after string
		if err := pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(r)-'decision_credential' ORDER BY revision_number)::text FROM project_baseline_revision r`).Scan(&after); err != nil {
			t.Fatal(err)
		}
		if after != before {
			t.Fatal("stage migration changed historical revisions")
		}
		var hasStages, enabled bool
		if err := pool.QueryRow(ctx, `SELECT to_jsonb(e) ? 'stages', enabled FROM project_engineering e`).Scan(&hasStages, &enabled); err != nil {
			t.Fatal(err)
		}
		if !enabled || hasStages != (direction == "up") {
			t.Fatalf("%s hasStages=%v enabled=%v", direction, hasStages, enabled)
		}
		if direction == "down" {
			continue
		}
		var stages string
		if err := pool.QueryRow(ctx, `SELECT stages::text FROM project_engineering`).Scan(&stages); err != nil || stages != "[]" {
			t.Fatalf("default stages=%s err=%v", stages, err)
		}
		for _, invalid := range []any{nil, `null`, `{}`, `"text"`, `1`, `true`, `[` + strings.Repeat(`{},`, 50) + `{}]`} {
			if _, err := pool.Exec(ctx, `UPDATE project_engineering SET stages=$1::jsonb`, invalid); err == nil {
				t.Fatalf("CHECK accepted %v", invalid)
			}
		}
		for _, valid := range []string{`[]`, `[` + strings.Repeat(`{},`, 49) + `{}]`, `[{"stage":9,"title":"Release","goal":""}]`} {
			if _, err := pool.Exec(ctx, `UPDATE project_engineering SET stages=$1::jsonb`, valid); err != nil {
				t.Fatal(err)
			}
		}
	}
	versions = append(versions, stagesMigration)
	reversed := slices.Clone(versions)
	slices.Reverse(reversed)
	opts.Direction, opts.Files, opts.Hooks = "down", realMigrationFiles(t, reversed, "down"), hooksForDirection("down")
	if err := runMigrations(ctx, pool, opts); err != nil {
		t.Fatal(err)
	}
	opts.Direction, opts.Files, opts.Hooks = "up", realMigrationFiles(t, versions, "up"), hooksForDirection("up")
	if err := runMigrations(ctx, pool, opts); err != nil {
		t.Fatal(err)
	}
	assertIndexValidity(t, pool, schema, "idx_project_baseline_candidate", true)
}
