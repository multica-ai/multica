package notetypes

import (
	"fmt"
	"strings"
	"time"
)

// Recurrence modes.
const (
	ModeRunningDoc = "running_doc"
	ModeNewNote    = "new_note"
)

// Cadence units. "manual" means the type never auto-fires — it only
// materialises when an admin clicks "run now".
const (
	CadenceManual  = "manual"
	CadenceDay     = "day"
	CadenceWeek    = "week"
	CadenceMonth   = "month"
	CadenceQuarter = "quarter"
)

// ValidModes / ValidCadences are exported so the handler can echo the allowed
// values in a 400 response.
var ValidModes = []string{ModeRunningDoc, ModeNewNote}
var ValidCadences = []string{CadenceManual, CadenceDay, CadenceWeek, CadenceMonth, CadenceQuarter}

var danishMonths = [...]string{
	"januar", "februar", "marts", "april", "maj", "juni",
	"juli", "august", "september", "oktober", "november", "december",
}

// ValidateMode returns nil when mode is a supported recurrence mode.
func ValidateMode(mode string) error {
	for _, m := range ValidModes {
		if m == mode {
			return nil
		}
	}
	return fmt.Errorf("invalid recurrence_mode %q, expected one of: %s", mode, strings.Join(ValidModes, ", "))
}

// ValidateCadence returns nil when cadence is a supported cadence unit.
func ValidateCadence(cadence string) error {
	for _, c := range ValidCadences {
		if c == cadence {
			return nil
		}
	}
	return fmt.Errorf("invalid cadence_unit %q, expected one of: %s", cadence, strings.Join(ValidCadences, ", "))
}

func quarterOf(t time.Time) int { return (int(t.Month())-1)/3 + 1 }

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// normalizeCount clamps cadence_count to a sane minimum of 1.
func normalizeCount(count int32) int32 {
	if count < 1 {
		return 1
	}
	return count
}

// dateOnly drops the clock part so interval maths work on whole days.
func dateOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// intervalIndex counts how many whole units of `unit` lie between the anchor
// and t — the basis for "every N units" period keys.
func intervalIndex(anchor, t time.Time, unit string) int {
	switch unit {
	case CadenceDay:
		return int(dateOnly(t).Sub(dateOnly(anchor)).Hours()) / 24
	case CadenceWeek:
		return (int(dateOnly(t).Sub(dateOnly(anchor)).Hours()) / 24) / 7
	case CadenceMonth:
		return (t.Year()-anchor.Year())*12 + (int(t.Month()) - int(anchor.Month()))
	case CadenceQuarter:
		months := (t.Year()-anchor.Year())*12 + (int(t.Month()) - int(anchor.Month()))
		return months / 3
	}
	return 0
}

// PeriodKey returns a stable key for the period that t falls in. With
// cadence_count == 1 the key is calendar-aligned (the historical behaviour);
// with count > 1 it is an anchor-relative interval index so "every N units"
// produces one period per N-unit window. Manual cadence returns a
// timestamp-unique key so a manual run is never suppressed by a previous one.
func PeriodKey(t time.Time, unit string, count int32, anchor time.Time) string {
	if unit == CadenceManual {
		return "manual-" + t.UTC().Format("20060102-150405")
	}
	count = normalizeCount(count)
	if count == 1 {
		switch unit {
		case CadenceDay:
			return t.Format("2006-01-02")
		case CadenceWeek:
			y, w := t.ISOWeek()
			return fmt.Sprintf("%04d-W%02d", y, w)
		case CadenceMonth:
			return fmt.Sprintf("%04d-%02d", t.Year(), int(t.Month()))
		case CadenceQuarter:
			return fmt.Sprintf("%04d-Q%d", t.Year(), quarterOf(t))
		}
	}
	idx := intervalIndex(anchor, t, unit) / int(count)
	return fmt.Sprintf("%s%d-%d", unit, count, idx)
}

// PeriodLabel is the human title fragment for the period (Danish). It reflects
// the calendar position of t; the per-period number (when enabled) carries the
// sequence, so the label stays a readable date/week/month even for intervals.
func PeriodLabel(t time.Time, unit string) string {
	switch unit {
	case CadenceWeek:
		y, w := t.ISOWeek()
		return fmt.Sprintf("Uge %d %d", w, y)
	case CadenceMonth:
		return fmt.Sprintf("%s %d", capitalize(danishMonths[int(t.Month())-1]), t.Year())
	case CadenceQuarter:
		return fmt.Sprintf("Q%d %d", quarterOf(t), t.Year())
	default: // day + manual
		return t.Format("02-01-2006")
	}
}

// RenderTemplate substitutes the supported placeholders in a template body
// with values derived from t. number > 0 fills {{nummer}}; number <= 0 renders
// it as empty (numbering disabled). Unknown placeholders are left untouched.
func RenderTemplate(tmpl string, t time.Time, unit string, number int32) string {
	_, week := t.ISOWeek()
	month := capitalize(danishMonths[int(t.Month())-1])
	year := fmt.Sprintf("%d", t.Year())
	numberStr := ""
	if number > 0 {
		numberStr = fmt.Sprintf("%d", number)
	}
	repl := strings.NewReplacer(
		"{{år}}", year,
		"{{aar}}", year,
		"{{måned}}", month,
		"{{maaned}}", month,
		"{{kvartal}}", fmt.Sprintf("Q%d", quarterOf(t)),
		"{{uge}}", fmt.Sprintf("%d", week),
		"{{dato}}", t.Format("02-01-2006"),
		"{{periode}}", PeriodLabel(t, unit),
		"{{nummer}}", numberStr,
	)
	return repl.Replace(tmpl)
}
