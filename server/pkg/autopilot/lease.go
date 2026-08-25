// Package autopilot holds the cross-cutting autopilot infrastructure that
// is shared between the dispatch service and background workers. It is a
// leaf package (no server-internal dependencies) so both the service layer
// and the sweeper can use it without import cycles.
package autopilot

import "time"

const (
	// MinLeaseDuration is the hard floor for any computed lease. It exists
	// to stop a misconfigured (too aggressive) lease from killing long
	// legitimate runs: below this value, autopilot dispatch is effectively
	// broken, so the config is clamped instead of honored.
	MinLeaseDuration = 5 * time.Minute

	// DefaultLeaseTimeout is the base in-flight lease for an autopilot run
	// when AUTOPILOT_RUN_LEASE_TIMEOUT is unset. After this much wall time a
	// run stuck in issue_created/running is treated as stale: the dispatch
	// gate terminalizes it (failed + lease_expired) and admits the next slot.
	DefaultLeaseTimeout = 30 * time.Minute
)

// LeaseConfig is the input to CalculateLeaseDuration. BaseTimeout is the
// operator-configured floor (AUTOPILOT_RUN_LEASE_TIMEOUT); SlotInterval is
// the autopilot's scheduling cadence when known (max cron interval between
// two consecutive occurrences).
type LeaseConfig struct {
	// BaseTimeout is the base in-flight lease duration.
	BaseTimeout time.Duration
	// SlotInterval is the scheduling cadence. When zero, it never dominates.
	SlotInterval time.Duration
}

// CalculateLeaseDuration returns the bounded lease for an in-flight run:
// the larger of the base timeout and the slot interval, so the lease always
// covers at least one full scheduling cycle. The result is clamped at
// MinLeaseDuration so an undersized configuration can never cause
// over-aggressive stale-run cleanup.
func (c *LeaseConfig) CalculateLeaseDuration() time.Duration {
	lease := c.BaseTimeout
	if c.SlotInterval > lease {
		lease = c.SlotInterval
	}
	if lease < MinLeaseDuration {
		return MinLeaseDuration
	}
	return lease
}
