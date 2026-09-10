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

func TestParseSearchWorkMemMB(t *testing.T) {
	tests := []struct {
		name   string
		raw    string
		want   int
		wantOK bool
	}{
		{name: "unset uses default", want: defaultSearchWorkMemMB, wantOK: true},
		{name: "disable local override", raw: "0", want: 0, wantOK: true},
		{name: "lower cap", raw: " 16 ", want: 16, wantOK: true},
		{name: "default explicitly", raw: "64", want: 64, wantOK: true},
		{name: "negative rejected", raw: "-1", want: defaultSearchWorkMemMB},
		{name: "higher cap rejected", raw: "65", want: defaultSearchWorkMemMB},
		{name: "unit suffix rejected", raw: "16MB", want: defaultSearchWorkMemMB},
		{name: "invalid rejected", raw: "large", want: defaultSearchWorkMemMB},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseSearchWorkMemMB(tt.raw)
			if got != tt.want || ok != tt.wantOK {
				t.Fatalf("parseSearchWorkMemMB(%q) = (%d, %t), want (%d, %t)", tt.raw, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

// TestRunSearchQuery_StatementTimeoutFires exercises the safety net end
// to end against a live Postgres, proving that a deliberately hung
// pg_sleep query is cut off by SET LOCAL statement_timeout (SQLSTATE
// 57014) before the production search cap could ever be reached. Skips
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

func TestRunSearchQuery_WorkMemIsTransactionLocal(t *testing.T) {
	if testPool == nil {
		t.Skip("DATABASE_URL not set; skipping live-Postgres search work_mem test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := testPool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire dedicated connection: %v", err)
	}
	defer conn.Release()

	var before string
	if err := conn.QueryRow(ctx, "SHOW work_mem").Scan(&before); err != nil {
		t.Fatalf("read baseline work_mem: %v", err)
	}

	var during string
	err = runSearchQuery(ctx, conn, "SELECT current_setting('work_mem')", nil, func(rows pgx.Rows) error {
		if !rows.Next() {
			return rows.Err()
		}
		return rows.Scan(&during)
	})
	if err != nil {
		t.Fatalf("run search query: %v", err)
	}
	wantDuring := before
	if configured := searchWorkMemValue(); configured != "" {
		wantDuring = configured
	}
	if during != wantDuring {
		t.Fatalf("work_mem during search = %q, want %q", during, wantDuring)
	}

	var after string
	if err := conn.QueryRow(ctx, "SHOW work_mem").Scan(&after); err != nil {
		t.Fatalf("read work_mem after search: %v", err)
	}
	if after != before {
		t.Fatalf("transaction-local work_mem leaked: before=%q after=%q", before, after)
	}
}

// setSearchStatementTimeoutForTest is a package-private hook used only
// by the live-Postgres timeout test above. Kept out of the public
// surface to prevent handlers from accidentally raising the cap.
func setSearchStatementTimeoutForTest(t *testing.T, v time.Duration) {
	t.Helper()
	searchStatementTimeoutOverride = v
}
