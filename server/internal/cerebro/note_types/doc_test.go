package notetypes

import (
	"strings"
	"testing"
	"time"
)

// fixed reference instant: Wednesday 2026-02-11, ISO week 7, Q1.
var refTime = time.Date(2026, time.February, 11, 10, 30, 0, 0, time.UTC)

// anchorTime: the note type's creation instant used as the interval origin.
var anchorTime = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

// count == 1 keeps the historical calendar-aligned keys (and adds 'day').
func TestPeriodKey_CalendarAligned(t *testing.T) {
	cases := []struct {
		unit string
		want string
	}{
		{CadenceDay, "2026-02-11"},
		{CadenceWeek, "2026-W07"},
		{CadenceMonth, "2026-02"},
		{CadenceQuarter, "2026-Q1"},
	}
	for _, c := range cases {
		if got := PeriodKey(refTime, c.unit, 1, anchorTime); got != c.want {
			t.Errorf("PeriodKey(%s, count=1) = %q, want %q", c.unit, got, c.want)
		}
	}
}

// count > 1 switches to anchor-relative interval indexing: one period per
// N-unit window. Anchor 2026-01-01, ref 2026-02-11 = day 41.
func TestPeriodKey_Intervals(t *testing.T) {
	// every 7 days, anchor Jan 1: Feb 11 is day 41 → window 41/7 = 5.
	if got := PeriodKey(refTime, CadenceDay, 7, anchorTime); got != "day7-5" {
		t.Errorf("every-7-days key = %q, want day7-5", got)
	}
	// window 5 spans days 35–41 (Feb 5–11); Feb 6 is in the same window.
	sameWindow := refTime.AddDate(0, 0, -5) // Feb 6, day 36
	if a, b := PeriodKey(refTime, CadenceDay, 7, anchorTime), PeriodKey(sameWindow, CadenceDay, 7, anchorTime); a != b {
		t.Errorf("dates in same 7-day window should share a key: %q vs %q", a, b)
	}
	// Feb 12 is day 42 → window 6, a different key.
	nextWindow := refTime.AddDate(0, 0, 1) // Feb 12, day 42
	if a, b := PeriodKey(refTime, CadenceDay, 7, anchorTime), PeriodKey(nextWindow, CadenceDay, 7, anchorTime); a == b {
		t.Errorf("day 41 and day 42 should fall in different 7-day windows: both %q", a)
	}
	// every 2 weeks: 41 days / 7 = week 5, / 2 = index 2.
	if got := PeriodKey(refTime, CadenceWeek, 2, anchorTime); got != "week2-2" {
		t.Errorf("every-2-weeks key = %q, want week2-2", got)
	}
	// every 2 months: Feb is month 1 from Jan anchor, / 2 = index 0.
	if got := PeriodKey(refTime, CadenceMonth, 2, anchorTime); got != "month2-0" {
		t.Errorf("every-2-months key = %q, want month2-0", got)
	}
}

// Manual cadence must produce a unique key per instant so off-cycle runs never
// collide with or suppress one another.
func TestPeriodKey_ManualIsUnique(t *testing.T) {
	a := PeriodKey(refTime, CadenceManual, 1, anchorTime)
	b := PeriodKey(refTime.Add(time.Second), CadenceManual, 1, anchorTime)
	if !strings.HasPrefix(a, "manual-") {
		t.Errorf("manual key %q missing manual- prefix", a)
	}
	if a == b {
		t.Errorf("manual keys at different instants collided: %q", a)
	}
}

func TestPeriodLabel(t *testing.T) {
	cases := []struct {
		unit string
		want string
	}{
		{CadenceWeek, "Uge 7 2026"},
		{CadenceMonth, "Februar 2026"},
		{CadenceQuarter, "Q1 2026"},
		{CadenceManual, "11-02-2026"},
	}
	for _, c := range cases {
		if got := PeriodLabel(refTime, c.unit); got != c.want {
			t.Errorf("PeriodLabel(%s) = %q, want %q", c.unit, got, c.want)
		}
	}
}

func TestRenderTemplate(t *testing.T) {
	tmpl := "Review {{måned}} {{år}} ({{kvartal}}, uge {{uge}}, {{dato}}) — {{periode}}"
	got := RenderTemplate(tmpl, refTime, CadenceMonth, 0)
	want := "Review Februar 2026 (Q1, uge 7, 11-02-2026) — Februar 2026"
	if got != want {
		t.Errorf("RenderTemplate = %q, want %q", got, want)
	}
}

// {{nummer}} renders the assigned number when numbering is on, and empty when
// off (number <= 0) so a disabled type never shows "#0".
func TestRenderTemplate_Number(t *testing.T) {
	if got := RenderTemplate("Review #{{nummer}}", refTime, CadenceMonth, 7); got != "Review #7" {
		t.Errorf("with number = %q, want %q", got, "Review #7")
	}
	if got := RenderTemplate("Review{{nummer}}", refTime, CadenceMonth, 0); got != "Review" {
		t.Errorf("numbering off should blank {{nummer}}, got %q", got)
	}
}

// Unknown placeholders are left untouched rather than blanked.
func TestRenderTemplate_UnknownPlaceholderUntouched(t *testing.T) {
	got := RenderTemplate("Hej {{ukendt}} {{år}}", refTime, CadenceMonth, 0)
	if !strings.Contains(got, "{{ukendt}}") {
		t.Errorf("unknown placeholder was modified: %q", got)
	}
	if !strings.Contains(got, "2026") {
		t.Errorf("known placeholder not substituted: %q", got)
	}
}

func TestValidateModeAndCadence(t *testing.T) {
	if err := ValidateMode(ModeRunningDoc); err != nil {
		t.Errorf("running_doc should be valid: %v", err)
	}
	if err := ValidateMode("bogus"); err == nil {
		t.Error("bogus mode should be rejected")
	}
	if err := ValidateCadence(CadenceQuarter); err != nil {
		t.Errorf("quarter should be valid: %v", err)
	}
	if err := ValidateCadence("bogus"); err == nil {
		t.Error("bogus cadence should be rejected")
	}
}
