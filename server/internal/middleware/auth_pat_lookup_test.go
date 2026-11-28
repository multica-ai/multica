package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/multica-ai/multica/server/internal/auth"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/redis/go-redis/v9"
)

type patLookupErrorDB struct {
	db.DBTX
	err     error
	lookups int
}

func (d *patLookupErrorDB) QueryRow(context.Context, string, ...any) pgx.Row {
	d.lookups++
	return patLookupErrorRow{err: d.err}
}

func (*patLookupErrorDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	panic("unexpected database write after failed PAT lookup")
}

type patLookupErrorRow struct{ err error }

func (r patLookupErrorRow) Scan(...any) error { return r.err }

type patLookupErrorCache struct{ redis.UniversalClient }

func (*patLookupErrorCache) Get(context.Context, string) *redis.StringCmd {
	return redis.NewStringResult("", redis.Nil)
}

func (*patLookupErrorCache) Set(context.Context, string, any, time.Duration) *redis.StatusCmd {
	panic("unexpected cache write after failed PAT lookup")
}

func TestAuth_PATLookupErrors(t *testing.T) {
	dbErr := errors.New("database unavailable: private connection details")
	for _, tc := range []struct {
		name       string
		err        error
		wantStatus int
		wantBody   string
	}{
		{"missing row", pgx.ErrNoRows, http.StatusUnauthorized, "{\"error\":\"invalid token\"}\n"},
		{"wrapped missing row", fmt.Errorf("lookup: %w", pgx.ErrNoRows), http.StatusUnauthorized, "{\"error\":\"invalid token\"}\n"},
		{"database failure", dbErr, http.StatusServiceUnavailable, "{\"error\":\"pat lookup unavailable\"}\n"},
		{"wrapped database failure", fmt.Errorf("lookup: %w", dbErr), http.StatusServiceUnavailable, "{\"error\":\"pat lookup unavailable\"}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lookup := &patLookupErrorDB{err: tc.err}
			cache := auth.NewPATCache(&patLookupErrorCache{})
			handler := Auth(db.New(lookup), cache, nil, nil)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Error("next handler called after failed PAT lookup")
			}))
			req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
			req.Header.Set("Authorization", "Bearer mul_lookup_error_test_token")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tc.wantStatus)
			}
			if got := w.Body.String(); got != tc.wantBody {
				t.Errorf("body = %q, want %q", got, tc.wantBody)
			}
			if lookup.lookups != 1 {
				t.Errorf("PAT lookups = %d, want 1", lookup.lookups)
			}
		})
	}
}
