package issueactivitybackfill

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

func fixture(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	schema := fmt.Sprintf("issue_activity_backfill_%d_%d", time.Now().UnixNano(), rand.IntN(1_000_000))
	if _, err := pool.Exec(context.Background(), fmt.Sprintf(`CREATE SCHEMA %q`, schema)); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP SCHEMA %q CASCADE`, schema))
	})
	table := fmt.Sprintf("%q.issue", schema)
	if _, err := pool.Exec(context.Background(), fmt.Sprintf(`
CREATE TABLE %s (
    id UUID PRIMARY KEY,
    updated_at TIMESTAMPTZ NOT NULL,
    last_activity_at TIMESTAMPTZ
)`, table)); err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	return table
}

func TestBatchIsBoundedAndResumable(t *testing.T) {
	pool := testPool(t)
	defer pool.Close()
	ctx := context.Background()
	table := fixture(t, pool)
	if _, err := pool.Exec(ctx, fmt.Sprintf(`
INSERT INTO %s (id, updated_at, last_activity_at) VALUES
('00000000-0000-0000-0000-000000000001', '2026-01-01T00:00:00Z', NULL),
('00000000-0000-0000-0000-000000000002', '2026-01-02T00:00:00Z', NULL),
('00000000-0000-0000-0000-000000000003', '2026-01-03T00:00:00Z', '2026-02-01T00:00:00Z')`, table)); err != nil {
		t.Fatalf("seed fixture: %v", err)
	}

	first, err := Batch(ctx, pool, Options{BatchSize: 1, Table: table})
	if err != nil || first.Rows != 1 || first.LastID != "00000000-0000-0000-0000-000000000001" {
		t.Fatalf("first Batch = (%+v, %v), want one row ending at id 1", first, err)
	}
	remaining, err := CountRemaining(ctx, pool, table)
	if err != nil || remaining != 1 {
		t.Fatalf("CountRemaining = (%d, %v), want (1, nil)", remaining, err)
	}
	second, err := Batch(ctx, pool, Options{BatchSize: 10, AfterID: &first.LastID, Table: table})
	if err != nil || second.Rows != 1 || second.LastID != "00000000-0000-0000-0000-000000000002" {
		t.Fatalf("second Batch = (%+v, %v), want one row ending at id 2", second, err)
	}
	idempotent, err := Batch(ctx, pool, Options{BatchSize: 10, AfterID: &second.LastID, Table: table})
	if err != nil || idempotent.Rows != 0 || idempotent.LastID != "" {
		t.Fatalf("idempotent Batch = (%+v, %v), want empty result", idempotent, err)
	}

	var preserved time.Time
	if err := pool.QueryRow(ctx, fmt.Sprintf(`
SELECT last_activity_at FROM %s WHERE id = '00000000-0000-0000-0000-000000000003'`, table)).Scan(&preserved); err != nil {
		t.Fatalf("read pre-populated row: %v", err)
	}
	if want := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC); !preserved.Equal(want) {
		t.Fatalf("pre-populated activity changed to %s, want %s", preserved, want)
	}
}

func TestBatchWrapsToRecoverRowsSkippedByLock(t *testing.T) {
	pool := testPool(t)
	defer pool.Close()
	ctx := context.Background()
	table := fixture(t, pool)
	if _, err := pool.Exec(ctx, fmt.Sprintf(`
INSERT INTO %s (id, updated_at, last_activity_at) VALUES
('00000000-0000-0000-0000-000000000001', '2026-01-01T00:00:00Z', NULL),
('00000000-0000-0000-0000-000000000002', '2026-01-02T00:00:00Z', NULL),
('00000000-0000-0000-0000-000000000003', '2026-01-03T00:00:00Z', NULL)`, table)); err != nil {
		t.Fatalf("seed fixture: %v", err)
	}

	lockTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin lock transaction: %v", err)
	}
	if _, err := lockTx.Exec(ctx, fmt.Sprintf(`
SELECT id FROM %s WHERE id = '00000000-0000-0000-0000-000000000001' FOR UPDATE`, table)); err != nil {
		_ = lockTx.Rollback(ctx)
		t.Fatalf("lock first row: %v", err)
	}

	first, err := Batch(ctx, pool, Options{BatchSize: 2, Table: table})
	if err != nil || first.Rows != 2 || first.LastID != "00000000-0000-0000-0000-000000000003" {
		_ = lockTx.Rollback(ctx)
		t.Fatalf("locked-row Batch = (%+v, %v), want ids 2-3", first, err)
	}
	if err := lockTx.Rollback(ctx); err != nil {
		t.Fatalf("release first row: %v", err)
	}

	endOfPass, err := Batch(ctx, pool, Options{BatchSize: 2, AfterID: &first.LastID, Table: table})
	if err != nil || endOfPass.Rows != 0 {
		t.Fatalf("end-of-pass Batch = (%+v, %v), want empty result", endOfPass, err)
	}
	remaining, err := CountRemaining(ctx, pool, table)
	if err != nil || remaining != 1 {
		t.Fatalf("CountRemaining = (%d, %v), want skipped row", remaining, err)
	}
	recovered, err := Batch(ctx, pool, Options{BatchSize: 2, Table: table})
	if err != nil || recovered.Rows != 1 || recovered.LastID != "00000000-0000-0000-0000-000000000001" {
		t.Fatalf("wrapped Batch = (%+v, %v), want recovered id 1", recovered, err)
	}
}
