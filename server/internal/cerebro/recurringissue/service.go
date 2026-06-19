package recurringissue

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Recurrence frequencies accepted by the issue-side panel (FIR-334 mockup).
const (
	FreqDaily        = "daily"
	FreqWeekly       = "weekly"
	FreqMonthly      = "monthly"
	FreqYearly       = "yearly"
	FreqDaysAfter    = "days_after"
	FreqCustom       = "custom"
	FreqEveryWeekday = "every_weekday"
)

// Anchor modes — where the next due date is measured from.
const (
	AnchorCompletion = "completion" // from the day the issue was completed
	AnchorDueDate    = "due_date"   // from the previous due date ("sync to due date")
)

// ValidFrequencies is the source-of-truth list used by validators; exported
// so the handler can echo allowed values in its 400 response.
var ValidFrequencies = []string{FreqDaily, FreqWeekly, FreqMonthly, FreqYearly, FreqDaysAfter, FreqCustom, FreqEveryWeekday}

// ValidAnchors mirrors the anchor CHECK constraint.
var ValidAnchors = []string{AnchorCompletion, AnchorDueDate}

// ValidateFrequency returns nil when freq is one of the supported values.
func ValidateFrequency(freq string) error {
	for _, f := range ValidFrequencies {
		if f == freq {
			return nil
		}
	}
	return fmt.Errorf("invalid frequency %q, expected one of: %s", freq, strings.Join(ValidFrequencies, ", "))
}

// ValidateAnchor returns nil when anchor is supported.
func ValidateAnchor(anchor string) error {
	for _, a := range ValidAnchors {
		if a == anchor {
			return nil
		}
	}
	return fmt.Errorf("invalid anchor %q, expected one of: %s", anchor, strings.Join(ValidAnchors, ", "))
}

// NextDueDate derives the due date of the next occurrence from a base date.
//
// base is the day the recurrence is measured from: for anchor=completion it
// is the completion day (today), for anchor=due_date it is the previous due
// date. The result is intentionally pure so the handler (preview), the
// sweeper and the tests share one implementation.
//
//   - daily       → base + interval days
//   - weekly      → base + interval weeks, then snapped onto the next
//     configured weekday when weekdays is non-empty
//   - monthly     → base + interval months
//   - yearly      → base + interval years
//   - days_after  → base + daysAfter days (interval ignored)
//   - custom      → treated as weekly (every N weeks + weekday picker), which
//     is exactly the FIR-334 "Custom… Every N week(s)" mockup
func NextDueDate(base time.Time, freq string, interval int, weekdays []int, daysAfter int) time.Time {
	base = truncateDay(base)
	if interval <= 0 {
		interval = 1
	}
	switch freq {
	case FreqDaily:
		return base.AddDate(0, 0, interval)
	case FreqMonthly:
		return base.AddDate(0, interval, 0)
	case FreqYearly:
		return base.AddDate(interval, 0, 0)
	case FreqDaysAfter:
		if daysAfter < 0 {
			daysAfter = 0
		}
		return base.AddDate(0, 0, daysAfter)
	case FreqWeekly, FreqCustom:
		next := base.AddDate(0, 0, 7*interval)
		return snapToWeekday(next, weekdays)
	case FreqEveryWeekday:
		next := base.AddDate(0, 0, 1)
		for isoWeekday(next) > 5 {
			next = next.AddDate(0, 0, 1)
		}
		return next
	default:
		return base.AddDate(0, 0, interval)
	}
}

// snapToWeekday moves day forward (including itself) to the nearest ISO
// weekday present in weekdays. Empty weekdays leaves day unchanged.
func snapToWeekday(day time.Time, weekdays []int) time.Time {
	set := normalizeWeekdays(weekdays)
	if len(set) == 0 {
		return day
	}
	for i := 0; i < 7; i++ {
		cand := day.AddDate(0, 0, i)
		if containsInt(set, isoWeekday(cand)) {
			return cand
		}
	}
	return day
}

// normalizeWeekdays clamps to ISO 1..7, dedupes and sorts. Out-of-range
// values are dropped so a bad payload can never wedge the snap loop.
func normalizeWeekdays(weekdays []int) []int {
	seen := map[int]bool{}
	out := make([]int, 0, len(weekdays))
	for _, w := range weekdays {
		if w < 1 || w > 7 || seen[w] {
			continue
		}
		seen[w] = true
		out = append(out, w)
	}
	sort.Ints(out)
	return out
}

func containsInt(xs []int, v int) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
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

// LoadTimezone parses a timezone name with a UTC fallback.
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

// TodayIn returns the start-of-day in loc for the given instant.
func TodayIn(loc *time.Location, now time.Time) time.Time {
	if loc == nil {
		loc = time.UTC
	}
	local := now.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
}
