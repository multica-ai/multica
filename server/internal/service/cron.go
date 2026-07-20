package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// cronParser accepts standard 5-field cron expressions.
var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// NextOccurrenceAfterUTC parses cronExpr in the named IANA timezone and
// returns the next activation strictly after `after`. The result is always
// in UTC and represents the canonical fire time of the next occurrence.
//
// `after` is interpreted as an absolute instant; callers should pass DB
// time (e.g. `SELECT now()`) rather than `time.Now()` so that two
// app instances with skewed clocks still produce the same answer.
//
// This is the building block the new scheduler.AutopilotScheduleDispatchJob
// uses to compute plan_times; the handler / UI write paths use it via
// ComputeNextRunFromUTC to fill in the display-only
// autopilot_trigger.next_run_at column.
func NextOccurrenceAfterUTC(cronExpr, timezone string, after time.Time) (time.Time, error) {
	sched, loc, err := parseCronSchedule(cronExpr, timezone)
	if err != nil {
		return time.Time{}, err
	}
	return sched.Next(after.In(loc)).UTC(), nil
}

// NextOccurrencesAfterUTC parses cronExpr in `timezone` once and returns the
// next `count` activations strictly after `after`, ascending, in UTC. It stops
// early — returning a shorter slice — when the expression has no further
// occurrence within robfig's five-year search horizon (e.g. "0 0 30 2 *"), which
// robfig reports as the zero time.
//
// Unlike calling NextOccurrenceAfterUTC in a loop, the expression is parsed and
// the location loaded exactly once. The schedule editor's preview endpoint asks
// for this on every debounced keystroke, so the difference is not academic.
func NextOccurrencesAfterUTC(cronExpr, timezone string, after time.Time, count int) ([]time.Time, error) {
	sched, loc, err := parseCronSchedule(cronExpr, timezone)
	if err != nil {
		return nil, err
	}
	out := make([]time.Time, 0, count)
	cursor := after.In(loc)
	for i := 0; i < count; i++ {
		next := sched.Next(cursor)
		if next.IsZero() {
			break
		}
		out = append(out, next.UTC())
		cursor = next
	}
	return out, nil
}

// NextOccurrencesUTC parses cronExpr in `timezone` and returns every
// activation in the half-open interval `(after, until]`, in canonical
// UTC order (ascending). Used by the Autopilot schedule dispatch job to
// enumerate every plan_time that became due between the last stored
// occurrence and DB now().
//
// The slice is capped at 1024 entries — a safety net against an
// accidental "every second" cron over a multi-day catch-up window.
// The scheduler manager additionally caps the returned slice at
// JobSpec.MaxPlansPerTick.
func NextOccurrencesUTC(cronExpr, timezone string, after, until time.Time) ([]time.Time, error) {
	sched, loc, err := parseCronSchedule(cronExpr, timezone)
	if err != nil {
		return nil, err
	}
	const hardCap = 1024
	out := make([]time.Time, 0, 8)
	cursor := after.In(loc)
	untilLocal := until.In(loc)
	for len(out) < hardCap {
		next := sched.Next(cursor)
		if next.After(untilLocal) {
			break
		}
		out = append(out, next.UTC())
		cursor = next
	}
	return out, nil
}

// ComputeNextRun evaluates the cron at the app's local now() and backs
// the display-only autopilot_trigger.next_run_at column for the trigger
// create/update handlers and the failure monitor. Using the local clock
// for this display value is deliberate: app/DB clock skew under NTP is far
// below the column's minute-level granularity, so threading DB time
// through these UI write paths would buy no user-visible accuracy.
//
// The scheduler's post-dispatch advance keeps next_run_at on the same
// local clock but calls NextOccurrenceAfterUTC anchored at
// max(now, plan_time) rather than this helper, so a lagging app clock can
// never re-point the column at the slot that just fired (see
// scheduler.advancedNextRun / autopilotHandler, MUL-3749).
//
// Scheduling decisions are a separate concern and MUST go through
// NextOccurrencesUTC / NextOccurrenceAfterUTC against DB time instead:
// dispatch correctness across clock-skewed app instances depends on it.
func ComputeNextRun(cronExpr, timezone string) (time.Time, error) {
	return NextOccurrenceAfterUTC(cronExpr, timezone, time.Now())
}

// ValidateTimezone returns an error if the timezone string is not recognized.
func ValidateTimezone(timezone string) error {
	_, err := time.LoadLocation(timezone)
	if err != nil {
		return fmt.Errorf("invalid timezone %q: %w", timezone, err)
	}
	return nil
}

func parseCronSchedule(cronExpr, timezone string) (cron.Schedule, *time.Location, error) {
	// robfig v3.0.1 reads an optional "TZ="/"CRON_TZ=" prefix up to the first
	// space and panics (parser.go:99, slice[:-1]) when that space is missing.
	// The preview endpoint feeds raw user text here, so reject the shape the
	// parser cannot survive instead of turning a typo into a 500.
	if (strings.HasPrefix(cronExpr, "TZ=") || strings.HasPrefix(cronExpr, "CRON_TZ=")) &&
		!strings.Contains(cronExpr, " ") {
		return nil, nil, fmt.Errorf("parse cron: missing schedule after timezone prefix %q", cronExpr)
	}
	sched, err := cronParser.Parse(cronExpr)
	if err != nil {
		return nil, nil, fmt.Errorf("parse cron: %w", err)
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid timezone %q: %w", timezone, err)
	}
	return sched, loc, nil
}
