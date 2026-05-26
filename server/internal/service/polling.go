package service

import "time"

const DefaultPollingIntervalMinutes = 30

func normalizePollingIntervalMinutes(intervalMinutes int32) int32 {
	if intervalMinutes > 0 {
		return intervalMinutes
	}
	return DefaultPollingIntervalMinutes
}

func normalizePollingStartAt(startAt time.Time, now time.Time) time.Time {
	if startAt.IsZero() {
		return now.UTC().Truncate(time.Second)
	}
	return startAt.UTC().Truncate(time.Second)
}

// InitialPollingNextRun returns the first execution time for a polling issue.
// If the configured start is already in the past, it advances to the next
// future boundary so the scheduler never runs the same slot twice.
func InitialPollingNextRun(startAt time.Time, intervalMinutes int32, now time.Time) time.Time {
	start := normalizePollingStartAt(startAt, now)
	interval := time.Duration(normalizePollingIntervalMinutes(intervalMinutes)) * time.Minute
	if !start.Before(now) {
		return start
	}

	elapsed := now.Sub(start)
	steps := int(elapsed/interval) + 1
	return start.Add(time.Duration(steps) * interval)
}

// AdvancePollingNextRun returns the next run after a completed execution.
// It advances past now so a lagging scheduler can catch up cleanly.
func AdvancePollingNextRun(previous time.Time, intervalMinutes int32, now time.Time) time.Time {
	interval := time.Duration(normalizePollingIntervalMinutes(intervalMinutes)) * time.Minute
	next := normalizePollingStartAt(previous, now).Add(interval)
	if !next.Before(now) {
		return next
	}

	elapsed := now.Sub(previous)
	steps := int(elapsed/interval) + 1
	return previous.Add(time.Duration(steps) * interval)
}
