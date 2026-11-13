package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// searchFailureTx injects a query failure after the transaction-local settings
// have been applied, including cancellation while the search is in flight.
type searchFailureTx struct {
	pgx.Tx
	err          error
	cancel       context.CancelFunc
	rows         pgx.Rows
	commitErr    error
	commitCancel context.CancelFunc
}

func (tx searchFailureTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (tx searchFailureTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	if tx.rows != nil {
		return tx.rows, nil
	}
	if tx.cancel != nil {
		tx.cancel()
	}
	return nil, tx.err
}

func (tx searchFailureTx) Rollback(context.Context) error { return nil }

func (tx searchFailureTx) Commit(context.Context) error {
	if tx.commitCancel != nil {
		tx.commitCancel()
	}
	return tx.commitErr
}

type searchFailureTxStarter struct{ tx searchFailureTx }

func (s searchFailureTxStarter) Begin(context.Context) (pgx.Tx, error) { return s.tx, nil }

func TestSearchRequestErrorClassification(t *testing.T) {
	for _, endpoint := range []string{"issues", "projects"} {
		t.Run(endpoint, func(t *testing.T) {
			for _, tc := range []struct {
				name       string
				err        error
				cancel     bool
				wantStatus int
			}{
				{"client cancellation", context.Canceled, true, 499},
				{"wrapped client cancellation", fmt.Errorf("scan: %w", context.Canceled), true, 499},
				{"postgres cancellation after disconnect", &pgconn.PgError{Code: "57014"}, true, 499},
				{"database statement timeout", &pgconn.PgError{Code: "57014"}, false, http.StatusServiceUnavailable},
				{"database failure", &pgconn.PgError{Code: "42P01"}, false, http.StatusInternalServerError},
				{"unexpected cancellation with live request", context.Canceled, false, http.StatusInternalServerError},
				{"database failure concurrent with disconnect", &pgconn.PgError{Code: "42P01"}, true, http.StatusInternalServerError},
			} {
				t.Run(tc.name, func(t *testing.T) {
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					tx := searchFailureTx{err: tc.err}
					if tc.cancel {
						tx.cancel = cancel
					}
					h := *testHandler
					h.TxStarter = searchFailureTxStarter{tx: tx}
					assertSearchStatus(t, &h, endpoint, ctx, tc.wantStatus)
				})
			}
		})
	}
}

func TestSearchAlreadyCanceledRequest(t *testing.T) {
	for _, tc := range []struct {
		name    string
		path    string
		handler http.HandlerFunc
	}{
		{"issue status lookup", "/api/issues/search?q=needle", testHandler.SearchIssues},
		{"issue search transaction", "/api/issues/search?q=needle&include_closed=true", testHandler.SearchIssues},
		{"project search transaction", "/api/projects/search?q=needle", testHandler.SearchProjects},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			response := testutil.Call(t, tc.handler, newRequest(http.MethodGet, tc.path, nil).WithContext(ctx)).Want(499)
			if response.Body.Len() != 0 {
				t.Fatalf("already canceled search wrote a response body: %s", response.Body.String())
			}
		})
	}
}

func assertSearchStatus(t *testing.T, h *Handler, endpoint string, ctx context.Context, status int) {
	t.Helper()
	registry := metrics.NewRegistry(metrics.RegistryOptions{})
	router := chi.NewRouter()
	router.Use(registry.HTTP.Middleware)
	path := "/api/" + endpoint + "/search"
	if endpoint == "issues" {
		router.Get(path, h.SearchIssues)
	} else {
		router.Get(path, h.SearchProjects)
	}
	req := newRequest(http.MethodGet, path+"?q=SearchCancellationEnrichment&include_closed=true", nil).WithContext(ctx)
	response := testutil.Call(t, router.ServeHTTP, req).Want(status)
	if status == 499 && response.Body.Len() != 0 {
		t.Fatalf("disconnected search wrote a response body: %s", response.Body.String())
	}

	rec := httptest.NewRecorder()
	metrics.NewHandler(registry.Gatherer).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rec.Body.String()
	want := `multica_http_requests_total{method="GET",route="` + path + `",status="` + strconv.Itoa(status) + `"} 1`
	if !strings.Contains(body, want) {
		t.Fatalf("missing request classification %q in metrics:\n%s", want, body)
	}
	if status == 499 {
		for _, incorrect := range []string{`status="200"`, `status="500"`, `status="503"`} {
			if strings.Contains(body, incorrect) {
				t.Fatalf("canceled search counted as %s:\n%s", incorrect, body)
			}
		}
	}
}

type searchCancellationRows struct {
	pgx.Rows
	cancel context.CancelFunc
	err    error
}

func (rows searchCancellationRows) Next() bool {
	if rows.cancel != nil {
		rows.cancel()
	}
	return false
}

