package service

import (
	"testing"
	"time"
)

// TestNextOccurrencesUTCEnumeratesInTimezone verifies the cron evaluator
// is timezone-aware on input but always returns UTC on output, and that
// the half-open (after, until] window is respected.
func TestNextOccurrencesUTCEnumeratesInTimezone(t *testing.T) {
	// Every Mon-Fri at 09:00 in Asia/Shanghai (UTC+8). 09:00 CST is
	// 01:00 UTC the same day.
	cron := "0 9 * * MON-FRI"
	tz := "Asia/Shanghai"

	// 2026-06-23 is a Tuesday. Pick a Monday-Friday span:
	//   Monday 2026-06-22 09:00 CST  = 2026-06-22T01:00:00Z
	//   Tuesday 2026-06-23 09:00 CST = 2026-06-23T01:00:00Z
	//   Wednesday 2026-06-24 09:00 CST = 2026-06-24T01:00:00Z
	after := time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC) // Sunday — before Mon's first fire
	until := time.Date(2026, 6, 24, 1, 0, 0, 0, time.UTC) // exactly Wed 09:00 CST

	got, err := NextOccurrencesUTC(cron, tz, after, until)
	if err != nil {
		t.Fatalf("NextOccurrencesUTC: %v", err)
	}

	want := []time.Time{
		time.Date(2026, 6, 22, 1, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 23, 1, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 24, 1, 0, 0, 0, time.UTC),
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d occurrences, got %d: %v", len(want), len(got), got)
	}
	for i, g := range got {
		if !g.Equal(want[i]) {
			t.Fatalf("occurrence[%d]: got %s, want %s",
				i, g.Format(time.RFC3339), want[i].Format(time.RFC3339))
		}
		if g.Location() != time.UTC {
			t.Fatalf("occurrence[%d] must be UTC, got %s", i, g.Location())
		}
	}
}

// TestNextOccurrencesUTCEmptyWindow asserts that an `after` >= `until`
// returns nothing without erroring — the planner relies on this for
// the "nothing due yet" branch.
func TestNextOccurrencesUTCEmptyWindow(t *testing.T) {
	cron := "*/5 * * * *"
	tz := "UTC"

	// after AFTER until: no occurrences should be returned.
	after := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	until := after.Add(-time.Minute)

	got, err := NextOccurrencesUTC(cron, tz, after, until)
	if err != nil {
		t.Fatalf("NextOccurrencesUTC: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no occurrences for empty window, got %v", got)
	}
}

// TestNextOccurrencesUTCExclusiveAfter verifies that a plan_time equal
// to `after` is NOT re-emitted. This is essential for the planner: when
// the most recent stored plan_time is X, the next tick must NOT
// produce X again.
func TestNextOccurrencesUTCExclusiveAfter(t *testing.T) {
	cron := "*/5 * * * *"
	tz := "UTC"

	// after sits exactly on a cron tick. The next emitted plan must
	// be the SUBSEQUENT tick, not the same one.
	after := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	until := time.Date(2026, 6, 23, 12, 10, 0, 0, time.UTC)

	got, err := NextOccurrencesUTC(cron, tz, after, until)
	if err != nil {
		t.Fatalf("NextOccurrencesUTC: %v", err)
	}
	want := []time.Time{
		time.Date(2026, 6, 23, 12, 5, 0, 0, time.UTC),
		time.Date(2026, 6, 23, 12, 10, 0, 0, time.UTC),
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d occurrences, got %d: %v", len(want), len(got), got)
	}
	for i, g := range got {
		if !g.Equal(want[i]) {
			t.Fatalf("occurrence[%d]: got %s want %s",
				i, g.Format(time.RFC3339), want[i].Format(time.RFC3339))
		}
	}
}

// TestNextOccurrencesUTCInvalidInputs surfaces parse errors loudly so
// a bad cron expression cannot silently disable a trigger.
func TestNextOccurrencesUTCInvalidInputs(t *testing.T) {
	t.Run("bad cron", func(t *testing.T) {
		_, err := NextOccurrencesUTC("not a cron", "UTC", time.Now(), time.Now().Add(time.Hour))
		if err == nil {
			t.Fatal("expected error for bad cron expression")
		}
	})
	t.Run("bad timezone", func(t *testing.T) {
		_, err := NextOccurrencesUTC("* * * * *", "Mars/Olympus", time.Now(), time.Now().Add(time.Hour))
		if err == nil {
			t.Fatal("expected error for invalid timezone")
		}
	})
}

