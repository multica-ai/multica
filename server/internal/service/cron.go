package service

import (
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

// cronParser accepts standard 5-field cron expressions.
var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// ComputeNextRun parses a cron expression and returns the next fire time
// in the given timezone, relative to the current process time.
func ComputeNextRun(cronExpr, timezone string) (time.Time, error) {
	return ComputeNextRunAfter(cronExpr, timezone, time.Now())
}

// ComputeNextRunAfter parses a cron expression and returns the next fire time
// strictly after the given reference instant, evaluated in the given timezone.
//
// Callers advancing a schedule past an occurrence they just fired must pass a
// reference that is at least that occurrence (see NextScheduleReference);
// otherwise a node whose clock lags the database clock can land back on the
// same occurrence and re-fire it.
func ComputeNextRunAfter(cronExpr, timezone string, after time.Time) (time.Time, error) {
	sched, err := cronParser.Parse(cronExpr)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse cron: %w", err)
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timezone %q: %w", timezone, err)
	}
	return sched.Next(after.In(loc)), nil
}

// NextScheduleReference returns the instant from which to compute a schedule's
// next run after firing an occurrence. It is the later of the process clock
// (now) and the occurrence that was just fired (firedAt), so a scheduler node
// whose local clock lags the database clock never recomputes — and re-fires —
// the same occurrence. A zero firedAt means there is no just-fired occurrence
// to advance past, so now is used.
func NextScheduleReference(now, firedAt time.Time) time.Time {
	if firedAt.After(now) {
		return firedAt
	}
	return now
}

// ValidateTimezone returns an error if the timezone string is not recognized.
func ValidateTimezone(timezone string) error {
	_, err := time.LoadLocation(timezone)
	if err != nil {
		return fmt.Errorf("invalid timezone %q: %w", timezone, err)
	}
	return nil
}
