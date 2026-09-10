package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestCommentSearchIndexStrategyChoosesOneUsableIndexPerEnvironment(t *testing.T) {
	adminPool := openTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	if _, err := adminPool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS pg_trgm"); err != nil {
		t.Fatalf("install pg_trgm test dependency: %v", err)
	}

	tests := []struct {
		name              string
		createPreferred   string
		createFallback    bool
		versions          []string
		wantFallbackAfter bool
	}{
		{
			name:              "self-host without preferred index builds and keeps fallback",
			versions:          []string{"140_comment_content_trgm_index", "371_comment_content_search_index_strategy"},
			wantFallbackAfter: true,
		},
		{
			name: "fresh pg_bigm-like environment skips fallback build",
			createPreferred: `CREATE INDEX idx_comment_content_bigm
				ON comment USING gin (LOWER(content) public.gin_trgm_ops)`,
			versions: []string{"140_comment_content_trgm_index", "371_comment_content_search_index_strategy"},
		},
		{
			name: "existing deployment with both indexes drops fallback",
			createPreferred: `CREATE INDEX idx_comment_content_bigm
				ON comment USING gin (LOWER(content) public.gin_trgm_ops)`,
			createFallback: true,
			versions:       []string{"371_comment_content_search_index_strategy"},
		},
		{
			name: "same-named but wrong preferred index preserves fallback",
			createPreferred: `CREATE INDEX idx_comment_content_bigm
				ON comment (LOWER(content))`,
			versions:          []string{"140_comment_content_trgm_index", "371_comment_content_search_index_strategy"},
			wantFallbackAfter: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suffix := fmt.Sprintf("%d_%d", time.Now().UnixNano(), rand.Uint32())
			schema := "migrate_comment_search_" + suffix
			schemaIdent := pgx.Identifier{schema}.Sanitize()
			if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+schemaIdent); err != nil {
				t.Fatalf("create schema: %v", err)
			}
			t.Cleanup(func() {
				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cleanupCancel()
				if _, err := adminPool.Exec(cleanupCtx, "DROP SCHEMA IF EXISTS "+schemaIdent+" CASCADE"); err != nil {
					t.Logf("drop schema %s: %v", schema, err)
				}
			})

			pool := openTestPoolWithSearchPath(t, schema+", public")
			if _, err := pool.Exec(ctx, `CREATE TABLE comment (
				id BIGSERIAL PRIMARY KEY,
				content TEXT NOT NULL
			)`); err != nil {
				t.Fatalf("create comment fixture: %v", err)
			}
			if tt.createPreferred != "" {
				if _, err := pool.Exec(ctx, tt.createPreferred); err != nil {
					t.Fatalf("create preferred fixture index: %v", err)
				}
			}
			if tt.createFallback {
				if _, err := pool.Exec(ctx, `CREATE INDEX idx_comment_content_trgm
					ON comment USING gin (LOWER(content) public.gin_trgm_ops)`); err != nil {
					t.Fatalf("create fallback fixture index: %v", err)
				}
			}

			requirement := usableIndexRequirement{
				IndexRegclass: schema + ".idx_comment_content_bigm",
				TableRegclass: schema + ".comment",
				AccessMethod:  "gin",
				OperatorClass: "gin_trgm_ops",
				Expression:    "lower(content)",
				Extension:     "pg_trgm",
			}
			conditions := map[string]migrationCondition{
				"140_comment_content_trgm_index":            whenIndexNotUsable(requirement),
				"371_comment_content_search_index_strategy": whenIndexUsable(requirement),
			}
			migrationsTable := schema + ".schema_migrations"
			if err := runMigrations(ctx, pool, runOptions{
				Direction:             "up",
				Files:                 realMigrationFiles(t, tt.versions, "up"),
				SchemaMigrationsTable: migrationsTable,
				AdvisoryLockKey:       int64(rand.Uint64()&0x7fffffffffffffff) | 1,
				Hooks:                 hooksForDirection("up"),
				Conditions:            conditions,
			}); err != nil {
				t.Fatalf("apply search index strategy: %v", err)
			}

			assertIndexExists(t, pool, schema, "idx_comment_content_trgm", tt.wantFallbackAfter)
			if tt.createPreferred != "" {
				assertIndexExists(t, pool, schema, "idx_comment_content_bigm", true)
			}
			for _, version := range tt.versions {
				assertMigrationVersionRecorded(t, ctx, pool, schema, version, true)
			}

			// Rollback always restores the portable fallback. On a self-hosted
			// database where it was retained, IF NOT EXISTS makes this a no-op.
			if err := runMigrations(ctx, pool, runOptions{
				Direction:             "down",
				Files:                 realMigrationFiles(t, []string{"371_comment_content_search_index_strategy"}, "down"),
				SchemaMigrationsTable: migrationsTable,
				AdvisoryLockKey:       int64(rand.Uint64()&0x7fffffffffffffff) | 1,
				Hooks:                 hooksForDirection("down"),
			}); err != nil {
				t.Fatalf("roll back search index strategy: %v", err)
			}
			assertIndexValidity(t, pool, schema, "idx_comment_content_trgm", true)
			assertMigrationVersionRecorded(t, ctx, pool, schema, "371_comment_content_search_index_strategy", false)
		})
	}
}

