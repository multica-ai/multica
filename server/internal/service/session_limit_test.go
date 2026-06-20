package service

import (
	"testing"
	"time"
)

func TestParseSessionLimitResetTime(t *testing.T) {
	tests := []struct {
		name    string
		message string
		wantOK  bool
	}{
		{
			name:    "standard claude message pm",
			message: "You've hit your session limit · resets 5:10pm (UTC)",
			wantOK:  true,
		},
		{
			name:    "standard claude message am",
			message: "You've hit your session limit · resets 10:10am (UTC)",
			wantOK:  true,
		},
		{
			name:    "midnight 12am",
			message: "You've hit your session limit · resets 12:00am (UTC)",
			wantOK:  true,
		},
		{
			name:    "noon 12pm",
			message: "You've hit your session limit · resets 12:00pm (UTC)",
			wantOK:  true,
		},
		{
			name:    "no parentheses",
			message: "You've hit your session limit · resets 5:10pm UTC",
			wantOK:  true,
		},
		{
			name:    "single digit hour 1am",
			message: "You've hit your session limit · resets 1:00am (UTC)",
			wantOK:  true,
		},
		{
			name:    "case-insensitive AM",
			message: "You've hit your session limit · resets 9:45AM (UTC)",
			wantOK:  true,
		},
		{
			name:    "message embedded in larger text",
			message: "Task failed: You've hit your session limit · resets 3:15pm (UTC). Please wait.",
			wantOK:  true,
		},
		{
			name:    "iana timezone Europe/Paris",
			message: "You've hit your session limit · resets 5:10pm (Europe/Paris)",
			wantOK:  true,
		},
		{
			name:    "iana timezone America/New_York with underscore",
			message: "You've hit your session limit · resets 9:30am (America/New_York)",
			wantOK:  true,
		},
		{
			name:    "iana timezone Asia/Tokyo no parentheses",
			message: "You've hit your session limit · resets 11:00pm Asia/Tokyo",
			wantOK:  true,
		},
		{
			name:    "multi-segment iana timezone",
			message: "You've hit your session limit · resets 2:00pm (America/Argentina/Buenos_Aires)",
			wantOK:  true,
		},
		{
			name:    "whole hour no minutes UTC",
			message: "resets 12pm (UTC)",
			wantOK:  true,
		},
		{
			name:    "whole hour no minutes Europe/Paris",
			message: "resets 2pm (Europe/Paris)",
			wantOK:  true,
		},
		{
			// Whole-hour am case that prompted the follow-up test addition:
			// the real Claude final message uses the `·` separator and an
			// am hour with no minutes. Locks the full-message path end to
			// end, not just the `resets …` substring, so a regression in
			// either the regex or any upstream message split would fail
			// here rather than silently no-op in production.
			name:    "full message whole hour 5am UTC",
			message: "You've hit your session limit · resets 5am (UTC)",
			wantOK:  true,
		},
		{
			name:    "unresolvable timezone",
			message: "You've hit your session limit · resets 5:10pm (Mars/Olympus_Mons)",
			wantOK:  false,
		},
		{
			name:    "unrelated error",
			message: "connection refused",
			wantOK:  false,
		},
		{
			name:    "empty",
			message: "",
			wantOK:  false,
		},
		{
			name:    "no reset time",
			message: "You've hit your session limit",
			wantOK:  false,
		},
		{
			name:    "hour out of range 0",
			message: "You've hit your session limit · resets 0:00am (UTC)",
			wantOK:  false,
		},
		{
			name:    "hour out of range 13",
			message: "You've hit your session limit · resets 13:00pm (UTC)",
			wantOK:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParseSessionLimitResetTime(tc.message)
			if ok != tc.wantOK {
				t.Fatalf("ParseSessionLimitResetTime(%q) ok = %v, want %v", tc.message, ok, tc.wantOK)
			}
			if ok {
				if got.IsZero() {
					t.Fatal("expected non-zero time")
				}
				if got.Before(time.Now().UTC().Add(-time.Minute)) {
					t.Fatalf("reset time %v is in the past", got)
				}
			}
		})
	}
}

// TestParseSessionLimitResetTimeValues verifies the exact hour/minute conversion.
func TestParseSessionLimitResetTimeValues(t *testing.T) {
	msg := "You've hit your session limit · resets 11:30pm (UTC)"
	got, ok := ParseSessionLimitResetTime(msg)
	if !ok {
		t.Fatal("expected parse success")
	}
	if got.Hour() != 23 || got.Minute() != 30 {
		t.Fatalf("expected 23:30, got %02d:%02d", got.Hour(), got.Minute())
	}
}

