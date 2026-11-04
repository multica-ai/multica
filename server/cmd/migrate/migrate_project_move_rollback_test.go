package main

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func seedProjectMoveRollbackDuplicates(t *testing.T, ctx context.Context, pool *pgxpool.Pool, count int) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO issue SELECT gen_random_uuid(),'00000000-0000-0000-0000-00000000000a'::uuid FROM generate_series(1,$1)`, count); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE issue SET workspace_id='00000000-0000-0000-0000-00000000000b'`); err != nil {
		t.Fatal(err)
	}
}

// A test-only delete trigger blocks on a session-owned advisory lock. Observe
// its actual PostgreSQL wait before testing an unrelated ordinary issue write.
func gateProjectMoveDeletes(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (*pgxpool.Conn, func()) {
	t.Helper()
	gate, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gate.Exec(ctx, `SELECT pg_advisory_lock(73579,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `CREATE TABLE move_delete_batches(batch_xid xid8);
CREATE FUNCTION gate_move_delete() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN PERFORM pg_advisory_xact_lock(73579,1);
 INSERT INTO move_delete_batches VALUES(pg_current_xact_id()); RETURN OLD; END $$;
 CREATE TRIGGER gate_move_delete BEFORE DELETE ON search_index_change FOR EACH ROW EXECUTE FUNCTION gate_move_delete()`); err != nil {
		t.Fatal(err)
	}
	released := false
	release := func() {
		if !released {
			released = true
			if _, err := gate.Exec(context.Background(), `SELECT pg_advisory_unlock(73579,1)`); err != nil {
				t.Error(err)
			}
			gate.Release()
		}
	}
	t.Cleanup(release)
	return gate, release
}

func waitProjectMoveDeleteGate(t *testing.T, ctx context.Context, pool *pgxpool.Pool, done <-chan error) int32 {
	t.Helper()
	for {
		var pid int32
		err := pool.QueryRow(ctx, `SELECT pid FROM pg_locks WHERE locktype='advisory' AND classid=73579 AND objid=1 AND NOT granted LIMIT 1`).Scan(&pid)
		if err == nil {
			return pid
		}
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
		select {
		case err := <-done:
			t.Fatalf("cleanup finished before gate: %v", err)
		default:
		}
	}
}

func TestProjectMoveRollbackBatchesAllowOrdinaryIssueWrite(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool, schema := projectMoveMigrationPool(t, ctx)
	seedProjectMoveRollbackDuplicates(t, ctx, pool, 257)
	if _, err := pool.Exec(ctx, `INSERT INTO issue VALUES('00000000-0000-0000-0000-000000000999','00000000-0000-0000-0000-00000000000b')`); err != nil {
		t.Fatal(err)
	}
	_, release := gateProjectMoveDeletes(t, ctx, pool)
	done := make(chan error, 1)
	go func() { done <- runMigrations(ctx, pool, projectMoveRollbackOptions(t, schema)) }()
	pid := waitProjectMoveDeleteGate(t, ctx, pool, done)
	var exclusive bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND relation='search_index_change'::regclass AND mode='AccessExclusiveLock' AND granted)`, pid).Scan(&exclusive); err != nil {
		t.Fatal(err)
	}
	writerCtx, stop := context.WithTimeout(ctx, time.Second)
	_, writeErr := pool.Exec(writerCtx, `UPDATE issue SET workspace_id=workspace_id WHERE id='00000000-0000-0000-0000-000000000999'`)
	stop()
	release()
	rollbackErr := <-done
	if writeErr != nil || exclusive {
		t.Fatalf("ordinary issue write must finish while cleanup is gated: write=%v exclusive=%v rollback=%v", writeErr, exclusive, rollbackErr)
	}
	if rollbackErr != nil {
		t.Fatal(rollbackErr)
	}
	var rows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM search_index_change`).Scan(&rows); err != nil || rows != 258 {
		t.Fatalf("rows=%d err=%v", rows, err)
	}
	var batches, largest int
	if err := pool.QueryRow(ctx, `SELECT count(*),max(n) FROM (SELECT count(*) n FROM move_delete_batches GROUP BY batch_xid) b`).Scan(&batches, &largest); err != nil || batches != 3 || largest != 128 {
		t.Fatalf("committed deletion transactions: batches=%d largest=%d err=%v", batches, largest, err)
	}
	t.Logf("ordinary UPDATE completed before releasing delete gate; ACCESS EXCLUSIVE=%v; 257 duplicates removed in %d committed batches, largest=%d", exclusive, batches, largest)
	runProjectMoveMigrations(t, ctx, pool, schema, "down", projectMoveMigrationVersions[5:6])
}

func TestProjectMoveRollbackResidualDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool, schema := projectMoveMigrationPool(t, ctx)
	seedProjectMoveRollbackDuplicates(t, ctx, pool, 1)
	_, release := gateProjectMoveDeletes(t, ctx, pool)
	// The gate synchronizes entry; pg_sleep is only injected slow execution,
	// not synchronization, and does not itself hit lock_timeout.
	if _, err := pool.Exec(ctx, `CREATE OR REPLACE FUNCTION gate_move_delete() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN PERFORM pg_advisory_xact_lock(73579,1); PERFORM pg_sleep(20); RETURN OLD; END $$`); err != nil {
		t.Fatal(err)
	}
	sql, err := os.ReadFile(realMigrationFiles(t, []string{"620_search_index_workspace_moves"}, "down")[0])
	if err != nil {
		t.Fatal(err)
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	done := make(chan error, 1)
	start := time.Now()
	go func() { _, err := conn.Exec(ctx, string(sql)); done <- err }()
	waitProjectMoveDeleteGate(t, ctx, pool, done)
	release()
	err = <-done
	elapsed := time.Since(start)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "57014" || elapsed > 15*time.Second {
		t.Fatalf("residual must hit its SQL execution deadline: elapsed=%s err=%v", elapsed, err)
	}
	// A failed simple-query transaction must be rolled back before releasing it.
	conn.Exec(context.Background(), "ROLLBACK")
	var rows int
	var marked, recorded bool
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM search_index_change),EXISTS(SELECT 1 FROM search_index_prune_mark),EXISTS(SELECT 1 FROM schema_migrations WHERE version='620_search_index_workspace_moves')`).Scan(&rows, &marked, &recorded); err != nil || rows != 2 || marked || !recorded {
		t.Fatalf("atomic timeout rows=%d marked=%v recorded=%v err=%v", rows, marked, recorded, err)
	}
	t.Logf("residual deadline SQLSTATE=%s elapsed=%s", pgErr.Code, elapsed)
	if _, err := pool.Exec(ctx, "DROP TRIGGER gate_move_delete ON search_index_change"); err != nil {
		t.Fatal(err)
	}
	runProjectMoveMigrations(t, ctx, pool, schema, "down", projectMoveMigrationVersions[5:])
}