func TestCommentContentBigramRequirementMatchesRealPGBigm(t *testing.T) {
	adminPool := openTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	var available bool
	if err := adminPool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_available_extensions
			WHERE name = 'pg_bigm'
		)
	`).Scan(&available); err != nil {
		t.Fatalf("inspect pg_bigm availability: %v", err)
	}
	if !available {
		t.Skip("Postgres does not provide pg_bigm; install it to run the real-opclass integration test")
	}
	if _, err := adminPool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS pg_bigm"); err != nil {
		t.Fatalf("install pg_bigm test dependency: %v", err)
	}

	suffix := fmt.Sprintf("%d_%d", time.Now().UnixNano(), rand.Uint32())
	schema := "migrate_comment_bigm_" + suffix
	schemaIdent := pgx.Identifier{schema}.Sanitize()
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+schemaIdent); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if _, err := adminPool.Exec(cleanupCtx, "DROP SCHEMA IF EXISTS "+schemaIdent+" CASCADE"); err != nil {
			t.Logf("drop schema %s: %v", schema, err)
		}
	})

	pool := openTestPoolWithSearchPath(t, schema+", public")
	if _, err := pool.Exec(ctx, `CREATE TABLE comment (
		id BIGSERIAL PRIMARY KEY,
		content TEXT NOT NULL
	)`); err != nil {
		t.Fatalf("create comment fixture: %v", err)
	}
	if _, err := pool.Exec(ctx, `CREATE INDEX idx_comment_content_bigm
		ON comment USING gin (LOWER(content) gin_bigm_ops)`); err != nil {
		t.Fatalf("create real pg_bigm fixture index: %v", err)
	}

	requirement := commentContentBigramIndex
	requirement.IndexRegclass = schema + ".idx_comment_content_bigm"
	requirement.TableRegclass = schema + ".comment"
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}
	defer conn.Release()

	usable, err := indexIsUsable(ctx, conn, requirement)
	if err != nil {
		t.Fatalf("inspect real pg_bigm index: %v", err)
	}
	if !usable {
		t.Fatal("production pg_bigm requirement did not match a real gin_bigm_ops index")
	}
}

func assertMigrationVersionRecorded(
	t *testing.T,
	ctx context.Context,
	pool interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	},
	schema string,
	version string,
	want bool,
) {
	t.Helper()
	var recorded bool
	migrationsTable := pgx.Identifier{schema, "schema_migrations"}.Sanitize()
	if err := pool.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM "+migrationsTable+" WHERE version = $1)",
		version,
	).Scan(&recorded); err != nil {
		t.Fatalf("read migration version %s: %v", version, err)
	}
	if recorded != want {
		t.Fatalf("migration %s recorded = %v, want %v", version, recorded, want)
	}
}
