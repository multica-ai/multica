package service

import (
	"testing"
	"time"
)

func TestParseCronAcceptsStandardFiveField(t *testing.T) {
	t.Parallel()
	cases := []string{
		"* * * * *",
		"0 9 * * *",
		"*/5 * * * *",
		"0 0 * * 0",
		"30 14 1 * *",
		"@daily",
		"@hourly",
		"@weekly",
	}
	for _, expr := range cases {
		if _, err := ParseCron(expr); err != nil {
			t.Errorf("ParseCron(%q) failed: %v", expr, err)
		}
	}
}

func TestParseCronRejectsSecondsField(t *testing.T) {
	t.Parallel()
	// 6-field (seconds) syntax is intentionally not supported — cron that
	// fires every second makes no sense for Multica.
	if _, err := ParseCron("0 0 9 * * *"); err == nil {
		t.Error("expected 6-field cron to be rejected")
	}
}

func TestParseCronRejectsEmpty(t *testing.T) {
	t.Parallel()
	if _, err := ParseCron(""); err == nil {
		t.Error("expected empty cron to be rejected")
	}
}

func TestParseCronRejectsGarbage(t *testing.T) {
	t.Parallel()
	if _, err := ParseCron("not a cron"); err == nil {
		t.Error("expected garbage cron to be rejected")
	}
}

func TestLoadTimezoneDefaultsToUTC(t *testing.T) {
	t.Parallel()
	loc, err := LoadTimezone("")
	if err != nil {
		t.Fatalf("LoadTimezone(\"\") failed: %v", err)
	}
	if loc != time.UTC {
		t.Errorf("expected UTC, got %v", loc)
	}
}

func TestLoadTimezoneAcceptsIANA(t *testing.T) {
	t.Parallel()
	loc, err := LoadTimezone("America/New_York")
	if err != nil {
		t.Fatalf("LoadTimezone failed: %v", err)
	}
	if loc.String() != "America/New_York" {
		t.Errorf("expected America/New_York, got %v", loc)
	}
}

func TestLoadTimezoneRejectsGarbage(t *testing.T) {
	t.Parallel()
	if _, err := LoadTimezone("Not/A_Zone"); err == nil {
		t.Error("expected garbage timezone to be rejected")
	}
}

func TestNextFireTimeDailyInTimezone(t *testing.T) {
	t.Parallel()
	// "every day at 9am America/New_York", starting at 2026-01-15 04:00 UTC
	// (which is 2025-01-14 23:00 EST) — next fire should be 2026-01-15 14:00 UTC
	// (= 09:00 EST on the 15th).
	after := time.Date(2026, 1, 15, 4, 0, 0, 0, time.UTC)
	next, err := NextFireTime("0 9 * * *", "America/New_York", after)
	if err != nil {
		t.Fatalf("NextFireTime: %v", err)
	}
	want := time.Date(2026, 1, 15, 14, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Errorf("expected %s, got %s", want, next)
	}
}

func TestNextFireTimeEveryMinute(t *testing.T) {
	t.Parallel()
	after := time.Date(2026, 1, 15, 12, 34, 15, 0, time.UTC)
	next, err := NextFireTime("* * * * *", "UTC", after)
	if err != nil {
		t.Fatalf("NextFireTime: %v", err)
	}
	// Next minute boundary after 12:34:15 is 12:35:00.
	want := time.Date(2026, 1, 15, 12, 35, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Errorf("expected %s, got %s", want, next)
	}
}

func TestNextFireTimeReturnsUTC(t *testing.T) {
	t.Parallel()
	after := time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)
	next, err := NextFireTime("0 9 * * *", "America/New_York", after)
	if err != nil {
		t.Fatalf("NextFireTime: %v", err)
	}
	if next.Location() != time.UTC {
		t.Errorf("expected UTC location, got %v", next.Location())
	}
}
