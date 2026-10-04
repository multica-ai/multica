//go:build !windows

package daemon

import "time"

// Go uses CLOCK_MONOTONIC on Linux and mach_absolute_time on Darwin; both
// exclude suspend. Keep the monotonic reading: UTC, UnixNano and serialization
// strip it and reintroduce clock adjustments/sleep into inactivity accounting.
func newWatchdogClock() func() time.Duration {
	start := time.Now()
	return func() time.Duration { return time.Since(start) }
}
