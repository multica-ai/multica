// Package dbid mints primary keys for rows the application inserts.
//
// Every id produced here is a UUIDv7 (RFC 9562): a 48-bit big-endian
// millisecond timestamp followed by random bits. Consecutive inserts therefore
// cluster in a narrow contiguous key range instead of scattering across the
// primary-key B-tree the way random v4 keys do. On tables that already contain
// v4 ids, that range is not necessarily the tree's right edge.
//
// Scope and rules of use:
//
//   - Only for identity columns of rows we insert and keep — task queue entries,
//     messages, comments, activity and audit rows. NOT for lease/claim tokens,
//     idempotency keys, or anything used as a secret: those want
//     unpredictability and no embedded timestamp, so they stay on
//     gen_random_uuid()/v4.
//   - The DB-side `DEFAULT gen_random_uuid()` on these columns stays in place,
//     and every query that takes an id from here wraps it in
//     `COALESCE(sqlc.narg('id')::uuid, gen_random_uuid())`. A table therefore
//     holds a mix of v4 and v7 ids forever, which Postgres's uuid type and every
//     parser in this repo accept, and an unset id degrades to today's behaviour
//     instead of violating NOT NULL.
//   - The database fallback makes NewV7 suitable only when the id is used solely
//     as the inserted row's identity. If the application must also use the id as
//     an object key, filename, correlation id, or any other external reference,
//     call uuid.NewV7 directly and handle its error instead; otherwise the
//     database may mint a different id after entropy generation fails.
//   - A v7 embeds its creation time only to millisecond precision and is only
//     approximately ordered across writers. It is not a substitute for
//     created_at and must not be used to derive ordering guarantees.
//   - On an INSERT ... ON CONFLICT, an id passed in is used only if the row is
//     actually inserted; the conflicting row keeps its own id. Callers must read
//     the id back from RETURNING rather than assume the value they minted won.
package dbid

import (
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// NewV7 returns a fresh UUIDv7 wrapped as a pgtype.UUID, ready to assign to a
// sqlc-generated parameter struct's ID field.
//
// It deliberately has no error return. uuid.NewV7 only fails when the OS
// entropy source does, which does not happen on a healthy host; if it ever
// does, this returns the zero (NULL) pgtype.UUID and the query's
// COALESCE(..., gen_random_uuid()) fallback lets Postgres mint the id instead.
// That keeps the insert working rather than failing a user request over an id
// flavour, and keeps ~60 call sites free of an error path that would never run.
func NewV7() pgtype.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		slog.Error("dbid: uuidv7 generation failed, falling back to the database default", "error", err)
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: [16]byte(id), Valid: true}
}
