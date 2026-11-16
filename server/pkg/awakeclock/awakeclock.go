// Package awakeclock reads a clock that advances only while the host is
// awake. Liveness budgets (the daemon's idle watchdog, GitHub #9024) must
// measure it rather than wall time: nothing can run while the host is
// suspended, so slept time says nothing about whether a process is hung.
//
// Readings are durations from an arbitrary process-wide origin. Only the
// difference between two readings is meaningful, and absolute values may be
// negative (for example inside a testing/synctest bubble).
package awakeclock

import "time"

// origin carries Go's monotonic reading. time.Since(origin) therefore uses
// the runtime's monotonic clock, which already stops during suspend on
// Linux (CLOCK_MONOTONIC) and macOS (mach_absolute_time). On Windows the
// runtime clock keeps counting through sleep, so sleptSinceOrigin
// subtracts it there; it returns 0 everywhere else.
var origin = time.Now()

// Now returns the awake time elapsed since the process-wide origin.
func Now() time.Duration {
	return time.Since(origin) - sleptSinceOrigin()
}
