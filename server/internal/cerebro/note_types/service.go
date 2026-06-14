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
	CadenceWeek    = "week"
	CadenceMonth   = "month"
	CadenceQuarter = "quarter"
)

// ValidModes / ValidCadences are exported so the handler can echo the allowed
// values in a 400 response.
var ValidModes = []string{ModeRunningDoc, ModeNewNote}
var ValidCadences = []string{CadenceManual, CadenceWeek, CadenceMonth, CadenceQuarter}

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

// PeriodKey returns a stable key for the period that t falls in. Manual
// cadence returns a timestamp-unique key so a manual run is never suppressed
// by, or collides with, a previous one.
func PeriodKey(t time.Time, unit string) string {
	switch unit {
	case CadenceWeek:
		y, w := t.ISOWeek()
		return fmt.Sprintf("%04d-W%02d", y, w)
	case CadenceMonth:
		return fmt.Sprintf("%04d-%02d", t.Year(), int(t.Month()))
	case CadenceQuarter:
		return fmt.Sprintf("%04d-Q%d", t.Year(), quarterOf(t))
	default: // manual
		return "manual-" + t.UTC().Format("20060102-150405")
	}
}

// PeriodLabel is the human title fragment for the period (Danish).
func PeriodLabel(t time.Time, unit string) string {
	switch unit {
	case CadenceWeek:
		y, w := t.ISOWeek()
		return fmt.Sprintf("Uge %d %d", w, y)
	case CadenceMonth:
		return fmt.Sprintf("%s %d", capitalize(danishMonths[int(t.Month())-1]), t.Year())
	case CadenceQuarter:
		return fmt.Sprintf("Q%d %d", quarterOf(t), t.Year())
	default:
		return t.Format("02-01-2006")
	}
}

// RenderTemplate substitutes the supported placeholders in a template body
// with values derived from t. Unknown placeholders are left untouched.
func RenderTemplate(tmpl string, t time.Time, unit string) string {
	_, week := t.ISOWeek()
	month := capitalize(danishMonths[int(t.Month())-1])
	year := fmt.Sprintf("%d", t.Year())
	repl := strings.NewReplacer(
		"{{år}}", year,
		"{{aar}}", year,
		"{{måned}}", month,
		"{{maaned}}", month,
		"{{kvartal}}", fmt.Sprintf("Q%d", quarterOf(t)),
		"{{uge}}", fmt.Sprintf("%d", week),
		"{{dato}}", t.Format("02-01-2006"),
		"{{periode}}", PeriodLabel(t, unit),
	)
	return repl.Replace(tmpl)
}
