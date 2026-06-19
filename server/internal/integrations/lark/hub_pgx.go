package lark

import "github.com/jackc/pgx/v5"

func init() {
	// Bind the package-level sentinel used by hub.go's isNoRowsErr.
	// Keeping the pgx import isolated to this file means the rest of
	// hub.go (and the test seam against fake queries) doesn't need the
	// pgx import path; fakes can return errors.New("no rows in result
	// set") and still trigger the lease "held elsewhere" branch.
	errPgxNoRows = pgx.ErrNoRows
}
