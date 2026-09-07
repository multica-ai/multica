package handler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// searchStatementTimeout bounds every /search request at the Postgres level.
//
// SearchProjects runs LOWER(col) LIKE '%pattern%' queries whose fast path
// depends on pg_bigm / pg_trgm GIN indexes (see migrations 032, 033, 036,
// 137–142). SearchIssues instead deliberately uses a candidate-first plan that
// scans each selected workspace's issues and comments once, avoiding repeated
// global GIN/hashed-subplan work at the cost of giving up content-index
// selectivity. Missing extensions or an unexpectedly large workspace can
// therefore still make either path slow enough that the frontend appears to
// hang ("搜索卡死没有任何反应", MUL-4059).
//
// The 3 s cap leaves margin above the production-observed candidate-first
// maximum (1.72 s in the MUL-7055 matrix) and is short enough that the
// frontend's implicit request timeout (browser default, ~30 s) never kicks in.
// On timeout the caller sees a 503 with a descriptive error rather than a
// stalled connection — SearchIssues / SearchProjects map SQLSTATE 57014 to
// http.StatusServiceUnavailable so the frontend can distinguish this from a
// generic 500.
const (
	searchStatementTimeout = 3 * time.Second

	// searchWorkMem removes the candidate-first comment aggregation spill seen
	// at Postgres' 4 MB default. work_mem is a per-node ceiling, not a
	// reservation: the production plan's material sort used about 9 MB. At the
	// default 25-connection pool ceiling, 25 simultaneous searches would use
	// about 225 MB for that node; the theoretical per-node ceiling is 1.6 GB.
	// Keep this transaction-local so unrelated pooled work retains its configured
	// database default, and keep the 3 s timeout above as the lifetime bound.
	searchWorkMem    = "64MB"
	searchWorkMemSQL = "SET LOCAL work_mem = '" + searchWorkMem + "'"
)

// searchStatementTimeoutOverride, when non-zero, replaces
// searchStatementTimeout for the duration of a test. Never read outside
// of the runSearchQuery hot path — see search_timeout_test.go.
var searchStatementTimeoutOverride time.Duration

func effectiveSearchStatementTimeout() time.Duration {
	if searchStatementTimeoutOverride > 0 {
		return searchStatementTimeoutOverride
	}
	return searchStatementTimeout
}

// runSearchQuery executes a search SQL query inside a short-lived read-only
// transaction with SET LOCAL statement_timeout as the safety net. rowsFn
// receives each pgx.Rows result and is responsible for scanning /
// accumulating results before returning; runSearchQuery handles
// commit / rollback and returns the first error encountered.
//
// tx uses IsoLevel ReadCommitted (Postgres default) and AccessMode ReadOnly
// so a stuck search cannot hold row locks against writers.
func runSearchQuery(
	ctx context.Context,
	txStarter txStarter,
	sql string,
	args []any,
	rowsFn func(pgx.Rows) error,
) error {
	tx, err := txStarter.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin search tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			// Best-effort rollback with a fresh context so a caller
			// cancellation still lets the connection go back clean.
			_ = tx.Rollback(context.Background())
		}
	}()

	// SET LOCAL is transaction-scoped, so pgxpool can safely hand this
	// connection back out after COMMIT without search-specific settings leaking
	// to unrelated queries.
	timeoutMs := int(effectiveSearchStatementTimeout() / time.Millisecond)
	if _, err := tx.Exec(ctx, fmt.Sprintf("SET LOCAL statement_timeout = %d", timeoutMs)); err != nil {
		return fmt.Errorf("set search statement_timeout: %w", err)
	}
	if _, err := tx.Exec(ctx, searchWorkMemSQL); err != nil {
		return fmt.Errorf("set search work_mem: %w", err)
	}
	// The read-only mode is applied here rather than via TxOptions so we
	// keep the txStarter interface signature (Begin only) intact. It's
	// belt-and-suspenders — the search queries only SELECT anyway.
	if _, err := tx.Exec(ctx, "SET LOCAL transaction_read_only = on"); err != nil {
		return fmt.Errorf("set search transaction_read_only: %w", err)
	}

	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return err
	}
	// Close rows before commit so pgx does not complain about a busy
	// connection during Commit.
	if err := rowsFn(rows); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	if err := tx.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}

// isSearchStatementTimeout reports whether err is the canonical Postgres
// query_canceled error (SQLSTATE 57014). Both `SET LOCAL statement_timeout`
// firing and a client-side context cancellation surface as 57014 — the two
// are indistinguishable from the client side, which is intentional in the
// pgx layer.
func isSearchStatementTimeout(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "57014"
	}
	return false
}
