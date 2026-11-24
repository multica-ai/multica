package delegatedrecoverybackfill

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Skipf("connect to database: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Skipf("database not reachable: %v", err)
	}
	return pool
}

// fixture mirrors only the columns the walk reads, in a throwaway schema, so
// the test never touches the real comment or agent_task_queue tables.
func fixture(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	ctx := context.Background()
	schema := fmt.Sprintf("delegated_recovery_backfill_%d_%d", time.Now().UnixNano(), rand.IntN(1_000_000))
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %q`, schema)); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP SCHEMA %q CASCADE`, schema))
	})
	if _, err := pool.Exec(ctx, fmt.Sprintf(`
CREATE TABLE %q.comment (
    id UUID PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    author_type TEXT NOT NULL,
    type TEXT NOT NULL,
    source_task_id UUID,
    recovery_settled_at TIMESTAMPTZ
);
CREATE TABLE %q.agent_task_queue (
    id UUID PRIMARY KEY,
    status TEXT NOT NULL,
    delivered_comment_ids UUID[] NOT NULL DEFAULT '{}'
)`, schema, schema)); err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	return schema
}

func id(n int) string {
	return fmt.Sprintf("00000000-0000-0000-0000-%012d", n)
}

func seed(t *testing.T, pool *pgxpool.Pool, schema, comments, tasks string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), fmt.Sprintf(
		`INSERT INTO %q.comment (id, created_at, author_type, type, source_task_id) VALUES %s`, schema, comments)); err != nil {
		t.Fatalf("seed comments: %v", err)
	}
	if tasks == "" {
		return
	}
	if _, err := pool.Exec(context.Background(), fmt.Sprintf(
		`INSERT INTO %q.agent_task_queue (id, status, delivered_comment_ids) VALUES %s`, schema, tasks)); err != nil {
		t.Fatalf("seed tasks: %v", err)
	}
}

func unsettled(t *testing.T, pool *pgxpool.Pool, schema string) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(), fmt.Sprintf(`
SELECT id::text FROM %q.comment
WHERE author_type = 'system' AND type = 'progress_update'
  AND source_task_id IS NOT NULL AND recovery_settled_at IS NULL
ORDER BY id`, schema))
	if err != nil {
		t.Fatalf("read unsettled: %v", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var got string
		if err := rows.Scan(&got); err != nil {
			t.Fatalf("scan unsettled: %v", err)
		}
		ids = append(ids, got)
	}
	return ids
}

// The walk retires only recovery comments a terminal task already received.
// Everything else — a live receipt, a comment nobody delivered, an ordinary
// comment — has to survive, because those are exactly the rows the sweeper
// still needs to see.
func TestBatchSettlesOnlyTerminallyDeliveredRecoveries(t *testing.T) {
	pool := testPool(t)
	defer pool.Close()
	ctx := context.Background()
	schema := fixture(t, pool)

	seed(t, pool, schema, fmt.Sprintf(`
('%s', '2026-01-01T00:00:01Z', 'system', 'progress_update', '%s'),
('%s', '2026-01-01T00:00:02Z', 'system', 'progress_update', '%s'),
('%s', '2026-01-01T00:00:03Z', 'system', 'progress_update', '%s'),
('%s', '2026-01-01T00:00:04Z', 'member', 'comment',         NULL)`,
		id(1), id(90), // delivered to a completed task -> settle
		id(2), id(90), // delivered to a still-running task -> keep
		id(3), id(90), // never delivered -> keep
		id(4)), fmt.Sprintf(`
('%s', 'completed', ARRAY['%s']::uuid[]),
('%s', 'running',   ARRAY['%s']::uuid[])`,
		id(80), id(1),
		id(81), id(2)))

	result, err := Batch(ctx, pool, Options{Schema: schema})
	if err != nil {
		t.Fatalf("Batch: %v", err)
	}
	if result.Scanned != 3 || result.Settled != 1 {
		t.Fatalf("batch = scanned %d settled %d, want 3/1", result.Scanned, result.Settled)
	}
	if result.Last == nil || result.Last.ID != id(3) {
		t.Fatalf("watermark = %v, want the highest SCANNED candidate %q", result.Last, id(3))
	}
	if got := unsettled(t, pool, schema); len(got) != 2 || got[0] != id(2) || got[1] != id(3) {
		t.Fatalf("unsettled = %v, want the live receipt and the undelivered comment", got)
	}
	remaining, err := CountRemaining(ctx, pool, schema)
	if err != nil {
		t.Fatalf("CountRemaining: %v", err)
	}
	if remaining != 2 {
		t.Fatalf("CountRemaining = %d, want 2", remaining)
	}
}

