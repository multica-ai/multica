package main

import (
	"context"
	"errors"
	"math/rand/v2"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var projectMoveMigrationVersions = []string{"614_project_move", "615_issue_key_alias_key", "616_issue_key_alias_issue", "617_project_move_id", "618_project_move_source", "619_search_index_workspace_key", "620_search_index_workspace_moves"}

func projectMoveMigrationPool(t *testing.T, ctx context.Context) (*pgxpool.Pool, string) {
	t.Helper()
	pool, schema := projectMoveBaseMigrationPool(t, ctx)
	runProjectMoveMigrations(t, ctx, pool, schema, "up", projectMoveMigrationVersions)
	return pool, schema
}

func projectMoveBaseMigrationPool(t *testing.T, ctx context.Context) (*pgxpool.Pool, string) {
	t.Helper()
	base := openTestPool(t)
	schema := createScratchSchema(t, ctx, base, "project_move_")
	pool := openTestPoolWithSearchPath(t, schema)
	if _, err := pool.Exec(ctx, `CREATE TABLE issue(id uuid,workspace_id uuid);
CREATE TABLE project(id uuid,workspace_id uuid);
CREATE TABLE comment(id uuid,workspace_id uuid,content text,deleted_at timestamptz,issue_id uuid,created_at timestamptz,resolved boolean);`); err != nil {
		t.Fatal(err)
	}
	runProjectMoveMigrations(t, ctx, pool, schema, "up", []string{"561_search_index_change"})
	return pool, schema
}

func runProjectMoveMigrations(t *testing.T, ctx context.Context, pool *pgxpool.Pool, schema, direction string, versions []string) {
	t.Helper()
	ordered := slices.Clone(versions)
	if direction == "down" {
		slices.Reverse(ordered)
	}
	if err := runMigrations(ctx, pool, runOptions{Direction: direction, Files: realMigrationFiles(t, ordered, direction), SchemaMigrationsTable: schema + ".schema_migrations", AdvisoryLockKey: int64(rand.Uint64()&0x7fffffffffffffff) | 1, Hooks: hooksForDirection(direction)}); err != nil {
		t.Fatalf("%s: %v", direction, err)
	}
}

func TestProjectMoveRollbackLockTimeout(t *testing.T) {
	for _, write := range []string{"UPDATE comment SET content='held write'", "UPDATE comment SET resolved=true"} {
		t.Run(write, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			pool, schema := projectMoveMigrationPool(t, ctx)
			if _, err := pool.Exec(ctx, `INSERT INTO comment(id,workspace_id,content) VALUES(gen_random_uuid(),gen_random_uuid(),'before');
INSERT INTO issue VALUES(gen_random_uuid(),'00000000-0000-0000-0000-00000000000a');
UPDATE issue SET workspace_id='00000000-0000-0000-0000-00000000000b';`); err != nil {
				t.Fatal(err)
			}
			blocker, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer blocker.Rollback(context.Background())
			// The completed UPDATE deterministically owns the ordinary comment writer's
			// locks before rollback starts; no timing/sleep-based synchronization.
			if _, err := blocker.Exec(ctx, write); err != nil {
				t.Fatal(err)
			}
			attemptCtx, stop := context.WithTimeout(ctx, 8*time.Second)
			defer stop()
			start := time.Now()
			err = runMigrations(attemptCtx, pool, runOptions{Direction: "down", Files: realMigrationFiles(t, []string{"620_search_index_workspace_moves"}, "down"), SchemaMigrationsTable: schema + ".schema_migrations", AdvisoryLockKey: int64(rand.Uint64()&0x7fffffffffffffff) | 1, Hooks: hooksForDirection("down")})
			elapsed := time.Since(start)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "55P03" || elapsed > 5*time.Second {
				t.Errorf("rollback must give up on its own lock_timeout, not the 8s watchdog: elapsed=%s err=%v", elapsed, err)
			} else {
				t.Logf("rollback lock_timeout: elapsed=%s SQLSTATE=%s", elapsed, pgErr.Code)
			}
			if err := blocker.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			var projections int
			var recorded, marked bool
			if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM search_index_change WHERE entity_type='issue'),
EXISTS(SELECT 1 FROM schema_migrations WHERE version='620_search_index_workspace_moves'),
EXISTS(SELECT 1 FROM search_index_prune_mark)`).Scan(&projections, &recorded, &marked); err != nil || projections != 1 || !recorded || !marked {
				t.Fatalf("failed DDL must retain the committed hook's prune mark and ledger: projections=%d recorded=%v marked=%v err=%v", projections, recorded, marked, err)
			}
			runProjectMoveMigrations(t, ctx, pool, schema, "down", projectMoveMigrationVersions[5:])
			assertIndexReadyAndValid(t, pool, schema, "search_index_change_pkey", true)
		})
	}
}

func TestProjectMoveRollbackCleanupBeforeCommentDDL(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	pool, _ := projectMoveMigrationPool(t, ctx)
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(context.Background())
	if _, err := blocker.Exec(ctx, "LOCK TABLE search_index_change IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile(realMigrationFiles(t, []string{"620_search_index_workspace_moves"}, "down")[0])
	if err != nil {
		t.Fatal(err)
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	done := make(chan error, 1)
	pid := conn.Conn().PgConn().PID()
	go func() { _, err := conn.Exec(ctx, string(migration)); done <- err }()
	// Observe PostgreSQL's actual waiting lock, not a sleep or goroutine start.
	for {
		var waiting, commentLocked bool
		if err := pool.QueryRow(ctx, `SELECT
EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND relation='search_index_change'::regclass AND NOT granted),
EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND relation='comment'::regclass AND granted)`, pid).Scan(&waiting, &commentLocked); err != nil {
			t.Fatal(err)
		}
		if waiting {
			if commentLocked {
				t.Error("rollback already holds comment DDL lock while search cleanup is blocked")
			}
			t.Logf("search cleanup waiting; comment lock held=%v", commentLocked)
			break
		}
		select {
		case err := <-done:
			t.Fatalf("rollback finished before reaching blocked cleanup: %v", err)
		default:
		}
	}
	if err := blocker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("rollback after releasing blocker: %v", err)
	}
}

func TestProjectMoveRollbackGapRejectsWrites(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool, schema := projectMoveMigrationPool(t, ctx)
	if _, err := pool.Exec(ctx, `INSERT INTO issue VALUES(gen_random_uuid(),'00000000-0000-0000-0000-00000000000a')`); err != nil {
		t.Fatal(err)
	}
	runProjectMoveMigrations(t, ctx, pool, schema, "down", projectMoveMigrationVersions[6:])
	// A writer in another connection enters the exact gap before 619's
	// concurrent unique build. Fail closed rather than introduce duplicates.
	writer, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Release()
	_, err = writer.Exec(ctx, "UPDATE issue SET workspace_id='00000000-0000-0000-0000-00000000000b'")
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42P10" {
		t.Errorf("write without the entity key must fail closed: %v", err)
	}
	var workspace string
	if err := pool.QueryRow(ctx, "SELECT workspace_id::text FROM issue").Scan(&workspace); err != nil || workspace != "00000000-0000-0000-0000-00000000000a" {
		t.Errorf("failed move must leave ownership unchanged: workspace=%s err=%v", workspace, err)
	}
	runProjectMoveMigrations(t, ctx, pool, schema, "down", projectMoveMigrationVersions[5:6])
	assertIndexReadyAndValid(t, pool, schema, "search_index_change_pkey", true)
	if _, err := writer.Exec(ctx, "UPDATE issue SET workspace_id='00000000-0000-0000-0000-00000000000b'"); err != nil {
		t.Fatalf("retry after 619: %v", err)
	}
	var projections int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM search_index_change").Scan(&projections); err != nil || projections != 1 {
		t.Fatalf("retry projections=%d err=%v", projections, err)
	}
}

func TestProjectMoveMigrationsUpDownUp(t *testing.T) {
	for _, source := range []string{"00000000-0000-0000-0000-00000000000a", "00000000-0000-0000-0000-00000000000b"} {
		t.Run(source, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
			defer cancel()
			pool, schema := projectMoveMigrationPool(t, ctx)
			target := "00000000-0000-0000-0000-00000000000a"
			if source == target {
				target = "00000000-0000-0000-0000-00000000000b"
			}
			if _, err := pool.Exec(ctx, "INSERT INTO issue VALUES('00000000-0000-0000-0000-000000000001',$1)", source); err != nil {
				t.Fatal(err)
			}
			q := db.New(pool)
			// Each workspace client holds a real snapshot taken before the move.
			cursors := make(map[string]string)
			for _, workspace := range []string{source, target} {
				cursor, err := q.GetSearchIndexCurrentSnapshot(ctx)
				if err != nil {
					t.Fatal(err)
				}
				cursors[workspace] = cursor
			}
			if _, err := pool.Exec(ctx, "UPDATE issue SET workspace_id=$1", target); err != nil {
				t.Fatal(err)
			}
			var projections, xids int
			if err := pool.QueryRow(ctx, "SELECT count(*),count(DISTINCT change_xid) FROM search_index_change").Scan(&projections, &xids); err != nil || projections != 2 || xids != 1 {
				t.Fatalf("move projections=%d xids=%d err=%v", projections, xids, err)
			}
			for workspace, cursor := range cursors {
				if pruned, err := q.SearchIndexSinceIsPruned(ctx, cursor); err != nil || pruned {
					t.Fatalf("before rollback workspace=%s pruned=%v err=%v", workspace, pruned, err)
				}
			}
			// Keep the populated search tables: losing either projection must
			// invalidate both clients, regardless of the workspace UUID order.
			runProjectMoveMigrations(t, ctx, pool, schema, "down", projectMoveMigrationVersions[5:])
			for workspace, cursor := range cursors {
				if pruned, err := q.SearchIndexSinceIsPruned(ctx, cursor); err != nil || !pruned {
					t.Errorf("after rollback workspace=%s pruned=%v err=%v; client must rebuild", workspace, pruned, err)
				}
			}
			assertIndexReadyAndValid(t, pool, schema, "search_index_change_pkey", true)
			runProjectMoveMigrations(t, ctx, pool, schema, "down", projectMoveMigrationVersions[:5])
			for _, table := range []string{"issue_key_alias", "project_move"} {
				var exists bool
				if err := pool.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", schema+"."+table).Scan(&exists); err != nil || exists {
					t.Fatalf("down table=%s exists=%v err=%v", table, exists, err)
				}
			}
			runProjectMoveMigrations(t, ctx, pool, schema, "up", projectMoveMigrationVersions)
			for _, index := range []string{"idx_issue_key_alias_key", "idx_issue_key_alias_issue", "idx_project_move_id", "idx_project_move_source", "search_index_change_pkey"} {
				assertIndexReadyAndValid(t, pool, schema, index, true)
			}
			if _, err := pool.Exec(ctx, "UPDATE issue SET workspace_id=$1", source); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, "UPDATE issue SET workspace_id=workspace_id"); err != nil {
				t.Fatal(err)
			}
			if err := pool.QueryRow(ctx, "SELECT count(*) FROM search_index_change").Scan(&projections); err != nil || projections != 2 {
				t.Fatalf("up again projections=%d err=%v", projections, err)
			}
		})
	}
}
