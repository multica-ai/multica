package service

import (
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

// cronParser is the shared parser used by the schedule service. It accepts
// the standard 5-field POSIX cron syntax (minute granularity) and a handful
// of convenient descriptors ("@daily", "@hourly", ...). Seconds fields are
// intentionally *not* supported to keep schedule behavior consistent with
// what users see in their crontab.
var cronParser = cron.NewParser(
	cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
)

// ParseCron validates a cron expression and returns a cron.Schedule ready to
// compute fire times. The returned schedule is timezone-agnostic — callers
// apply a timezone when computing NextFireTime.
func ParseCron(expr string) (cron.Schedule, error) {
	if expr == "" {
		return nil, fmt.Errorf("cron expression is required")
	}
	sched, err := cronParser.Parse(expr)
	if err != nil {
		return nil, fmt.Errorf("invalid cron expression %q: %w", expr, err)
	}
	return sched, nil
}

// LoadTimezone wraps time.LoadLocation with a friendlier error message and a
// UTC default for the empty string.
func LoadTimezone(tz string) (*time.Location, error) {
	if tz == "" {
		return time.UTC, nil
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone %q: %w", tz, err)
	}
	return loc, nil
}

// NextFireTime computes the next time `expr` fires after `after`, interpreted
// in the given timezone. The returned time is in UTC (safe for DB storage).
//
// This is a convenience wrapper around ParseCron + LoadTimezone so that
// callers who only need the next fire time can make a single call.
func NextFireTime(expr, timezone string, after time.Time) (time.Time, error) {
	sched, err := ParseCron(expr)
	if err != nil {
		return time.Time{}, err
	}
	loc, err := LoadTimezone(timezone)
	if err != nil {
		return time.Time{}, err
	}
	// cron.Schedule.Next uses the zone of the input time. We convert the
	// "after" instant into the schedule's zone, compute next, then convert
	// back to UTC for storage.
	local := after.In(loc)
	next := sched.Next(local)
	return next.UTC(), nil
}
