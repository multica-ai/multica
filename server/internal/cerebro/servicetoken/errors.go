package servicetoken

import "github.com/jackc/pgx/v5"

// errNoRows is the "no matching row" sentinel a Store returns from GetByHash
// (token not found / revoked / expired) and Revoke (nothing to revoke). It
// aliases pgx.ErrNoRows so the real cerebroStore and an in-memory test Store
// agree on the same value.
var errNoRows = pgx.ErrNoRows
