package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// FIXME(test-integration): Exercises PostgreSQL interrupted concurrent builds and
// the production migration hooks; requires the isolated test database.
func TestTypingCleanupRepairsInterruptedIndexes(t *testing.T) {
	versions := []string{
		"536_channel_typing_cleanup",
		"537_channel_typing_reaction_id_idx",
		"538_channel_typing_reaction_retry_idx",
		"539_channel_typing_reaction_gc_idx",
		"540_channel_typing_limits",
		"541_channel_typing_quota_idx",
		"542_channel_typing_expiry_idx",
		"543_channel_typing_abandoned_idx",
	}
	files := make([]string, len(versions))
	for i, version := range versions {
		files[i] = filepath.Join("..", "..", "migrations", version+".up.sql")
	}
	for i, index := range []string{
		"channel_typing_reaction_id_idx",
		"channel_typing_reaction_retry_idx",
		"channel_typing_reaction_gc_idx",
		"channel_typing_reaction_quota_idx",
		"channel_typing_reaction_expiry_idx",
		"channel_typing_reaction_abandoned_idx",
	} {
		t.Run(index, func(t *testing.T) {
			buildPosition := i + 1
			if i >= 3 {
				buildPosition++
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			admin := openTestPool(t)
			schema := fmt.Sprintf("typing_index_%d_%d", time.Now().UnixNano(), rand.Uint32())
			schemaIdent := pgx.Identifier{schema}.Sanitize()
			if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schemaIdent); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cleanupCancel()
				if _, err := admin.Exec(cleanupCtx, "DROP SCHEMA "+schemaIdent+" CASCADE"); err != nil {
					t.Errorf("drop isolated schema: %v", err)
				}
			})
			// Scope every connection so the real SQL and production hooks are
			// exercised unchanged, including their unqualified relation names.
			pool := openTestPoolWithSearchPath(t, schema)
			if _, err := pool.Exec(ctx, "CREATE TABLE chat_message (id uuid NOT NULL)"); err != nil {
				t.Fatal(err)
			}
			tableSQL, err := os.ReadFile(files[0])
			if err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, string(tableSQL)); err != nil {
				t.Fatal(err)
			}
			limitsSQL, err := os.ReadFile(files[4])
			if err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, string(limitsSQL)); err != nil {
				t.Fatal(err)
			}
			buildSQL, err := os.ReadFile(files[buildPosition])
			if err != nil {
				t.Fatal(err)
			}
			// Hold a writer lock so the actual concurrent build is interrupted
			// after its catalog entry exists. No pg_catalog mutation or mock DDL.
			blocker, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer blocker.Rollback(context.Background())
			if _, err := blocker.Exec(ctx, "LOCK TABLE channel_typing_reaction IN ROW EXCLUSIVE MODE"); err != nil {
				t.Fatal(err)
			}
			builder, err := pool.Acquire(ctx)
			if err != nil {
				t.Fatal(err)
			}
			// Close instead of returning a connection with changed session state.
			conn := builder.Hijack()
			defer conn.Close(context.Background())
			if _, err := conn.Exec(ctx, "SET statement_timeout = '2s'"); err != nil {
				t.Fatal(err)
			}
			_, buildErr := conn.Exec(ctx, string(buildSQL))
			var pgErr *pgconn.PgError
			if !errors.As(buildErr, &pgErr) || pgErr.Code != "57014" {
				t.Fatalf("want interrupted build (57014), got %v", buildErr)
			}
			if err := blocker.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			assertIndexValidity(t, pool, schema, index, false)
			indexOID := func() uint32 {
				t.Helper()
				var oid uint32
				if err := pool.QueryRow(ctx, "SELECT to_regclass($1)::oid", index).Scan(&oid); err != nil {
					t.Fatal(err)
				}
				return oid
			}
			invalidOID := indexOID()
			opts := runOptions{
				Direction: "up", Files: files,
				SchemaMigrationsTable: schema + ".schema_migrations",
				AdvisoryLockKey:       int64(rand.Uint64()&0x7fffffffffffffff) | 1,
				Hooks:                 preMigrationHooks,
			}
			if err := runMigrations(ctx, pool, opts); err != nil {
				t.Fatal(err)
			}
			assertIndexReadyAndValid(t, pool, schema, index, true)
			rebuiltOID := indexOID()
			if rebuiltOID == invalidOID {
				t.Fatal("INVALID index was not dropped and rebuilt")
			}
			var applied int
			if err := pool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations WHERE version = ANY($1)", versions).Scan(&applied); err != nil || applied != len(versions) {
				t.Fatalf("want all migrations recorded, got %d: %v", applied, err)
			}
			// Cover a completed build whose migration stamp was interrupted:
			// the hook must preserve the valid index on retry.
			if _, err := pool.Exec(ctx, "DELETE FROM schema_migrations WHERE version = $1", versions[buildPosition]); err != nil {
				t.Fatal(err)
			}
			if err := runMigrations(ctx, pool, opts); err != nil {
				t.Fatal(err)
			}
			if indexOID() != rebuiltOID {
				t.Fatal("retry replaced an already-valid index")
			}
			assertIndexReadyAndValid(t, pool, schema, index, true)
			t.Log("interrupted INVALID index dropped and rebuilt; all migrations recorded; valid retry preserves index")
		})
	}
}
