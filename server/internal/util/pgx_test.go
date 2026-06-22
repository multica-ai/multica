package util

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestParseUUID_Valid(t *testing.T) {
	u, err := ParseUUID("550e8400-e29b-41d4-a716-446655440000")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !u.Valid {
		t.Fatalf("expected u.Valid = true")
	}
}

func TestParseUUID_InvalidReturnsError(t *testing.T) {
	cases := []string{"", "not-a-uuid", "MUL-123", "12345"}
	for _, s := range cases {
		t.Run(s, func(t *testing.T) {
			u, err := ParseUUID(s)
			if err == nil {
				t.Fatalf("expected error for %q, got nil (u.Valid=%v)", s, u.Valid)
			}
			if u.Valid {
				// Critical invariant: invalid input must NOT yield a valid UUID.
				// Returning a valid zero-UUID was the root cause of #1661.
				t.Fatalf("expected u.Valid = false for %q, got true", s)
			}
		})
	}
}

func TestMustParseUUID_PanicsOnInvalid(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected MustParseUUID to panic on invalid input")
		}
	}()
	MustParseUUID("not-a-uuid")
}

func TestMustParseUUID_RoundTrip(t *testing.T) {
	const s = "550e8400-e29b-41d4-a716-446655440000"
	u := MustParseUUID(s)
	if got := UUIDToString(u); got != s {
		t.Fatalf("round-trip mismatch: got %q want %q", got, s)
	}
}

func TestParseCalendarDate_DateOnly(t *testing.T) {
	d, err := ParseCalendarDate("2026-03-01")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got := DateToPtr(d); got == nil || *got != "2026-03-01" {
		t.Fatalf("round-trip mismatch: got %v want 2026-03-01", got)
	}
}

func TestParseCalendarDate_AcceptsUTCMidnight(t *testing.T) {
	// A UTC-midnight instant unambiguously denotes that calendar day.
	d, err := ParseCalendarDate("2026-03-01T00:00:00Z")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got := DateToPtr(d); got == nil || *got != "2026-03-01" {
		t.Fatalf("got %v want 2026-03-01", got)
	}
}

func TestParseCalendarDate_RejectsNonMidnightInstant(t *testing.T) {
	// The legacy bug: UTC+8 picking 2026-03-01 sent 2026-02-28T16:00:00Z. Its
	// intended calendar day is unrecoverable, so reject instead of silently
	// storing the wrong day (2026-02-28).
	cases := []string{
		"2026-02-28T16:00:00Z", // UTC+8 local midnight
		"2026-03-01T05:00:00Z", // UTC-5 local midnight
		"2026-03-01T00:00:00+08:00",
	}
	for _, s := range cases {
		t.Run(s, func(t *testing.T) {
			if _, err := ParseCalendarDate(s); err == nil {
				t.Fatalf("expected error for non-midnight instant %q, got nil", s)
			}
		})
	}
}

func TestParseCalendarDate_RejectsGarbage(t *testing.T) {
	for _, s := range []string{"", "not-a-date", "03/01/2026", "2026-13-40"} {
		t.Run(s, func(t *testing.T) {
			if _, err := ParseCalendarDate(s); err == nil {
				t.Fatalf("expected error for %q, got nil", s)
			}
		})
	}
}

func TestDateToPtr_NullIsNil(t *testing.T) {
	if got := DateToPtr(pgtype.Date{Valid: false}); got != nil {
		t.Fatalf("expected nil for invalid date, got %v", *got)
	}
}

// Guard against a localtime regression: DateToPtr must emit the stored calendar
// day regardless of the host process timezone.
func TestDateToPtr_StableAcrossTimezone(t *testing.T) {
	d := pgtype.Date{Time: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), Valid: true}
	if got := DateToPtr(d); got == nil || *got != "2026-03-01" {
		t.Fatalf("got %v want 2026-03-01", got)
	}
}

func TestCalendarDateToTimestamptz_DateOnly(t *testing.T) {
	ts, err := CalendarDateToTimestamptz("2026-03-01")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !ts.Valid {
		t.Fatalf("expected ts.Valid = true")
	}
	want := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	if !ts.Time.UTC().Equal(want) {
		t.Fatalf("got %v want %v", ts.Time.UTC(), want)
	}
}

func TestCalendarDateToTimestamptz_AcceptsUTCMidnight(t *testing.T) {
	ts, err := CalendarDateToTimestamptz("2026-03-01T00:00:00Z")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got := FormatTimestamptzDateOnly(ts); got == nil || *got != "2026-03-01" {
		t.Fatalf("got %v want 2026-03-01", got)
	}
}

func TestCalendarDateToTimestamptz_RejectsNonMidnightAndGarbage(t *testing.T) {
	// PR1 keeps date-only semantics, so it mirrors ParseCalendarDate's policy:
	// a non-midnight instant has an unrecoverable calendar day and is rejected.
	for _, s := range []string{"2026-02-28T16:00:00Z", "not-a-date", "", "03/01/2026"} {
		t.Run(s, func(t *testing.T) {
			if _, err := CalendarDateToTimestamptz(s); err == nil {
				t.Fatalf("expected error for %q, got nil", s)
			}
		})
	}
}

func TestFormatTimestamptzDateOnly_NullIsNil(t *testing.T) {
	if got := FormatTimestamptzDateOnly(pgtype.Timestamptz{Valid: false}); got != nil {
		t.Fatalf("expected nil for invalid timestamp, got %v", *got)
	}
}

// Guard the timezone invariant: the date emitted is the stored UTC calendar day,
// independent of the host process timezone (the #3618 lesson, now on timestamptz).
func TestFormatTimestamptzDateOnly_StableAcrossTimezone(t *testing.T) {
	ts := pgtype.Timestamptz{Time: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), Valid: true}
	if got := FormatTimestamptzDateOnly(ts); got == nil || *got != "2026-03-01" {
		t.Fatalf("got %v want 2026-03-01", got)
	}
}

func TestParseInstant_RFC3339(t *testing.T) {
	ts, err := ParseInstant("2026-02-01T14:30:00Z")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	want := time.Date(2026, 2, 1, 14, 30, 0, 0, time.UTC)
	if !ts.Time.Equal(want) {
		t.Fatalf("got %v want %v", ts.Time, want)
	}
}

func TestParseInstant_NormalizesOffsetToUTC(t *testing.T) {
	// An offset instant is preserved as the same instant, stored in UTC.
	ts, err := ParseInstant("2026-02-01T14:30:00+02:00")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got := TimestampToString(ts); got != "2026-02-01T12:30:00Z" {
		t.Fatalf("got %q want 2026-02-01T12:30:00Z", got)
	}
}

func TestParseInstant_DateOnlyMeansUTCMidnight(t *testing.T) {
	ts, err := ParseInstant("2026-02-01")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got := TimestampToString(ts); got != "2026-02-01T00:00:00Z" {
		t.Fatalf("got %q want 2026-02-01T00:00:00Z", got)
	}
}

func TestParseInstant_RejectsGarbage(t *testing.T) {
	for _, s := range []string{"", "not-a-date", "03/01/2026", "2026-13-40", "14:30"} {
		t.Run(s, func(t *testing.T) {
			if _, err := ParseInstant(s); err == nil {
				t.Fatalf("expected error for %q, got nil", s)
			}
		})
	}
}
