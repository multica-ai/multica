package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// patLookupDB serves GetPersonalAccessTokenByHash with a fixed
// last_used_at and reports every Exec, so a test can tell whether the
// middleware issued the last_used_at UPDATE at all.
type patLookupDB struct {
	t          *testing.T
	lastUsedAt pgtype.Timestamptz
	execs      chan string
}

func (d *patLookupDB) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	d.execs <- sql
	return pgconn.CommandTag{}, nil
}

func (d *patLookupDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected Query")
}

func (d *patLookupDB) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	if !strings.Contains(sql, "name: GetPersonalAccessTokenByHash ") {
		d.t.Errorf("unexpected QueryRow: %s", sql)
	}
	return patRow{lastUsedAt: d.lastUsedAt}
}

type patRow struct{ lastUsedAt pgtype.Timestamptz }

// Scan fills the columns the middleware reads, in the generated scan order.
func (r patRow) Scan(dest ...any) error {
	*dest[0].(*pgtype.UUID) = pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	*dest[1].(*pgtype.UUID) = pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	*dest[6].(*pgtype.Timestamptz) = r.lastUsedAt
	return nil
}

// TestPATLookup_SkipsUpdateForFreshStamp pins that, without a PAT cache,
// Auth and DaemonAuth issue the last_used_at UPDATE only when the stamp
// they read is stale, so a fresh stamp costs no statement per request.
func TestPATLookup_SkipsUpdateForFreshStamp(t *testing.T) {
	middlewares := map[string]func(*db.Queries) func(http.Handler) http.Handler{
		"Auth": func(q *db.Queries) func(http.Handler) http.Handler {
			return Auth(q, nil, nil, nil)
		},
		"DaemonAuth": func(q *db.Queries) func(http.Handler) http.Handler {
			return DaemonAuth(q, nil, nil, nil)
		},
	}
	now := time.Now()
	stamps := []struct {
		name       string
		lastUsedAt pgtype.Timestamptz
		wantUpdate bool
	}{
		{"never used", pgtype.Timestamptz{}, true},
		{"fresh", pgtype.Timestamptz{Time: now.Add(-10 * time.Second), Valid: true}, false},
		{"stale", pgtype.Timestamptz{Time: now.Add(-2 * time.Minute), Valid: true}, true},
	}

	for mwName, newMW := range middlewares {
		for _, st := range stamps {
			t.Run(mwName+"/"+st.name, func(t *testing.T) {
				fake := &patLookupDB{t: t, lastUsedAt: st.lastUsedAt, execs: make(chan string, 4)}
				handler := newMW(db.New(fake))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))

				req := httptest.NewRequest("GET", "/api/me", nil)
				req.Header.Set("Authorization", "Bearer mul_last_used_test")
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, req)
				if w.Code != http.StatusOK {
					t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
				}

				// The UPDATE runs in a goroutine: wait for it when it is
				// expected, and give a wrongly spawned one time to show.
				wait := 200 * time.Millisecond
				if st.wantUpdate {
					wait = 5 * time.Second
				}
				select {
				case sql := <-fake.execs:
					if !st.wantUpdate {
						t.Fatalf("fresh stamp still issued a statement: %s", sql)
					}
					if !strings.Contains(sql, "name: UpdatePersonalAccessTokenLastUsed ") {
						t.Fatalf("unexpected Exec: %s", sql)
					}
				case <-time.After(wait):
					if st.wantUpdate {
						t.Fatal("stale stamp issued no last_used_at UPDATE")
					}
				}
			})
		}
	}
}
