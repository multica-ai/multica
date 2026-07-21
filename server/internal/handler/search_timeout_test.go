package handler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsSearchStatementTimeout(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"57014 pgx error", &pgconn.PgError{Code: "57014", Message: "canceling statement due to statement timeout"}, true},
		{"57014 wrapped", errors.Join(errors.New("outer"), &pgconn.PgError{Code: "57014"}), true},
		{"different pg code", &pgconn.PgError{Code: "42P01"}, false},
		{"plain error", errors.New("boom"), false},
		{"context canceled", context.Canceled, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSearchStatementTimeout(tc.err); got != tc.want {
				t.Errorf("isSearchStatementTimeout(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestRunSearchQuery_StatementTimeoutFires exercises the safety net end
// to end against a live Postgres, proving that a deliberately hung
// pg_sleep query is cut off by SET LOCAL statement_timeout (SQLSTATE
// 57014) before the 3 s search cap could ever be reached. Skips
// gracefully if the database is not reachable — mirrors the pattern in
// handler_test.go so CI without a DB stays green.
func TestRunSearchQuery_StatementTimeoutFires(t *testing.T) {
	if testPool == nil {
		t.Skip("DATABASE_URL not set; skipping live-Postgres search timeout test")
	}
	// Override the search timeout for this test only: 200 ms is short
	// enough that pg_sleep(2) is guaranteed to hit it, and this keeps
	// the test snappy. We restore the constant via t.Cleanup so other
	// tests keep the production value.
	oldTimeout := searchStatementTimeout
	setSearchStatementTimeoutForTest(t, 200*time.Millisecond)
	t.Cleanup(func() { setSearchStatementTimeoutForTest(t, oldTimeout) })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	err := runSearchQuery(ctx, testPool, "SELECT pg_sleep(2)", nil, func(rows pgx.Rows) error {
		for rows.Next() {
			// nothing to scan — but iterate so pgx surfaces the
			// server error once the statement_timeout fires
		}
		return rows.Err()
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected statement_timeout error, got nil")
	}
	if !isSearchStatementTimeout(err) {
		t.Fatalf("expected SQLSTATE 57014 (statement_timeout), got: %v", err)
	}
	if elapsed > 1500*time.Millisecond {
		t.Errorf("statement_timeout did not cut hung query fast enough: elapsed=%s (want <1.5s)", elapsed)
	}
}

// setSearchStatementTimeoutForTest is a package-private hook used only
// by the live-Postgres timeout test above. Kept out of the public
// surface to prevent handlers from accidentally raising the cap.
func setSearchStatementTimeoutForTest(t *testing.T, v time.Duration) {
	t.Helper()
	searchStatementTimeoutOverride = v
}
