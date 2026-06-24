package service

import (
	"fmt"
	"os"
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

// DefaultMinTriggerInterval is the floor enforced between consecutive
// schedule-trigger fires unless overridden via AUTOPILOT_MIN_TRIGGER_INTERVAL.
// A runaway high-frequency autopilot is the single biggest token-burn risk
// (each fire dispatches an agent run), so the default is deliberately
// conservative.
const DefaultMinTriggerInterval = 5 * time.Minute

// minTriggerIntervalSamples is how many consecutive fire times are inspected
// when validating a cron expression against the interval floor. Eight samples
// catch both uniform high-frequency patterns (* * * * *) and bursty lists
// (0,1,2 * * * *) without noticeably slowing down trigger creation.
const minTriggerIntervalSamples = 8

// MinTriggerInterval returns the enforced floor between consecutive schedule
// fires. AUTOPILOT_MIN_TRIGGER_INTERVAL accepts a Go duration string
// (e.g. "1m", "30s"); an explicit zero or negative value disables the check.
// Unparseable values fall back to the default rather than silently disabling
// the guard.
func MinTriggerInterval() time.Duration {
	raw := os.Getenv("AUTOPILOT_MIN_TRIGGER_INTERVAL")
	if raw == "" {
		return DefaultMinTriggerInterval
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return DefaultMinTriggerInterval
	}
	if d < 0 {
		return 0
	}
	return d
}

// ValidateCronMinInterval rejects cron expressions whose consecutive fire
// times are closer together than the configured floor. The expression is
// assumed to be already parseable (callers validate via ComputeNextRun);
// parse errors are still returned for safety.
func ValidateCronMinInterval(cronExpr, timezone string) error {
	return validateCronMinInterval(cronExpr, timezone, MinTriggerInterval())
}

func validateCronMinInterval(cronExpr, timezone string, floor time.Duration) error {
	if floor <= 0 {
		return nil
	}
	sched, err := cronParser.Parse(cronExpr)
	if err != nil {
		return fmt.Errorf("parse cron: %w", err)
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return fmt.Errorf("invalid timezone %q: %w", timezone, err)
	}
	prev := sched.Next(time.Now().In(loc))
	for i := 0; i < minTriggerIntervalSamples; i++ {
		next := sched.Next(prev)
		if next.IsZero() {
			// The expression stops firing within the sampled window.
			return nil
		}
		if gap := next.Sub(prev); gap < floor {
			return fmt.Errorf(
				"cron expression %q fires runs %s apart; the minimum allowed interval between runs is %s (operators can tune AUTOPILOT_MIN_TRIGGER_INTERVAL)",
				cronExpr, gap, floor,
			)
		}
		prev = next
	}
	return nil
}

// ValidateTimezone returns an error if the timezone string is not recognized.
func ValidateTimezone(timezone string) error {
	_, err := time.LoadLocation(timezone)
	if err != nil {
		return fmt.Errorf("invalid timezone %q: %w", timezone, err)
	}
	return nil
}
