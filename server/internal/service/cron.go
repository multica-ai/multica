package service

import (
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

// cronParser accepts standard 5-field cron expressions.
var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// ComputeNextRun parses a cron expression and returns the next fire time
// in the given timezone.
func ComputeNextRun(cronExpr, timezone string) (time.Time, error) {
	sched, err := cronParser.Parse(cronExpr)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse cron: %w", err)
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timezone %q: %w", timezone, err)
	}
	return sched.Next(time.Now().In(loc)), nil
}

// ValidateTimezone returns an error if the timezone string is not recognized.
func ValidateTimezone(timezone string) error {
	_, err := time.LoadLocation(timezone)
	if err != nil {
		return fmt.Errorf("invalid timezone %q: %w", timezone, err)
	}
	return nil
}
