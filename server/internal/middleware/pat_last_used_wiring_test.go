package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// countingRecorder counts Record calls so the PAT-miss tests can assert the
// middleware refreshes last_used exactly on a cache miss (and never on a hit).
type countingRecorder struct{ n atomic.Int64 }

func (c *countingRecorder) Record(pgtype.UUID) { c.n.Add(1) }
func (c *countingRecorder) count() int64       { return c.n.Load() }

// patRowDBTX is a minimal db.DBTX whose QueryRow satisfies
// GetPersonalAccessTokenByHash with a single valid, non-expired PAT row. Exec
// is a no-op (the middleware no longer writes on the request path). Query is
// unused by these tests.
type patRowDBTX struct {
	userID pgtype.UUID
	patID  pgtype.UUID
}

func (patRowDBTX) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (patRowDBTX) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }
func (d patRowDBTX) QueryRow(context.Context, string, ...any) pgx.Row {
	return patRow{userID: d.userID, patID: d.patID}
}

// patRow scans into the exact field order GetPersonalAccessTokenByHash uses:
// id, user_id, name, token_hash, token_prefix, expires_at, last_used_at,
// revoked, created_at. Only id and user_id need concrete values; the rest are
// left at their zero (NULL/false) which the middleware does not read.
type patRow struct {
	userID pgtype.UUID
	patID  pgtype.UUID
}

func (r patRow) Scan(dest ...any) error {
	for i, d := range dest {
		switch i {
		case 0:
			if p, ok := d.(*pgtype.UUID); ok {
				*p = r.patID
			}
		case 1:
			if p, ok := d.(*pgtype.UUID); ok {
				*p = r.userID
			}
		}
	}
	return nil
}

func testUUID(b byte) pgtype.UUID {
	var u pgtype.UUID
	u.Valid = true
	for i := range u.Bytes {
		u.Bytes[i] = b
	}
	return u
}

// TestAuth_PATMissRecordsLastUsed pins that a mul_ PAT cache miss (nil cache →
// always miss) resolves via the DB and records exactly one last_used mark.
func TestAuth_PATMissRecordsLastUsed(t *testing.T) {
	queries := db.New(patRowDBTX{userID: testUUID(0x11), patID: testUUID(0x22)})
	rec := &countingRecorder{}
	mw := Auth(queries, nil, nil, rec) // nil cache → guaranteed miss

	var gotUser string
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser = r.Header.Get("X-User-ID")
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("GET", "/api/me", nil)
	req.Header.Set("Authorization", "Bearer mul_miss_records_lastused")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if gotUser == "" {
		t.Fatalf("expected X-User-ID resolved from DB row")
	}
	if got := rec.count(); got != 1 {
		t.Fatalf("Record called %d times on cache miss, want 1", got)
	}
}

// TestDaemonAuth_PATMissRecordsLastUsed is the same assertion on the daemon
// middleware's mul_ fallback path.
func TestDaemonAuth_PATMissRecordsLastUsed(t *testing.T) {
	queries := db.New(patRowDBTX{userID: testUUID(0x33), patID: testUUID(0x44)})
	rec := &countingRecorder{}
	mw := DaemonAuth(queries, nil, nil, nil, rec)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("GET", "/api/daemon/workspaces", nil)
	req.Header.Set("Authorization", "Bearer mul_daemon_miss_records")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := rec.count(); got != 1 {
		t.Fatalf("Record called %d times on daemon cache miss, want 1", got)
	}
}

// interface guard: countingRecorder satisfies the recorder interface used by
// the middleware constructors.
var _ auth.PATLastUsedRecorder = (*countingRecorder)(nil)
