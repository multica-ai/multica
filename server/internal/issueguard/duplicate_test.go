package issueguard

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type catalogFailureDB struct {
	err error
}

func (f catalogFailureDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag("SELECT 1"), nil
}

func (f catalogFailureDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, f.err
}

func (f catalogFailureDB) QueryRow(context.Context, string, ...any) pgx.Row {
	return catalogFailureRow{err: f.err}
}

type catalogFailureRow struct {
	err error
}

func (r catalogFailureRow) Scan(...any) error { return r.err }

func TestLockAndFindActiveDuplicatePropagatesStatusCatalogFailure(t *testing.T) {
	catalogErr := errors.New("status catalog unavailable")
	q := db.New(catalogFailureDB{err: catalogErr})

	_, found, err := LockAndFindActiveDuplicate(
		context.Background(), q, testUUID(1), pgtype.UUID{}, pgtype.UUID{}, "duplicate title", false,
	)
	if !errors.Is(err, catalogErr) || found {
		t.Fatalf("LockAndFindActiveDuplicate = found %v, err %v; want false, catalog error", found, err)
	}
}

func TestLockAndFindRecentAutopilotDuplicatePropagatesStatusCatalogFailure(t *testing.T) {
	catalogErr := errors.New("status catalog unavailable")
	q := db.New(catalogFailureDB{err: catalogErr})

	_, found, err := LockAndFindRecentAutopilotDuplicate(
		context.Background(), q, testUUID(1), testUUID(2), pgtype.UUID{}, "duplicate title", time.Hour,
	)
	if !errors.Is(err, catalogErr) || found {
		t.Fatalf("LockAndFindRecentAutopilotDuplicate = found %v, err %v; want false, catalog error", found, err)
	}
}

func testUUID(lastByte byte) pgtype.UUID {
	var value [16]byte
	value[len(value)-1] = lastByte
	return pgtype.UUID{Bytes: value, Valid: true}
}
