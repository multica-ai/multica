package main

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// countingRecorder counts Record calls for the patResolver wiring test.
type countingRecorder struct{ n atomic.Int64 }

func (c *countingRecorder) Record(pgtype.UUID) { c.n.Add(1) }
func (c *countingRecorder) count() int64       { return c.n.Load() }

// patRowDBTX returns one valid PAT row for GetPersonalAccessTokenByHash.
type patRowDBTX struct{ userID, patID pgtype.UUID }

func (patRowDBTX) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (patRowDBTX) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }
func (d patRowDBTX) QueryRow(context.Context, string, ...any) pgx.Row {
	return patRow{userID: d.userID, patID: d.patID}
}

type patRow struct{ userID, patID pgtype.UUID }

func (r patRow) Scan(dest ...any) error {
	if len(dest) > 0 {
		if p, ok := dest[0].(*pgtype.UUID); ok {
			*p = r.patID
		}
	}
	if len(dest) > 1 {
		if p, ok := dest[1].(*pgtype.UUID); ok {
			*p = r.userID
		}
	}
	return nil
}

func resolverUUID(b byte) pgtype.UUID {
	var u pgtype.UUID
	u.Valid = true
	for i := range u.Bytes {
		u.Bytes[i] = b
	}
	return u
}

// TestPATResolver_MissRecordsLastUsed pins the third call site: a WS-handshake
// PAT resolution that misses the cache (nil cache → always miss) resolves via
// the DB and records exactly one last_used mark.
func TestPATResolver_MissRecordsLastUsed(t *testing.T) {
	rec := &countingRecorder{}
	pr := &patResolver{
		queries:  db.New(patRowDBTX{userID: resolverUUID(0x55), patID: resolverUUID(0x66)}),
		cache:    nil, // nil PATCache is nil-safe and always reports a miss
		lastUsed: rec,
	}

	userID, ok := pr.ResolveToken(context.Background(), "mul_ws_resolver_miss")
	if !ok || userID == "" {
		t.Fatalf("expected resolve ok with non-empty user id, got ok=%v id=%q", ok, userID)
	}
	if got := rec.count(); got != 1 {
		t.Fatalf("Record called %d times on resolver miss, want 1", got)
	}
}

// errPATLookupDBTX makes GetPersonalAccessTokenByHash fail so ResolveToken
// returns not-ok and must NOT record. Reuses the package's existing errRow.
type errPATLookupDBTX struct{}

func (errPATLookupDBTX) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (errPATLookupDBTX) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }
func (errPATLookupDBTX) QueryRow(context.Context, string, ...any) pgx.Row {
	return errRow{err: pgx.ErrNoRows}
}

// TestPATResolver_InvalidTokenDoesNotRecord pins that a failed lookup neither
// resolves nor records.
func TestPATResolver_InvalidTokenDoesNotRecord(t *testing.T) {
	rec := &countingRecorder{}
	pr := &patResolver{queries: db.New(errPATLookupDBTX{}), cache: nil, lastUsed: rec}

	if _, ok := pr.ResolveToken(context.Background(), "mul_bad"); ok {
		t.Fatal("expected resolve to fail for an unknown token")
	}
	if got := rec.count(); got != 0 {
		t.Fatalf("Record called %d times for an invalid token, want 0", got)
	}
}

var _ auth.PATLastUsedRecorder = (*countingRecorder)(nil)