func TestProjectMoveRollbackResidualOverflowRetries(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool, schema := projectMoveMigrationPool(t, ctx)
	seedProjectMoveRollbackDuplicates(t, ctx, pool, 129)
	opts := projectMoveRollbackOptions(t, schema)
	// Model moves committed after the hook: run SQL with no pre-cleanup.
	opts.Hooks = nil
	err := runMigrations(ctx, pool, opts)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "54000" {
		t.Fatalf("oversized residual must fail fast: %v", err)
	}
	var rows int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM search_index_change").Scan(&rows); err != nil || rows != 258 {
		t.Fatalf("overflow must be atomic rows=%d err=%v", rows, err)
	}
	runProjectMoveMigrations(t, ctx, pool, schema, "down", projectMoveMigrationVersions[5:])
	t.Log("129 residual duplicates rejected; registered-hook retry converged")
}

func TestProjectMoveRollbackCrashAfterHookRetries(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool, schema := projectMoveMigrationPool(t, ctx)
	q := db.New(pool)
	cursor, err := q.GetSearchIndexCurrentSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	seedProjectMoveRollbackDuplicates(t, ctx, pool, 257)
	opts := projectMoveRollbackOptions(t, schema)
	hook := opts.Hooks["620_search_index_workspace_moves"]
	if hook == nil {
		t.Fatal("620 down must register bounded pre-cleanup")
	}
	crash := errors.New("crash after committed hook, before SQL")
	opts.Hooks = map[string]preMigrationHook{"620_search_index_workspace_moves": func(ctx context.Context, pool *pgxpool.Pool) error {
		if err := hook(ctx, pool); err != nil {
			return err
		}
		return crash
	}}
	if err := runMigrations(ctx, pool, opts); !errors.Is(err, crash) {
		t.Fatalf("crash injection: %v", err)
	}
	var rows int
	var recorded bool
	var mark string
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM search_index_change),EXISTS(SELECT 1 FROM schema_migrations WHERE version='620_search_index_workspace_moves'),(SELECT pruned_through_xid::text FROM search_index_prune_mark)`).Scan(&rows, &recorded, &mark); err != nil || rows != 257 || !recorded {
		t.Fatalf("committed hook rows=%d ledger=%v mark=%s err=%v", rows, recorded, mark, err)
	}
	if pruned, err := q.SearchIndexSinceIsPruned(ctx, cursor); err != nil || !pruned {
		t.Fatalf("hook must invalidate discarded cursor pruned=%v err=%v", pruned, err)
	}
	// New moves between attempts create a fresh residual. A retry re-runs the
	// hook and cannot lower the existing prune watermark.
	if _, err := pool.Exec(ctx, `UPDATE issue SET workspace_id='00000000-0000-0000-0000-00000000000a'`); err != nil {
		t.Fatal(err)
	}
	runProjectMoveMigrations(t, ctx, pool, schema, "down", projectMoveMigrationVersions[5:])
	var monotone bool
	if err := pool.QueryRow(ctx, `SELECT pruned_through_xid >= $1::xid8 FROM search_index_prune_mark`, mark).Scan(&monotone); err != nil || !monotone {
		t.Fatalf("monotone=%v err=%v", monotone, err)
	}
	assertIndexReadyAndValid(t, pool, schema, "search_index_change_pkey", true)
	t.Log("committed hook survived injected crash; new-move retry preserved monotone prune mark and restored entity key")
}

func TestProjectMoveRollbackHookDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	pool, schema := projectMoveMigrationPool(t, ctx)
	seedProjectMoveRollbackDuplicates(t, ctx, pool, 1)
	_, release := gateProjectMoveDeletes(t, ctx, pool)
	if _, err := pool.Exec(ctx, `CREATE OR REPLACE FUNCTION gate_move_delete() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN PERFORM pg_advisory_xact_lock(73579,1); PERFORM pg_sleep(20); RETURN OLD; END $$`); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	start := time.Now()
	go func() { done <- runMigrations(ctx, pool, projectMoveRollbackOptions(t, schema)) }()
	waitProjectMoveDeleteGate(t, ctx, pool, done)
	release()
	err := <-done
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "57014" || time.Since(start) > 5*time.Second {
		t.Fatalf("hook must hit its own 2s execution cap: elapsed=%s err=%v", time.Since(start), err)
	}
	var rows int
	var marked, recorded bool
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM search_index_change),EXISTS(SELECT 1 FROM search_index_prune_mark),EXISTS(SELECT 1 FROM schema_migrations WHERE version='620_search_index_workspace_moves')`).Scan(&rows, &marked, &recorded); err != nil || rows != 2 || marked || !recorded {
		t.Fatalf("failed batch atomicity: rows=%d marked=%v ledger=%v err=%v", rows, marked, recorded, err)
	}
	t.Logf("hook execution deadline SQLSTATE=%s elapsed=%s; batch rolled back", pgErr.Code, time.Since(start))
	if _, err := pool.Exec(ctx, "DROP TRIGGER gate_move_delete ON search_index_change"); err != nil {
		t.Fatal(err)
	}
	runProjectMoveMigrations(t, ctx, pool, schema, "down", projectMoveMigrationVersions[5:])
}