func (rows searchCancellationRows) Err() error { return rows.err }
func (rows searchCancellationRows) Close()     {}

func TestSearchCancellationAfterQuery(t *testing.T) {
	for _, endpoint := range []string{"issues", "projects"} {
		t.Run(endpoint, func(t *testing.T) {
			for _, stage := range []string{"reading rows", "commit error", "successful commit"} {
				t.Run(stage, func(t *testing.T) {
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					tx := searchFailureTx{rows: searchCancellationRows{}}
					switch stage {
					case "reading rows":
						tx.rows = searchCancellationRows{cancel: cancel, err: context.Canceled}
					case "commit error":
						tx.commitCancel = cancel
						tx.commitErr = context.Canceled
					case "successful commit":
						tx.commitCancel = cancel
					}
					h := *testHandler
					h.TxStarter = searchFailureTxStarter{tx: tx}
					assertSearchStatus(t, &h, endpoint, ctx, 499)
				})
			}
		})
	}
}

// These wrappers cancel only after a real supplementary read completes. The
// primary search and commit succeed, so their error branches cannot catch it.
type searchEnrichmentDB struct {
	db.DBTX
	cancel   context.CancelFunc
	canceled bool
}

func (d *searchEnrichmentDB) stop() {
	d.canceled = true
	d.cancel()
}

func (d *searchEnrichmentDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	row := d.DBTX.QueryRow(ctx, sql, args...)
	if strings.HasPrefix(sql, "-- name: GetWorkspace :one") {
		return searchCancelRow{Row: row, cancel: d.stop}
	}
	return row
}

func (d *searchEnrichmentDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	rows, err := d.DBTX.Query(ctx, sql, args...)
	if err == nil && strings.HasPrefix(sql, "-- name: GetProjectResourceCounts :many") {
		return searchCancelAfterCloseRows{Rows: rows, cancel: d.stop}, nil
	}
	return rows, err
}

type searchCancelRow struct {
	pgx.Row
	cancel context.CancelFunc
}

func (row searchCancelRow) Scan(dest ...any) error {
	err := row.Row.Scan(dest...)
	if err == nil {
		row.cancel()
	}
	return err
}

type searchCancelAfterCloseRows struct {
	pgx.Rows
	cancel context.CancelFunc
}

func (rows searchCancelAfterCloseRows) Close() {
	rows.Rows.Close()
	rows.cancel()
}

func TestSearchCancellationDuringEnrichment(t *testing.T) {
	for _, endpoint := range []string{"issues", "projects"} {
		t.Run(endpoint, func(t *testing.T) {
			if endpoint == "issues" {
				dbfx.Issue(t, "SearchCancellationEnrichment issue")
			} else {
				dbfx.Project(t, "SearchCancellationEnrichment project")
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			d := &searchEnrichmentDB{DBTX: testPool, cancel: cancel}
			h := *testHandler
			h.Queries = db.New(d)
			assertSearchStatus(t, &h, endpoint, ctx, 499)
			if !d.canceled {
				t.Fatal("search never reached the supplementary read")
			}
		})
	}
}

type searchBlockingTxStarter struct{ txStarter }

func (s searchBlockingTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.txStarter.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return searchBlockingTx{Tx: tx}, nil
}

type searchBlockingTx struct{ pgx.Tx }

const cancellationSleepQuery = "SELECT pg_sleep(10) /* search cancellation regression */"

func (tx searchBlockingTx) Query(ctx context.Context, _ string, _ ...any) (pgx.Rows, error) {
	return tx.Tx.Query(ctx, cancellationSleepQuery)
}

func TestSearchCancellationInFlightPostgres(t *testing.T) {
	for _, endpoint := range []string{"issues", "projects"} {
		t.Run(endpoint, func(t *testing.T) {
			conn, err := testPool.Acquire(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Release()
			pid := conn.Conn().PgConn().PID()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			watchCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
			defer stop()
			observed := make(chan error, 1)
			go func() {
				defer cancel()
				ticker := time.NewTicker(10 * time.Millisecond)
				defer ticker.Stop()
				for {
					var running bool
					err := testPool.QueryRow(watchCtx, `SELECT EXISTS (
						SELECT 1 FROM pg_stat_activity WHERE pid = $1 AND state = 'active'
						AND wait_event = 'PgSleep' AND query = $2
					)`, pid, cancellationSleepQuery).Scan(&running)
					if err != nil || running {
						observed <- err
						return
					}
					select {
					case <-ticker.C:
					case <-watchCtx.Done():
						observed <- watchCtx.Err()
						return
					}
				}
			}()
			h := *testHandler
			h.TxStarter = searchBlockingTxStarter{txStarter: conn}
			assertSearchStatus(t, &h, endpoint, ctx, 499)
			if err := <-observed; err != nil {
				t.Fatalf("did not observe a real in-flight PostgreSQL query: %v", err)
			}
		})
	}
}
