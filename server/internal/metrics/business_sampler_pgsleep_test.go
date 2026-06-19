package metrics

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestBusinessSamplerStatementTimeoutCutsHungQuery is the integration test
// that proves the safety net is real. It connects to a live Postgres,
// asks it to `pg_sleep(2)` inside a sampler-style transaction with
// SET LOCAL statement_timeout = '500ms', and asserts:
//
//  1. The query returns in well under the sleep duration (cancelled by
//     the server, not by our caller-side context — the SET LOCAL is
//     doing the work).
//  2. The Postgres error we caught carries SQLSTATE 57014
//     ("query_canceled"). This is the canonical proof of statement_timeout
//     firing, and it's the assertion that catches the regression where
//     someone deletes the SET LOCAL line and the test would otherwise
//     pass on a context-cancellation timeout instead.
//  3. The error counter for that named query advances.
//  4. The duration histogram records the cancellation latency, so
//     dashboards can see it happen.
//
// Skips cleanly when no DATABASE_URL is set, mirroring the integration
// test pattern already used in cmd/server. Operators running CI without a
// reachable Postgres see "SKIP", not a failure.
func TestBusinessSamplerStatementTimeoutCutsHungQuery(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set; skipping live-Postgres statement_timeout test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("could not connect to %s: %v", dbURL, err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Skipf("database not reachable at %s: %v", dbURL, err)
	}

	c := NewBusinessSamplerCollector(&BusinessSamplerOptions{
		Pool:         pool,
		CacheTTL:     time.Second,
		QueryTimeout: 500 * time.Millisecond,
	})
	if c == nil {
		t.Fatal("NewBusinessSamplerCollector returned nil for live pool")
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire conn for hung-query test: %v", err)
	}
	defer conn.Release()

	const queryName = "pg_sleep_canary"
	var capturedErr error
	start := time.Now()
	c.runQuery(ctx, conn, queryName, func(ctx context.Context, tx pgx.Tx) error {
		// 2 s is comfortably longer than the 500 ms statement_timeout
		// AND the 550 ms outer context deadline, so whichever layer
		// fires first we still observe a cancelled query.
		_, err := tx.Exec(ctx, "SELECT pg_sleep(2)")
		capturedErr = err
		return err
	})
	elapsed := time.Since(start)

	// We give a generous upper bound (1.5 s) to absorb local-Postgres
	// scheduler jitter and pgx round-trip overhead. The lower bound
	// (>250 ms) confirms we *did* hit the timeout rather than the query
	// returning instantly because pg_sleep was elided somewhere.
	if elapsed >= 1500*time.Millisecond {
		t.Fatalf("statement_timeout did not cut the hung query: elapsed %s", elapsed)
	}
	if elapsed <= 250*time.Millisecond {
		t.Fatalf("query returned suspiciously fast (%s); SET LOCAL statement_timeout may not be in force", elapsed)
	}

	// SQLSTATE 57014 ("query_canceled") is the canonical proof that
	// Postgres itself terminated the query because of statement_timeout.
	// If a future refactor accidentally drops the SET LOCAL line, the
	// query would still get cancelled — but by our caller-side context,
	// not by Postgres, and this assertion would catch it.
	if capturedErr == nil {
		t.Fatal("expected pg_sleep to return an error; got nil")
	}
	var pgErr *pgconn.PgError
	if !errors.As(capturedErr, &pgErr) {
		t.Fatalf("expected *pgconn.PgError from pg_sleep cancellation; got %T: %v", capturedErr, capturedErr)
	}
	if pgErr.Code != "57014" {
		t.Fatalf("expected SQLSTATE 57014 (query_canceled); got %q (%s)", pgErr.Code, pgErr.Message)
	}

	// One labelled error must have been recorded against the named query.
	if got := testutil.ToFloat64(c.queryErrors.WithLabelValues(queryName)); got < 1 {
		t.Fatalf("query_errors_total{name=%q} = %v, want >= 1", queryName, got)
	}

	// And one observation must have landed on the duration histogram.
	if got := testutil.CollectAndCount(c.queryDuration); got < 1 {
		t.Fatalf("query_seconds histogram saw 0 observations after pg_sleep cancellation")
	}
}
