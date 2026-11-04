package main

import (
	"context"
	"math/rand/v2"
	"testing"
	"time"
)

// Extend the concurrent-index missing-ledger contract to every new file, in
// both directions. Only the ledger is changed after successful execution.
func TestRunMigrationsReplaysProjectMoveFilesWithoutLedger(t *testing.T) {
	for i, version := range projectMoveMigrationVersions {
		for _, direction := range []string{"up", "down"} {
			t.Run(version+"/"+direction, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
				defer cancel()
				pool, schema := projectMoveBaseMigrationPool(t, ctx)
				runProjectMoveMigrations(t, ctx, pool, schema, "up", projectMoveMigrationVersions[:i+1])
				if direction == "down" {
					runProjectMoveMigrations(t, ctx, pool, schema, "down", []string{version})
				}
				// Fingerprint the actual final catalog (including key columns, functions,
				// and trigger definitions), not merely relation existence.
				fingerprint := func() string {
					var result string
					err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
      'tables',(SELECT jsonb_agg(relname ORDER BY relname) FROM pg_class WHERE relnamespace=$1::regnamespace AND relkind='r'),
      'columns',(SELECT jsonb_agg(ROW(c.relname,a.attname,a.atttypid,a.attnotnull) ORDER BY c.relname,a.attnum) FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid WHERE c.relnamespace=$1::regnamespace AND c.relkind='r' AND a.attnum>0 AND NOT a.attisdropped),
      'indexes',(SELECT jsonb_agg(ROW(c.relname,pg_get_indexdef(c.oid),i.indisvalid,i.indisready) ORDER BY c.relname) FROM pg_class c JOIN pg_index i ON i.indexrelid=c.oid WHERE c.relnamespace=$1::regnamespace),
      'constraints',(SELECT jsonb_agg(ROW(c.conname,pg_get_constraintdef(c.oid)) ORDER BY c.conname) FROM pg_constraint c WHERE c.connamespace=$1::regnamespace),
      'triggers',(SELECT jsonb_agg(pg_get_triggerdef(t.oid) ORDER BY t.tgname) FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid WHERE c.relnamespace=$1::regnamespace AND NOT t.tgisinternal),
      'functions',(SELECT jsonb_agg(pg_get_functiondef(p.oid) ORDER BY p.proname) FROM pg_proc p WHERE p.pronamespace=$1::regnamespace)
     )::text`, schema).Scan(&result)
					if err != nil {
						t.Fatal(err)
					}
					return result
				}
				expected := fingerprint()
				if direction == "up" {
					if _, err := pool.Exec(ctx, "DELETE FROM schema_migrations WHERE version=$1", version); err != nil {
						t.Fatal(err)
					}
				} else {
					if _, err := pool.Exec(ctx, "INSERT INTO schema_migrations(version) VALUES($1)", version); err != nil {
						t.Fatal(err)
					}
				}
				runProjectMoveMigrations(t, ctx, pool, schema, direction, []string{version})
				if actual := fingerprint(); actual != expected {
					t.Fatalf("schema changed on replay:\nwant %s\ngot %s", expected, actual)
				}
				var recorded bool
				if err := pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)", version).Scan(&recorded); err != nil || recorded != (direction == "up") {
					t.Fatalf("ledger=%v err=%v", recorded, err)
				}
			})
		}
	}
}

func projectMoveRollbackOptions(t *testing.T, schema string) runOptions {
	t.Helper()
	return runOptions{Direction: "down", Files: realMigrationFiles(t, []string{"620_search_index_workspace_moves"}, "down"), SchemaMigrationsTable: schema + ".schema_migrations", AdvisoryLockKey: int64(rand.Uint64()&0x7fffffffffffffff) | 1, Hooks: hooksForDirection("down")}
}
