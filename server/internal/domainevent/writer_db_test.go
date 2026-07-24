package domainevent

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// newDomainEventTestPool connects to the shared, already-migrated test DB. Like
// every other DB-backed test in this repo it SKIPS (never fails) when no
// database is reachable — the schema is migrated out of band by `make test`/CI.
func newDomainEventTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("database unavailable: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database unreachable: %v", err)
	}
	// Confirm the migration is applied; a DB pinned to an older schema should
	// skip, not fail with a missing-relation error.
	if _, err := pool.Exec(ctx, "SELECT 1 FROM domain_event LIMIT 0"); err != nil {
		pool.Close()
		t.Skipf("domain_event table missing (migrate up first): %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func countEventsForWorkspace(t *testing.T, pool *pgxpool.Pool, ws pgtype.UUID) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM domain_event WHERE workspace_id = $1`, ws).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// cleanupWorkspaceEvents deletes rows with context.Background so it survives a
// cancelled test context.
func cleanupWorkspaceEvents(pool *pgxpool.Pool, ws pgtype.UUID) {
	pool.Exec(context.Background(), `DELETE FROM domain_event WHERE workspace_id = $1`, ws)
}

func standInFactEvent(ws pgtype.UUID) Event {
	return IssueCreated(ws, pgUUID(uuid.New()), SystemActor(), IssueCreatedPayload{Status: "todo", Title: "stand-in fact"})
}

// A committed Write must persist exactly one row and stamp the root-event
// invariants: dispatch_status='pending', hop_count=0, correlation_id=id, a
// monotonic seq, and the exact payload.
func TestWriteCommitPersistsRootEvent(t *testing.T) {
	pool := newDomainEventTestPool(t)
	ctx := context.Background()
	queries := db.New(pool)

	ws := pgUUID(uuid.New())
	subj := pgUUID(uuid.New())
	actor := MemberActor(pgUUID(uuid.New()))
	t.Cleanup(func() { cleanupWorkspaceEvents(pool, ws) })

	evt := IssueStatusChanged(ws, subj, actor, IssueStatusChangedPayload{From: "todo", To: "done"})

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	row, err := Write(ctx, queries.WithTx(tx), evt)
	if err != nil {
		tx.Rollback(ctx)
		t.Fatalf("write: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	if got := countEventsForWorkspace(t, pool, ws); got != 1 {
		t.Fatalf("expected exactly 1 event, got %d", got)
	}
	if row.DispatchStatus != DispatchPending {
		t.Errorf("dispatch_status = %q, want %q", row.DispatchStatus, DispatchPending)
	}
	if row.HopCount != 0 {
		t.Errorf("hop_count = %d, want 0", row.HopCount)
	}
	if row.CorrelationID != row.ID {
		t.Errorf("root event correlation_id (%v) must equal id (%v)", row.CorrelationID, row.ID)
	}
	if row.Seq <= 0 {
		t.Errorf("seq should be a positive monotonic value, got %d", row.Seq)
	}
	if row.Type != TypeIssueStatusChanged {
		t.Errorf("type = %q", row.Type)
	}
}

// The outbox durability invariant (MUL-4332 §1 kill test #1): a crash after the
// domain write but before commit must leave NO event — the write and the event
// share one transaction, so an uncommitted event simply does not exist.
func TestWriteRollbackPersistsNothing(t *testing.T) {
	pool := newDomainEventTestPool(t)
	ctx := context.Background()
	queries := db.New(pool)

	ws := pgUUID(uuid.New())
	t.Cleanup(func() { cleanupWorkspaceEvents(pool, ws) })

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Write(ctx, queries.WithTx(tx), standInFactEvent(ws)); err != nil {
		tx.Rollback(ctx)
		t.Fatalf("write: %v", err)
	}
	// Simulate the process dying before commit.
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	if got := countEventsForWorkspace(t, pool, ws); got != 0 {
		t.Fatalf("rolled-back event must not persist, found %d rows", got)
	}
}

// WriteInTx must be all-or-nothing: if the caller's domain write or its own
// event insert fails, the fact written inside fn rolls back too.
func TestWriteInTxAtomicity(t *testing.T) {
	pool := newDomainEventTestPool(t)
	ctx := context.Background()
	queries := db.New(pool)

	t.Run("commit persists fact and event", func(t *testing.T) {
		ws := pgUUID(uuid.New())
		t.Cleanup(func() { cleanupWorkspaceEvents(pool, ws) })
		err := WriteInTx(ctx, pool, queries, func(qtx *db.Queries) ([]Event, error) {
			// Stand-in "domain write" inside the tx.
			if _, err := Write(ctx, qtx, standInFactEvent(ws)); err != nil {
				return nil, err
			}
			return []Event{standInFactEvent(ws)}, nil
		})
		if err != nil {
			t.Fatalf("WriteInTx: %v", err)
		}
		if got := countEventsForWorkspace(t, pool, ws); got != 2 {
			t.Fatalf("expected fact + event = 2 rows, got %d", got)
		}
	})

	t.Run("fn error rolls back the fact", func(t *testing.T) {
		ws := pgUUID(uuid.New())
		t.Cleanup(func() { cleanupWorkspaceEvents(pool, ws) })
		boom := errors.New("domain write failed")
		err := WriteInTx(ctx, pool, queries, func(qtx *db.Queries) ([]Event, error) {
			if _, err := Write(ctx, qtx, standInFactEvent(ws)); err != nil {
				return nil, err
			}
			return nil, boom
		})
		if !errors.Is(err, boom) {
			t.Fatalf("expected boom, got %v", err)
		}
		if got := countEventsForWorkspace(t, pool, ws); got != 0 {
			t.Fatalf("fn error must roll back the fact, found %d rows", got)
		}
	})

	t.Run("invalid event rolls back the fact", func(t *testing.T) {
		ws := pgUUID(uuid.New())
		t.Cleanup(func() { cleanupWorkspaceEvents(pool, ws) })
		err := WriteInTx(ctx, pool, queries, func(qtx *db.Queries) ([]Event, error) {
			if _, err := Write(ctx, qtx, standInFactEvent(ws)); err != nil {
				return nil, err
			}
			bad := standInFactEvent(ws)
			bad.Type = "issue.exploded" // fails validate in Write
			return []Event{bad}, nil
		})
		if err == nil {
			t.Fatal("expected invalid event to fail WriteInTx")
		}
		if got := countEventsForWorkspace(t, pool, ws); got != 0 {
			t.Fatalf("invalid event must roll back the fact, found %d rows", got)
		}
	})
}