// The watermark has to advance over candidates the walk deliberately leaves
// alone. Keyed on settled rows instead, a batch holding only untouchable
// candidates would hand back the same watermark forever and never terminate.
func TestBatchWatermarkAdvancesPastUnsettleableCandidates(t *testing.T) {
	pool := testPool(t)
	defer pool.Close()
	ctx := context.Background()
	schema := fixture(t, pool)

	seed(t, pool, schema, fmt.Sprintf(`
('%s', '2026-01-01T00:00:01Z', 'system', 'progress_update', '%s'),
('%s', '2026-01-01T00:00:02Z', 'system', 'progress_update', '%s'),
('%s', '2026-01-01T00:00:03Z', 'system', 'progress_update', '%s')`,
		id(1), id(90),
		id(2), id(90),
		id(3), id(90)), fmt.Sprintf(`
('%s', 'cancelled', ARRAY['%s']::uuid[])`, id(80), id(3)))

	var after *Cursor
	var scanned, settled int64
	batches := 0
	for batches < 5 {
		result, err := Batch(ctx, pool, Options{BatchSize: 1, After: after, Schema: schema})
		if err != nil {
			t.Fatalf("batch %d: %v", batches, err)
		}
		if result.Scanned == 0 {
			break
		}
		batches++
		scanned += result.Scanned
		settled += result.Settled
		after = result.Last
	}
	if batches != 3 {
		t.Fatalf("walk took %d batches, want one per candidate (3)", batches)
	}
	if scanned != 3 || settled != 1 {
		t.Fatalf("walk = scanned %d settled %d, want 3/1", scanned, settled)
	}
}

// The per-batch receipt scan reads every terminal task's delivered ids, not
// just the ones in this batch. Those extra ids must be discarded: settling a
// candidate the current batch never examined would move it behind the keyset
// watermark without the walk ever having looked at it.
func TestBatchSettlesNothingOutsideTheCurrentBatch(t *testing.T) {
	pool := testPool(t)
	defer pool.Close()
	ctx := context.Background()
	schema := fixture(t, pool)

	seed(t, pool, schema, fmt.Sprintf(`
('%s', '2026-01-01T00:00:01Z', 'system', 'progress_update', '%s'),
('%s', '2026-01-01T00:00:02Z', 'system', 'progress_update', '%s')`,
		id(1), id(90),
		id(2), id(90)), fmt.Sprintf(`
('%s', 'completed', ARRAY['%s','%s']::uuid[])`, id(80), id(1), id(2)))

	// One candidate per batch, while the single covering task carries both.
	result, err := Batch(ctx, pool, Options{BatchSize: 1, Schema: schema})
	if err != nil {
		t.Fatalf("Batch: %v", err)
	}
	if result.Scanned != 1 || result.Settled != 1 {
		t.Fatalf("batch = scanned %d settled %d, want 1/1", result.Scanned, result.Settled)
	}
	if got := unsettled(t, pool, schema); len(got) != 1 || got[0] != id(2) {
		t.Fatalf("unsettled = %v, want only the candidate this batch never examined (%s)", got, id(2))
	}
}

// The keyset must be (created_at, id) to match
// idx_comment_delegated_failure_unsettled. Ordering by id alone would walk a
// different sequence than the index provides, so a run resumed from a
// checkpoint could skip candidates entirely.
func TestBatchOrdersByCreatedAtNotID(t *testing.T) {
	pool := testPool(t)
	defer pool.Close()
	ctx := context.Background()
	schema := fixture(t, pool)

	// created_at order is the reverse of id order.
	seed(t, pool, schema, fmt.Sprintf(`
('%s', '2026-01-01T00:00:03Z', 'system', 'progress_update', '%s'),
('%s', '2026-01-01T00:00:02Z', 'system', 'progress_update', '%s'),
('%s', '2026-01-01T00:00:01Z', 'system', 'progress_update', '%s')`,
		id(1), id(90),
		id(2), id(90),
		id(3), id(90)), "")

	var seen []string
	var after *Cursor
	for range 3 {
		result, err := Batch(ctx, pool, Options{BatchSize: 1, After: after, Schema: schema})
		if err != nil {
			t.Fatalf("Batch: %v", err)
		}
		if result.Scanned == 0 {
			break
		}
		seen = append(seen, result.Last.ID)
		after = result.Last
	}
	want := []string{id(3), id(2), id(1)}
	if len(seen) != len(want) {
		t.Fatalf("walked %v, want %v", seen, want)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("walked %v, want created_at order %v", seen, want)
		}
	}
}
