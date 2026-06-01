package sprints

import (
	"fmt"
	"strings"
	"time"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

// Cadence units accepted in settings + recurring tasks. The runtime never
// hardcodes a unit string outside this set; callers compare against these
// constants so adding a new unit (e.g. "year") is a one-place change.
const (
	UnitDay   = "day"
	UnitWeek  = "week"
	UnitMonth = "month"
)

// Sprint statuses.
const (
	StatusPlanned   = "planned"
	StatusActive    = "active"
	StatusDone      = "done"
	StatusCancelled = "cancelled"
)

// ValidUnits is the source-of-truth list used by validators. Exported so
// the handler can echo the allowed values in its 400 response.
var ValidUnits = []string{UnitDay, UnitWeek, UnitMonth}

// ValidateUnit returns nil when unit is one of the supported cadence units.
func ValidateUnit(unit string) error {
	for _, u := range ValidUnits {
		if u == unit {
			return nil
		}
	}
	return fmt.Errorf("invalid unit %q, expected one of: %s", unit, strings.Join(ValidUnits, ", "))
}

// ComputeNextStart derives the start date of the sprint that follows
// `previousEnd`. The result aligns to start_weekday when unit=week and to
// the first day of the following calendar month when unit=month. The
// computation is intentionally pure so it can be reused by the handler
// (preview), the sweeper, and tests.
//
// previousEnd is the *inclusive* end of the previous sprint, so the next
// sprint starts on previousEnd+1day (and is then snapped onto start_weekday
// for week-cadence). For day/month cadences start_weekday is ignored.
func ComputeNextStart(previousEnd time.Time, unit string, startWeekday int) time.Time {
	day := truncateDay(previousEnd).AddDate(0, 0, 1)
	switch unit {
	case UnitWeek:
		// ISO weekday: 1=Mon..7=Sun. time.Weekday: Sun=0..Sat=6.
		current := isoWeekday(day)
		target := normalizeISOWeekday(startWeekday)
		offset := (target - current + 7) % 7
		return day.AddDate(0, 0, offset)
	case UnitMonth:
		// Always snap to the first of the next month.
		return time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, day.Location())
	default:
		return day
	}
}

// ComputeEnd returns the inclusive end date of a sprint that starts on
// `start` with the given (unit, count). End is start + N units - 1 day.
func ComputeEnd(start time.Time, unit string, count int) time.Time {
	start = truncateDay(start)
	if count <= 0 {
		count = 1
	}
	var end time.Time
	switch unit {
	case UnitWeek:
		end = start.AddDate(0, 0, 7*count)
	case UnitMonth:
		end = start.AddDate(0, count, 0)
	default:
		end = start.AddDate(0, 0, count)
	}
	return end.AddDate(0, 0, -1)
}

// ApplyNameTemplate substitutes the {n} placeholder in template with the
// 1-based sequence number. Missing placeholder produces a trailing " N"
// suffix so admins can't accidentally have every sprint share a name.
func ApplyNameTemplate(template string, sequenceNo int32) string {
	if strings.Contains(template, "{n}") {
		return strings.ReplaceAll(template, "{n}", fmt.Sprintf("%d", sequenceNo))
	}
	if template == "" {
		return fmt.Sprintf("Sprint %d", sequenceNo)
	}
	return fmt.Sprintf("%s %d", template, sequenceNo)
}

// CadenceMatches reports whether a recurring task's cadence matches a
// sprint's duration. Required so a monthly template rolls into monthly
// sprints but not weekly ones.
func CadenceMatches(settings cerebrodb.CerebroSprintSetting, taskUnit string, taskCount int32) bool {
	return settings.DurationUnit == taskUnit && settings.DurationCount == taskCount
}

// LoadTimezone parses settings.Timezone with a UTC fallback so a bad value
// in the DB never crashes the sweeper.
func LoadTimezone(name string) *time.Location {
	if name == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}

// TodayIn returns the start-of-day in the given timezone, for sweeper
// comparisons that must be wall-clock day boundaries.
func TodayIn(loc *time.Location, now time.Time) time.Time {
	if loc == nil {
		loc = time.UTC
	}
	local := now.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
}

func truncateDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func isoWeekday(t time.Time) int {
	w := int(t.Weekday())
	if w == 0 {
		return 7
	}
	return w
}

func normalizeISOWeekday(w int) int {
	if w < 1 {
		return 1
	}
	if w > 7 {
		return 7
	}
	return w
}