func TestProjectMoveRollbackResidualCursors(t *testing.T) {
	for _, source := range []string{"00000000-0000-0000-0000-00000000000a", "00000000-0000-0000-0000-00000000000b"} {
		t.Run(source, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			pool, schema := projectMoveMigrationPool(t, ctx)
			target := "00000000-0000-0000-0000-00000000000a"
			if source == target {
				target = "00000000-0000-0000-0000-00000000000b"
			}
			q := db.New(pool)
			cursors := make([]string, 2)
			for i := range cursors {
				cursor, err := q.GetSearchIndexCurrentSnapshot(ctx)
				if err != nil {
					t.Fatal(err)
				}
				cursors[i] = cursor
			}
			if _, err := pool.Exec(ctx, "INSERT INTO issue VALUES(gen_random_uuid(),$1)", source); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, "UPDATE issue SET workspace_id=$1", target); err != nil {
				t.Fatal(err)
			}
			opts := projectMoveRollbackOptions(t, schema)
			opts.Hooks = nil // A move committed after a completed, empty hook.
			if err := runMigrations(ctx, pool, opts); err != nil {
				t.Fatal(err)
			}
			for _, cursor := range cursors {
				if pruned, err := q.SearchIndexSinceIsPruned(ctx, cursor); err != nil || !pruned {
					t.Fatalf("residual cursor rebuild: pruned=%v err=%v", pruned, err)
				}
			}
			runProjectMoveMigrations(t, ctx, pool, schema, "down", projectMoveMigrationVersions[5:6])
		})
	}
}

func TestProjectMoveRollbackPruneMarkMonotone(t *testing.T) {
	for _, residual := range []bool{false, true} {
		name := "hook"
		if residual {
			name = "residual"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			pool, schema := projectMoveMigrationPool(t, ctx)
			seedProjectMoveRollbackDuplicates(t, ctx, pool, 1)
			// Retention has already marked a newer committed xid than the move.
			if _, err := pool.Exec(ctx, `INSERT INTO search_index_prune_mark VALUES(true,pg_current_xact_id())`); err != nil {
				t.Fatal(err)
			}
			var before string
			if err := pool.QueryRow(ctx, `SELECT pruned_through_xid::text FROM search_index_prune_mark`).Scan(&before); err != nil {
				t.Fatal(err)
			}
			opts := projectMoveRollbackOptions(t, schema)
			if residual {
				opts.Hooks = nil
			}
			if err := runMigrations(ctx, pool, opts); err != nil {
				t.Fatal(err)
			}
			var after string
			if err := pool.QueryRow(ctx, `SELECT pruned_through_xid::text FROM search_index_prune_mark`).Scan(&after); err != nil || before != after {
				t.Fatalf("watermark decreased: before=%s after=%s err=%v", before, after, err)
			}
		})
	}
}