// TestNextOccurrenceAfterUTCIgnoresWallClock verifies that the helper
// uses the explicit `after` argument, not time.Now(). This is the key
// property the scheduler relies on: callers pass dbNow() and get
// answers consistent across app instances with skewed clocks.
func TestNextOccurrenceAfterUTCIgnoresWallClock(t *testing.T) {
	cron := "30 14 * * *" // 14:30 daily in UTC

	// Pretend "DB now" is at midnight UTC. The next fire is 14:30 UTC
	// the same day — independent of whatever the host clock says.
	after := time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC)
	want := time.Date(2026, 6, 23, 14, 30, 0, 0, time.UTC)

	got, err := NextOccurrenceAfterUTC(cron, "UTC", after)
	if err != nil {
		t.Fatalf("NextOccurrenceAfterUTC: %v", err)
	}
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

// TestNextOccurrenceAdvancesPastFiredSlot locks in the property the
// scheduler's next_run_at write-back relies on (MUL-3749): once a
// recurring trigger fires at a slot, the next computed occurrence is the
// FOLLOWING slot, strictly after the one that just fired — never the same
// instant. Regression guard for the bug where next_run_at froze at a past
// slot and the list rendered it as "53m ago". Uses the exact reported
// scenario: hourly cron in America/New_York, slot 03:00 EDT (07:00 UTC).
func TestNextOccurrenceAdvancesPastFiredSlot(t *testing.T) {
	const cron = "0 * * * *" // every hour, on the hour
	const tz = "America/New_York"

	fired := time.Date(2026, 6, 26, 7, 0, 0, 0, time.UTC) // 03:00 EDT
	want := time.Date(2026, 6, 26, 8, 0, 0, 0, time.UTC)  // 04:00 EDT

	got, err := NextOccurrenceAfterUTC(cron, tz, fired)
	if err != nil {
		t.Fatalf("NextOccurrenceAfterUTC: %v", err)
	}
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got.Format(time.RFC3339), want.Format(time.RFC3339))
	}
	if !got.After(fired) {
		t.Fatalf("next occurrence %s must be strictly after the fired slot %s", got, fired)
	}
}

// TestParseCronScheduleTimezonePrefix pins the parser's handling of robfig's
// "TZ=" / "CRON_TZ=" expression prefix — the grammar contract the schedule
// editor's extraction (cron-mapping.ts) is written against.
func TestParseCronScheduleTimezonePrefix(t *testing.T) {
	t.Run("prefix without a schedule errors instead of panicking", func(t *testing.T) {
		// robfig v3.0.1 slices up to the first space and panics when there is
		// none (parser.go:99). The guard must turn that into a parse error.
		for _, expr := range []string{"TZ=UTC", "CRON_TZ=Asia/Tokyo", "TZ="} {
			if _, _, err := parseCronSchedule(expr, "UTC"); err == nil {
				t.Fatalf("expected error for %q", expr)
			}
		}
	})
	t.Run("embedded timezone overrides the timezone argument", func(t *testing.T) {
		after := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
		got, err := NextOccurrenceAfterUTC("CRON_TZ=Asia/Tokyo 0 9 * * *", "UTC", after)
		if err != nil {
			t.Fatal(err)
		}
		// 09:00 in Tokyo, not 09:00 UTC. `after` is exactly 09:00 Tokyo and
		// Next is strictly after, so the answer is the following day's slot.
		want := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
		if !got.Equal(want) {
			t.Fatalf("got %s, want %s", got.Format(time.RFC3339), want.Format(time.RFC3339))
		}
	})
	t.Run("prefix and column are equivalent spellings", func(t *testing.T) {
		after := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
		embedded, err := NextOccurrenceAfterUTC("CRON_TZ=Asia/Tokyo 0 9 * * *", "UTC", after)
		if err != nil {
			t.Fatal(err)
		}
		column, err := NextOccurrenceAfterUTC("0 9 * * *", "Asia/Tokyo", after)
		if err != nil {
			t.Fatal(err)
		}
		if !embedded.Equal(column) {
			t.Fatalf("embedded %s != column %s", embedded, column)
		}
	})
	t.Run("prefix detection is exact", func(t *testing.T) {
		// Lowercase and leading-space forms are NOT prefixes to robfig: the
		// token lands in the fields and the expression is a field-count error.
		for _, expr := range []string{"tz=UTC 0 9 * * *", " TZ=UTC 0 9 * * *"} {
			if _, _, err := parseCronSchedule(expr, "UTC"); err == nil {
				t.Fatalf("expected error for %q", expr)
			}
		}
	})
}
