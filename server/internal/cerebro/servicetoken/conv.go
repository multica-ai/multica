package servicetoken

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
)

// toUUID converts a trusted, already-validated UUID string (workspace/token
// ids that round-trip through our own DB) to pgtype.UUID.
func toUUID(s string) pgtype.UUID { return util.MustParseUUID(s) }

// uuidOrNil converts a possibly-empty UUID string to pgtype.UUID, yielding a
// NULL (invalid) UUID for the empty case (e.g. an unattributed audit event).
func uuidOrNil(s string) pgtype.UUID {
	if s == "" {
		return pgtype.UUID{}
	}
	return util.MustParseUUID(s)
}

func timePtrToTs(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}

func tsToPtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}
