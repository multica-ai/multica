package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"testing"
	"time"
)

func TestRuntimeRateLimitRenumberPreservesExistingSnapshot(t *testing.T) {
	t.Parallel()
	const version = "673_runtime_rate_limit"
	const legacyVersion = "665_runtime_rate_limit"
	for _, previouslyApplied := range []bool{false, true} {
		t.Run(fmt.Sprintf("previously_applied_%t", previouslyApplied), func(t *testing.T) {
			t.Parallel()
			admin := openTestPool(t)
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			schema := createScratchSchema(t, ctx, admin, "runtime_limit_renumber_")
			pool := openTestPoolWithSearchPath(t, schema)
			if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations (
				version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
			)`); err != nil {
				t.Fatal(err)
			}
			files := realMigrationFiles(t, []string{version}, "up")
			var originalOID uint32
			if previouslyApplied {
				// The renamed SQL is identical to the previously numbered 665 migration.
				sql, err := os.ReadFile(files[0])
				if err != nil {
					t.Fatal(err)
				}
				if _, err := pool.Exec(ctx, string(sql)); err != nil {
					t.Fatal(err)
				}
				if _, err := pool.Exec(ctx, "INSERT INTO schema_migrations(version) VALUES ($1)", legacyVersion); err != nil {
					t.Fatal(err)
				}
				if _, err := pool.Exec(ctx, `INSERT INTO runtime_rate_limit
					(runtime_id, workspace_id, snapshot, observed_at) VALUES
					('11111111-1111-4111-8111-111111111111', '22222222-2222-4222-8222-222222222222',
					 '{"synthetic":true,"used_percent":12}', '2026-01-01T00:00:00Z')`); err != nil {
					t.Fatal(err)
				}
				if err := pool.QueryRow(ctx, "SELECT $1::regclass::oid", schema+".runtime_rate_limit").Scan(&originalOID); err != nil {
					t.Fatal(err)
				}
			}
			options := runOptions{
				Direction: "up", Files: files,
				SchemaMigrationsTable: schema + ".schema_migrations",
				AdvisoryLockKey:       int64(rand.Uint64()&0x7fffffffffffffff) | 1,
			}
			for range 2 {
				if err := runMigrations(ctx, pool, options); err != nil {
					t.Fatal(err)
				}
			}
			var recorded bool
			if err := pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)", version).Scan(&recorded); err != nil || !recorded {
				t.Fatalf("new version recorded=%t, error=%v", recorded, err)
			}
			if previouslyApplied {
				var currentOID uint32
				if err := pool.QueryRow(ctx, "SELECT $1::regclass::oid", schema+".runtime_rate_limit").Scan(&currentOID); err != nil || currentOID != originalOID {
					t.Fatalf("table rebuilt: oid=%d, want %d, error=%v", currentOID, originalOID, err)
				}
				var retained bool
				if err := pool.QueryRow(ctx, `SELECT count(*)=1 AND bool_and(
					runtime_id='11111111-1111-4111-8111-111111111111' AND
					workspace_id='22222222-2222-4222-8222-222222222222' AND
					snapshot='{"synthetic":true,"used_percent":12}'::jsonb AND
					observed_at='2026-01-01T00:00:00Z'::timestamptz)
					FROM runtime_rate_limit`).Scan(&retained); err != nil || !retained {
					t.Fatalf("existing snapshot retained=%t, error=%v", retained, err)
				}
				if err := pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)", legacyVersion).Scan(&recorded); err != nil || !recorded {
					t.Fatalf("historical version retained=%t, error=%v", recorded, err)
				}
			}
			options.Direction = "down"
			options.Files = realMigrationFiles(t, []string{version}, "down")
			if err := runMigrations(ctx, pool, options); err != nil {
				t.Fatal(err)
			}
			var exists bool
			if err := pool.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", schema+".runtime_rate_limit").Scan(&exists); err != nil || exists {
				t.Fatalf("table exists after down=%t, error=%v", exists, err)
			}
			if err := pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)", version).Scan(&recorded); err != nil || recorded {
				t.Fatalf("new version recorded after down=%t, error=%v", recorded, err)
			}
			options.Direction, options.Files = "up", files
			if err := runMigrations(ctx, pool, options); err != nil {
				t.Fatal(err)
			}
			if err := pool.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", schema+".runtime_rate_limit").Scan(&exists); err != nil || !exists {
				t.Fatalf("table restored=%t, error=%v", exists, err)
			}
		})
	}
}
