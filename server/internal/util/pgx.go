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

// DateToPtr formats a pgtype.Date as a date-only "YYYY-MM-DD" string, or nil
// when unset. Issue start_date/due_date are calendar days with no time-of-day
// or timezone, so they must never be rendered through an instant.
func DateToPtr(d pgtype.Date) *string {
	if !d.Valid {
		return nil
	}
	s := d.Time.Format(time.DateOnly)
	return &s
}

// ParseCalendarDate parses a calendar day from a "YYYY-MM-DD" string into a
// pgtype.Date carrying no time-of-day or timezone.
//
// For backward compatibility it ALSO accepts an RFC3339 timestamp, but ONLY
// when it lands exactly on a UTC day boundary (e.g. "2026-03-01T00:00:00Z"),
// which unambiguously denotes that calendar day. A non-midnight instant is a
// legacy local-midnight-as-UTC value (e.g. UTC+8 sends "2026-02-28T16:00:00Z"
// for the picked day 2026-03-01) whose intended calendar day is unrecoverable —
// it is rejected loudly rather than silently stored as the wrong day. New
// clients always send "YYYY-MM-DD".
func ParseCalendarDate(s string) (pgtype.Date, error) {
	if t, err := time.Parse(time.DateOnly, s); err == nil {
		return pgtype.Date{Time: t, Valid: true}, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		u := t.UTC()
		if u.Hour() == 0 && u.Minute() == 0 && u.Second() == 0 && u.Nanosecond() == 0 {
			return pgtype.Date{
				Time:  time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC),
				Valid: true,
			}, nil
		}
		return pgtype.Date{}, fmt.Errorf("invalid date %q: timestamps must be a UTC midnight boundary (e.g. 2026-03-01T00:00:00Z); use YYYY-MM-DD", s)
	}
	return pgtype.Date{}, fmt.Errorf("invalid date %q: expected YYYY-MM-DD", s)
}

func UUIDToPtr(u pgtype.UUID) *string {
	if !u.Valid {
		return nil
	}
	s := UUIDToString(u)
	return &s
}

func Int8ToPtr(v pgtype.Int8) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}
