package lark

import (
	"io"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

// Shared unit-test helpers for the lark package. (Formerly defined on
// hub_test.go, which the channel-engine cutover removed; relocated here so
// the surviving connector / outbound / runtime tests keep using them.)

func uuidFromString(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		t.Fatalf("scan uuid %q: %v", s, err)
	}
	return u
}

func newDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
