package handler

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestDaemonPollReadCancellation(t *testing.T) {
	id := uuid.NewString()
	for _, poll := range []struct {
		query   string
		request func() *http.Request
		call    func(*Handler, http.ResponseWriter, *http.Request)
	}{
		{"GetAgentTaskStatus", func() *http.Request {
			return withURLParam(newOwnerDaemonRequest(http.MethodGet, "/api/daemon/tasks/"+id+"/status", nil), "taskId", id)
		}, (*Handler).GetTaskStatus},
		{"GetAgentRuntime", func() *http.Request {
			return newOwnerDaemonRequest(http.MethodPost, "/api/daemon/heartbeat", map[string]any{"runtime_id": id})
		}, (*Handler).DaemonHeartbeat},
	} {
		t.Run(poll.query, func(t *testing.T) {
			cases := append(requestCancellationCases(http.StatusInternalServerError),
				requestCancellationCase{"live statement timeout", &pgconn.PgError{Code: "57014"}, false, false, 500},
				requestCancellationCase{"missing row during disconnect", pgx.ErrNoRows, true, false, 404})
			for _, tc := range cases {
				// Only a failed lookup takes the cancellation branch.
				if tc.after {
					continue
				}
				t.Run(tc.name, func(t *testing.T) {
					req := poll.request()
					ctx, cancel := context.WithCancel(req.Context())
					defer cancel()
					fault := requestCancellationDB{DBTX: testPool, query: poll.query, err: tc.err}
					if tc.cancel {
						fault.cancel = cancel
					}
					h := &Handler{Queries: db.New(fault)}
					assertRequestCancellationStatus(t, func(w http.ResponseWriter, r *http.Request) { poll.call(h, w, r) }, req.WithContext(ctx), tc.status)
					if tc.cancel && ctx.Err() != context.Canceled {
						t.Fatal("request did not reach the selected query")
					}
				})
			}
		})
	}
}

func TestDaemonTokenReadCancellation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		cancel bool
		want   int
	}{
		{"request and lookup cancelled", fmt.Errorf("lookup: %w", context.Canceled), true, 499},
		{"independent lookup cancellation", context.Canceled, false, 401},
		{"unknown token", pgx.ErrNoRows, false, 401},
		{"unknown token with disconnect", pgx.ErrNoRows, true, 401},
		{"lookup deadline with disconnect", context.DeadlineExceeded, true, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
			t.Cleanup(func() { slog.SetDefault(previous) })
			req := httptest.NewRequest(http.MethodGet, "/api/daemon/workspaces", nil)
			req.Header.Set("Authorization", "Bearer mul_test_lookup")
			ctx, cancel := context.WithCancel(req.Context())
			defer cancel()
			lookup := requestCancellationDB{DBTX: testPool, query: "GetPersonalAccessTokenByHash", err: tc.err}
			if tc.cancel {
				lookup.cancel = cancel
			}
			guard := middleware.DaemonAuth(db.New(lookup), nil, nil, nil)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Fatal("failed token lookup reached the next handler")
			}))
			assertRequestCancellationStatus(t, guard.ServeHTTP, req.WithContext(ctx), tc.want)
			if warned := strings.Contains(logs.String(), "daemon_auth: invalid PAT"); warned != (tc.want == http.StatusUnauthorized) {
				t.Fatalf("invalid PAT warning = %t for status %d", warned, tc.want)
			}
		})
	}
}
