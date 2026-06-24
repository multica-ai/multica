package service

import (
	"fmt"
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

// ComputeNextRun is the legacy wrapper used by the trigger create/update
// handlers and the failure monitor. It evaluates the cron at the app's
// local now() — kept for the display-only autopilot_trigger.next_run_at
// column so we do not have to thread DB time through every UI write
// path in this same change. Scheduling decisions MUST go through
// NextOccurrencesUTC against DB time instead.
//
// MUL-3551: this function is on its way out; new callers should use
// NextOccurrenceAfterUTC with a db_now() input instead.
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
