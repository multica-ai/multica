package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/multica-ai/multica/server/internal/inboxread"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestInboxReadPostgresTimeoutAndLocalSettings(t *testing.T) {
	conn, err := testPool.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	var before string
	if err := conn.QueryRow(context.Background(), "SHOW statement_timeout").Scan(&before); err != nil {
		t.Fatal(err)
	}
	g := inboxread.New(inboxread.Config{Concurrency: 1, StatementTimeout: 50 * time.Millisecond})
	start := time.Now()
	err = g.Run(context.Background(), "summary", conn, func(ctx context.Context, tx pgx.Tx) error {
		var timeout, readOnly string
		if err := tx.QueryRow(ctx, "SELECT current_setting('statement_timeout'), current_setting('transaction_read_only')").Scan(&timeout, &readOnly); err != nil {
			return err
		}
		if timeout != "50ms" || readOnly != "on" {
			t.Errorf("transaction settings = %s, %s", timeout, readOnly)
		}
		_, err := tx.Exec(ctx, "SELECT pg_sleep(2)")
		return err
	})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "57014" {
		t.Fatalf("database did not terminate slow SQL: %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("statement timeout took too long")
	}
	var after string
	if err := conn.QueryRow(context.Background(), "SHOW statement_timeout").Scan(&after); err != nil {
		t.Fatalf("connection not reusable after rollback: %v", err)
	}
	if after != before {
		t.Fatalf("transaction setting leaked: before %s after %s", before, after)
	}
	if err := g.Run(context.Background(), "list", conn, func(ctx context.Context, tx pgx.Tx) error { _, err := tx.Exec(ctx, "SELECT 1"); return err }); err != nil {
		t.Fatalf("permit/transaction leaked: %v", err)
	}
}

func TestInboxReadCallerCancellationKeepsDatabaseBudget(t *testing.T) {
	g := inboxread.New(inboxread.Config{Concurrency: 1, StatementTimeout: 50 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := g.Run(ctx, "list", testPool, func(ctx context.Context, tx pgx.Tx) error {
		cancel()
		if ctx.Err() != nil {
			t.Fatalf("caller canceled active SQL context: %v", ctx.Err())
		}
		_, err := tx.Exec(ctx, "SELECT pg_sleep(2)")
		return err
	})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "57014" {
		t.Fatalf("permit returned before database timeout: %v", err)
	}
}

func TestAllInboxReadHandlersShareCapacity(t *testing.T) {
	h := *testHandler
	h.InboxReads = inboxread.New(inboxread.Config{Concurrency: 1})
	started, finish, result := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		result <- h.InboxReads.Run(context.Background(), "summary", testPool, func(context.Context, pgx.Tx) error { close(started); <-finish; return nil })
	}()
	<-started
	defer func() {
		close(finish)
		if err := <-result; err != nil {
			t.Error(err)
		}
	}()
	for _, endpoint := range []struct {
		path    string
		handler http.HandlerFunc
	}{
		{"/api/inbox", h.ListInbox},
		{"/api/inbox/unread-count", h.CountUnreadInbox},
		{"/api/inbox/unread-summary", h.UnreadInboxSummary},
		{"/api/inbox/snoozed", h.ListSnoozedInbox},
		{"/api/inbox/archived", h.ListArchivedInbox},
		{"/api/inbox/archived/page", h.ListArchivedInboxPage},
		{"/api/inbox/archived/facets", h.GetArchivedInboxFacets},
	} {
		t.Run(endpoint.path, func(t *testing.T) {
			rec := testutil.Call(t, inboxWorkspaceHandler(endpoint.handler), inboxRequest(http.MethodGet, endpoint.path, testWorkspaceID)).Want(http.StatusServiceUnavailable)
			if rec.Header().Get("Retry-After") != "1" {
				t.Fatal("missing Retry-After")
			}
		})
	}
}

func TestInboxReadErrorContract(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
	}{
		{inboxread.ErrBusy, 503},
		{context.DeadlineExceeded, 503},
		{&pgconn.PgError{Code: "57014"}, 503},
		{&pgconn.PgError{Code: "42P01"}, 500},
		{context.Canceled, 499},
	} {
		rec := httptest.NewRecorder()
		writeInboxReadError(rec, tc.err, "failed to read inbox")
		if rec.Code != tc.status {
			t.Fatalf("%v => %d, want %d", tc.err, rec.Code, tc.status)
		}
		if tc.status == 503 && rec.Header().Get("Retry-After") != "1" {
			t.Fatal("missing Retry-After")
		}
		if tc.status == 499 && rec.Body.Len() != 0 {
			t.Fatal("wrote a body after cancellation")
		}
	}
}
