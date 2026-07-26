package taskmandate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type mandateDB struct{ row pgx.Row }

func (d mandateDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}
func (d mandateDB) QueryRow(context.Context, string, ...any) pgx.Row { return d.row }

type mandateRow struct {
	expiresAt time.Time
	contains  bool
	err       error
}

func (r mandateRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*(dest[0].(*time.Time)) = r.expiresAt
	*(dest[1].(*bool)) = r.contains
	return nil
}

func validUUID() pgtype.UUID {
	return pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
}

func TestErrorsRemainFailClosedAndDistinct(t *testing.T) {
	if errors.Is(ErrMissing, ErrExpired) || errors.Is(ErrExpired, ErrToolDeny) {
		t.Fatal("mandate denial reasons must stay distinct")
	}
}

func TestAuthorizeRejectsExpiredMandateAtCallTime(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	store := NewStoreDB(mandateDB{row: mandateRow{expiresAt: now.Add(-time.Second), contains: true}})
	store.now = func() time.Time { return now }
	if err := store.Authorize(context.Background(), validUUID(), validUUID(), validUUID(), "Bash"); !errors.Is(err, ErrExpired) {
		t.Fatalf("Authorize expired mandate = %v, want ErrExpired", err)
	}
}

func TestAuthorizeCannotBypassMissingOrOutOfScopeMandate(t *testing.T) {
	tests := []struct {
		name string
		row  mandateRow
		want error
	}{
		{name: "missing", row: mandateRow{err: pgx.ErrNoRows}, want: ErrMissing},
		{name: "tool outside snapshot", row: mandateRow{expiresAt: time.Now().Add(time.Hour), contains: false}, want: ErrToolDeny},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewStoreDB(mandateDB{row: tt.row})
			if err := store.Authorize(context.Background(), validUUID(), validUUID(), validUUID(), "Bash"); !errors.Is(err, tt.want) {
				t.Fatalf("Authorize = %v, want %v", err, tt.want)
			}
		})
	}
}
