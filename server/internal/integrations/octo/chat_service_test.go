package octo

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// TestIsUniqueViolation checks the SQLSTATE classifier that the
// EnsureChatSession race re-read path keys on: only a real 23505 PgError must
// be treated as a unique violation; everything else (other PgErrors, wrapped
// non-pg errors, nil) must not be.
func TestIsUniqueViolation(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"unique violation", &pgconn.PgError{Code: "23505"}, true},
		{"wrapped unique violation", fmt.Errorf("create: %w", &pgconn.PgError{Code: "23505"}), true},
		{"foreign key violation", &pgconn.PgError{Code: "23503"}, false},
		{"check violation", &pgconn.PgError{Code: "23514"}, false},
		{"non-pg error", errors.New("boom"), false},
		{"no rows", pgx.ErrNoRows, false},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isUniqueViolation(tc.err); got != tc.want {
				t.Errorf("isUniqueViolation(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
