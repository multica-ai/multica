package util

import (
	"encoding/hex"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// ParseUUID parses s into a pgtype.UUID. Invalid input returns an error
// instead of a zero-valued UUID — silently dropping bad input has caused
// data-loss bugs (e.g. DELETE matching no rows, returning 204 success).
//
// Use this at any boundary where s comes from user input (URL params,
// request bodies, headers) and pair it with a 4xx response on error.
// For trusted, already-validated UUID strings (sqlc round-trips, fixtures),
// use MustParseUUID instead.
func ParseUUID(s string) (pgtype.UUID, error) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return u, fmt.Errorf("invalid UUID %q: %w", s, err)
	}
	if !u.Valid {
		return u, fmt.Errorf("invalid UUID: %q", s)
	}
	return u, nil
}

// MustParseUUID parses s into a pgtype.UUID and panics on invalid input.
// Reserve for trusted callers (already-validated round-trips, test fixtures).
// At a request boundary, use ParseUUID and surface a 4xx instead.
func MustParseUUID(s string) pgtype.UUID {
	u, err := ParseUUID(s)
	if err != nil {
		panic(err)
	}
	return u
}

func UUIDToString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	dst := make([]byte, 36)
	hex.Encode(dst[0:8], b[0:4])
	dst[8] = '-'
	hex.Encode(dst[9:13], b[4:6])
	dst[13] = '-'
	hex.Encode(dst[14:18], b[6:8])
	dst[18] = '-'
	hex.Encode(dst[19:23], b[8:10])
	dst[23] = '-'
	hex.Encode(dst[24:36], b[10:16])
	return string(dst)
}

func TextToPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	return &t.String
}

func PtrToText(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *s, Valid: true}
}

func StrToText(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

func TimestampToString(t pgtype.Timestamptz) string {
	if !t.Valid {
		return ""
	}
	return t.Time.Format(time.RFC3339)
}

func TimestampToPtr(t pgtype.Timestamptz) *string {
	if !t.Valid {
		return nil
	}
	s := t.Time.Format(time.RFC3339)
	return &s
}

func UUIDToPtr(u pgtype.UUID) *string {
	if !u.Valid {
		return nil
	}
	s := UUIDToString(u)
	return &s
}