// TestParseSessionLimitResetTimeConversions verifies AM/PM and 12-hour boundary
// conversions produce the correct 24-hour UTC values.
func TestParseSessionLimitResetTimeConversions(t *testing.T) {
	cases := []struct {
		name       string
		message    string
		wantHour   int
		wantMinute int
	}{
		{
			name:       "12am is midnight (hour 0)",
			message:    "resets 12:00am (UTC)",
			wantHour:   0,
			wantMinute: 0,
		},
		{
			name:       "12pm is noon (hour 12)",
			message:    "resets 12:30pm (UTC)",
			wantHour:   12,
			wantMinute: 30,
		},
		{
			name:       "1pm becomes hour 13",
			message:    "resets 1:15pm (UTC)",
			wantHour:   13,
			wantMinute: 15,
		},
		{
			name:       "1am stays hour 1",
			message:    "resets 1:00am (UTC)",
			wantHour:   1,
			wantMinute: 0,
		},
		{
			name:       "whole-hour 12pm UTC has zero minutes",
			message:    "resets 12pm (UTC)",
			wantHour:   12,
			wantMinute: 0,
		},
		{
			// Pins the empty-minutes path the BLOCKER fix opened up:
			// a whole-hour am case where the (?:(\\d{2}))? group
			// matches the empty string and `strconv.Atoi("")` returns 0.
			// Confirms minute=0 and the am boundary (5 stays 5, not 17).
			name:       "whole-hour 5am UTC has zero minutes",
			message:    "You've hit your session limit · resets 5am (UTC)",
			wantHour:   5,
			wantMinute: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParseSessionLimitResetTime(tc.message)
			if !ok {
				t.Fatalf("expected parse success for %q", tc.message)
			}
			if got.Hour() != tc.wantHour || got.Minute() != tc.wantMinute {
				t.Fatalf("expected %02d:%02d, got %02d:%02d", tc.wantHour, tc.wantMinute, got.Hour(), got.Minute())
			}
		})
	}
}

// TestParseSessionLimitResetTimeTimezone verifies the wall-clock time is
// interpreted in the timezone named by the message, not in UTC.
func TestParseSessionLimitResetTimeTimezone(t *testing.T) {
	cases := []struct {
		name       string
		tz         string
		message    string
		wantHour   int
		wantMinute int
	}{
		{
			name:       "Europe/Paris",
			tz:         "Europe/Paris",
			message:    "You've hit your session limit · resets 5:10pm (Europe/Paris)",
			wantHour:   17,
			wantMinute: 10,
		},
		{
			name:       "Asia/Tokyo",
			tz:         "Asia/Tokyo",
			message:    "You've hit your session limit · resets 9:30am (Asia/Tokyo)",
			wantHour:   9,
			wantMinute: 30,
		},
		{
			name:       "America/New_York midnight",
			tz:         "America/New_York",
			message:    "You've hit your session limit · resets 12:00am (America/New_York)",
			wantHour:   0,
			wantMinute: 0,
		},
		{
			// DST-safe: asserts wall-clock 2pm in Paris regardless of UTC offset.
			name:       "whole-hour Europe/Paris 2pm",
			tz:         "Europe/Paris",
			message:    "resets 2pm (Europe/Paris)",
			wantHour:   14,
			wantMinute: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			loc, err := time.LoadLocation(tc.tz)
			if err != nil {
				t.Skipf("timezone %s unavailable on this system: %v", tc.tz, err)
			}
			got, ok := ParseSessionLimitResetTime(tc.message)
			if !ok {
				t.Fatalf("expected parse success for %q", tc.message)
			}
			// The returned instant, viewed in the named zone, must show the
			// original wall-clock time regardless of UTC offset or DST.
			inZone := got.In(loc)
			if inZone.Hour() != tc.wantHour || inZone.Minute() != tc.wantMinute {
				t.Fatalf("in %s expected %02d:%02d, got %02d:%02d",
					tc.tz, tc.wantHour, tc.wantMinute, inZone.Hour(), inZone.Minute())
			}
		})
	}
}

// TestParseSessionLimitResetTimeZonesDiffer confirms the same wall-clock time
// in two different timezones yields two different absolute instants — proving
// the zone is honored rather than ignored.
func TestParseSessionLimitResetTimeZonesDiffer(t *testing.T) {
	utc, ok := ParseSessionLimitResetTime("resets 6:00pm (UTC)")
	if !ok {
		t.Fatal("expected UTC parse success")
	}
	tokyo, ok := ParseSessionLimitResetTime("resets 6:00pm (Asia/Tokyo)")
	if !ok {
		t.Skip("Asia/Tokyo unavailable on this system")
	}
	if utc.Equal(tokyo) {
		t.Fatalf("expected different instants for 6:00pm UTC vs 6:00pm Asia/Tokyo, both were %v", utc)
	}
}

// TestParseSessionLimitResetTimeTomorrowRollover pins the contract that a
// parsed wall-clock time already in the past today is advanced to tomorrow.
func TestParseSessionLimitResetTimeTomorrowRollover(t *testing.T) {
	// 00:01 UTC is always in the past for the current day whenever the test
	// runs after 00:01 UTC — except for the one minute window at midnight,
	// so we skip in that edge case to keep the test deterministic.
	now := time.Now().UTC()
	if now.Hour() == 0 && now.Minute() == 0 {
		t.Skip("skipping midnight edge-case window")
	}

	// If current time is after 00:01, "resets 12:01am" is in the past today
	// and the parser should return tomorrow.
	got, ok := ParseSessionLimitResetTime("You've hit your session limit · resets 12:01am (UTC)")
	if !ok {
		t.Fatal("expected parse success")
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 1, 0, 0, time.UTC)
	if got.Before(now) {
		t.Fatalf("reset time %v is in the past (now=%v)", got, now)
	}
	_ = today // used to compute the expected tomorrow
	expectedDate := today.AddDate(0, 0, 1)
	if now.After(today) && got.Day() != expectedDate.Day() {
		t.Fatalf("expected rollover to tomorrow (day %d), got day %d", expectedDate.Day(), got.Day())
	}
}
