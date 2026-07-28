package notetypes

import (
	"fmt"
	"math"
	"strings"
	"time"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
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

func isoWeekday(t time.Time) int {
	w := int(t.Weekday())
	if w == 0 {
		return 7
	}
	return w
}

// hasMonthlyWeekAnchor reports whether the type schedules on the "Nth weekday
// of the month" model: a monthly cadence with both an anchor weekday (ISO 1..7)
// and an ordinal week-of-month (1..5, or -1 for "last").
func hasMonthlyWeekAnchor(nt cerebrodb.CerebroNoteType) bool {
	return nt.CadenceUnit == CadenceMonth && nt.AnchorWeekday.Valid && nt.AnchorWeekOfMonth.Valid &&
		nt.AnchorWeekday.Int16 >= 1 && nt.AnchorWeekday.Int16 <= 7 &&
		(nt.AnchorWeekOfMonth.Int16 == -1 || (nt.AnchorWeekOfMonth.Int16 >= 1 && nt.AnchorWeekOfMonth.Int16 <= 5))
}

// nthWeekdayOfMonth returns the date of the ordinal-th `weekday` (ISO 1..7) in
// the given month. ordinal 1..5 counts from the start; ordinal -1 is the last
// occurrence. When an ordinal beyond the month's occurrences is requested
// (e.g. a 5th Monday that does not exist), it clamps to the last occurrence,
// so the planner always yields a real date in that month.
func nthWeekdayOfMonth(year int, month time.Month, weekday int, ordinal int) time.Time {
	if ordinal == -1 {
		last := dateOnly(time.Date(year, month+1, 1, 0, 0, 0, 0, time.UTC)).AddDate(0, 0, -1)
		for isoWeekday(last) != weekday {
			last = last.AddDate(0, 0, -1)
		}
		return last
	}
	first := dateOnly(time.Date(year, month, 1, 0, 0, 0, 0, time.UTC))
	for isoWeekday(first) != weekday {
		first = first.AddDate(0, 0, 1)
	}
	candidate := first.AddDate(0, 0, (ordinal-1)*7)
	if candidate.Month() != month {
		candidate = candidate.AddDate(0, 0, -7) // requested ordinal overflowed → last occurrence
	}
	return candidate
}

// monthlyMeetingDate returns the scheduled meeting date within ref's month for
// a monthly week-anchored type.
func monthlyMeetingDate(nt cerebrodb.CerebroNoteType, ref time.Time) time.Time {
	return nthWeekdayOfMonth(ref.Year(), ref.Month(), int(nt.AnchorWeekday.Int16), int(nt.AnchorWeekOfMonth.Int16))
}

func periodAnchor(nt cerebrodb.CerebroNoteType) time.Time {
	anchor := nt.CreatedAt.Time
	if nt.CadenceUnit != CadenceWeek || !nt.AnchorWeekday.Valid {
		return anchor
	}
	target := int(nt.AnchorWeekday.Int16)
	if target < 1 || target > 7 {
		return anchor
	}
	day := dateOnly(anchor)
	for isoWeekday(day) != target {
		day = day.AddDate(0, 0, -1)
	}
	return day
}

func floorDiv(a, b int) int {
	if b == 0 {
		return 0
	}
	return int(math.Floor(float64(a) / float64(b)))
}

func anchoredPeriodStart(anchor, t time.Time, unit string, count int32) time.Time {
	count = normalizeCount(count)
	if unit != CadenceWeek {
		return dateOnly(t)
	}
	weeksFromAnchor := intervalIndex(anchor, t, CadenceWeek)
	window := floorDiv(weeksFromAnchor, int(count))
	return dateOnly(anchor).AddDate(0, 0, window*int(count)*7)
}

func shouldSweepApply(nt cerebrodb.CerebroNoteType, now time.Time) bool {
	// Monthly "Nth weekday of the month" types fire on the meeting date itself,
	// not on the first of the month, and skip the period that was already
	// current when the type was created (mirrors the weekly-anchor behaviour).
	if hasMonthlyWeekAnchor(nt) {
		count := int(normalizeCount(nt.CadenceCount))
		if intervalIndex(dateOnly(nt.CreatedAt.Time), now, CadenceMonth)%count != 0 {
			return false // not an on-cadence month
		}
		meeting := monthlyMeetingDate(nt, now)
		return !dateOnly(now).Before(meeting) && meeting.After(dateOnly(nt.CreatedAt.Time))
	}
	if nt.CadenceUnit != CadenceWeek || !nt.AnchorWeekday.Valid {
		return true
	}
	currentStart := anchoredPeriodStart(periodAnchor(nt), now, nt.CadenceUnit, nt.CadenceCount)
	return currentStart.After(dateOnly(nt.CreatedAt.Time))
}

// daysInMonth returns the number of days in the given calendar month.
func daysInMonth(year int, month time.Month) int {
	return dateOnly(time.Date(year, month+1, 1, 0, 0, 0, 0, time.UTC)).AddDate(0, 0, -1).Day()
}

// monthPeriodDate returns the scheduled date for a monthly/quarterly type in
// the month `pm` points at: the Nth weekday when week-anchored, otherwise the
// creation day-of-month clamped to the target month's length.
func monthPeriodDate(nt cerebrodb.CerebroNoteType, pm time.Time) time.Time {
	if hasMonthlyWeekAnchor(nt) {
		return nthWeekdayOfMonth(pm.Year(), pm.Month(), int(nt.AnchorWeekday.Int16), int(nt.AnchorWeekOfMonth.Int16))
	}
	day := nt.CreatedAt.Time.Day()
	if max := daysInMonth(pm.Year(), pm.Month()); day > max {
		day = max
	}
	return dateOnly(time.Date(pm.Year(), pm.Month(), day, 0, 0, 0, 0, time.UTC))
}

// NextOccurrence returns the next scheduled meeting date at or after `now`
// (whole-day granularity) for any non-manual cadence. ok is false for manual
// types, which have no projected schedule.
func NextOccurrence(nt cerebrodb.CerebroNoteType, now time.Time) (time.Time, bool) {
	dates := Upcoming(nt, now, 1)
	if len(dates) == 0 {
		return time.Time{}, false
	}
	return dates[0], true
}

// UpcomingUntil returns the scheduled meeting dates at or after `now` and at or
// before `until`, capped at `cap` entries — the source for the year-wheel
// overview. The cap bounds high-frequency cadences (daily/weekly) so the
// response never grows unbounded.
func UpcomingUntil(nt cerebrodb.CerebroNoteType, now, until time.Time, cap int) []time.Time {
	if cap <= 0 {
		return nil
	}
	all := Upcoming(nt, now, cap)
	out := make([]time.Time, 0, len(all))
	for _, d := range all {
		if d.After(dateOnly(until)) {
			break
		}
		out = append(out, d)
	}
	return out
}

// Upcoming returns up to n scheduled meeting dates at or after `now`, in
// ascending order. It is the single source of truth for the recurring planner:
// the same date maths the sweeper uses to decide when to materialise a note.
func Upcoming(nt cerebrodb.CerebroNoteType, now time.Time, n int) []time.Time {
	if n <= 0 || nt.CadenceUnit == CadenceManual {
		return nil
	}
	today := dateOnly(now)
	out := make([]time.Time, 0, n)

	switch nt.CadenceUnit {
	case CadenceDay, CadenceWeek:
		stepDays := int(normalizeCount(nt.CadenceCount))
		anchor := dateOnly(nt.CreatedAt.Time)
		if nt.CadenceUnit == CadenceWeek {
			anchor = periodAnchor(nt)
			stepDays *= 7
		}
		first := anchor
		if !anchor.After(today) {
			days := int(today.Sub(anchor).Hours()) / 24
			if rem := days % stepDays; rem == 0 {
				first = today
			} else {
				first = today.AddDate(0, 0, stepDays-rem)
			}
		}
		for i := 0; i < n; i++ {
			out = append(out, first.AddDate(0, 0, i*stepDays))
		}
	case CadenceMonth, CadenceQuarter:
		monthsStep := int(normalizeCount(nt.CadenceCount))
		if nt.CadenceUnit == CadenceQuarter {
			monthsStep *= 3
		}
		anchor := dateOnly(time.Date(nt.CreatedAt.Time.Year(), nt.CreatedAt.Time.Month(), 1, 0, 0, 0, 0, time.UTC))
		monthsBetween := intervalIndex(anchor, today, CadenceMonth)
		startIdx := 0
		if monthsBetween > 0 {
			startIdx = (monthsBetween / monthsStep) * monthsStep
		}
		for i := 0; len(out) < n && i < 400; i++ {
			pm := anchor.AddDate(0, (startIdx + i*monthsStep), 0)
			date := monthPeriodDate(nt, pm)
			if date.Before(today) {
				continue
			}
			out = append(out, date)
		}
	}
	return out
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
