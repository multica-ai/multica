package notetypes

import (
	"strings"
	"testing"
	"time"
)

// fixed reference instant: Wednesday 2026-02-11, ISO week 7, Q1.
var refTime = time.Date(2026, time.February, 11, 10, 30, 0, 0, time.UTC)

func TestPeriodKey(t *testing.T) {
	cases := []struct {
		unit string
		want string
	}{
		{CadenceWeek, "2026-W07"},
		{CadenceMonth, "2026-02"},
		{CadenceQuarter, "2026-Q1"},
	}
	for _, c := range cases {
		if got := PeriodKey(refTime, c.unit); got != c.want {
			t.Errorf("PeriodKey(%s) = %q, want %q", c.unit, got, c.want)
		}
	}
}

// Manual cadence must produce a unique key per instant so off-cycle runs never
// collide with or suppress one another.
func TestPeriodKey_ManualIsUnique(t *testing.T) {
	a := PeriodKey(refTime, CadenceManual)
	b := PeriodKey(refTime.Add(time.Second), CadenceManual)
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
	got := RenderTemplate(tmpl, refTime, CadenceMonth)
	want := "Review Februar 2026 (Q1, uge 7, 11-02-2026) — Februar 2026"
	if got != want {
		t.Errorf("RenderTemplate = %q, want %q", got, want)
	}
}

// Unknown placeholders are left untouched rather than blanked.
func TestRenderTemplate_UnknownPlaceholderUntouched(t *testing.T) {
	got := RenderTemplate("Hej {{ukendt}} {{år}}", refTime, CadenceMonth)
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
