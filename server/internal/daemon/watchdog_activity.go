package daemon

import (
	"sync/atomic"
	"time"
)

// watchdogActivity measures silence in awake time, not civil time. The clock
// and the stored offsets must share an epoch; neither is a Unix timestamp.
type watchdogActivity struct {
	now  func() time.Duration
	last atomic.Int64
}

func newWatchdogActivity(now func() time.Duration) *watchdogActivity {
	a := &watchdogActivity{now: now}
	a.last.Store(int64(now()))
	return a
}

func (a *watchdogActivity) record() {
	next := int64(a.now())
	for {
		previous := a.last.Load()
		if next <= previous || a.last.CompareAndSwap(previous, next) {
			return
		}
	}
}

// trackTools is polled only by the watchdog goroutine. A backend timestamp is
// a change marker, never an offset in our clock: it may have lost its monotonic
// reading, and Windows' Go clock includes suspend. Newly observed native tool
// activity gets a fresh budget even if its transcript was dropped. Observation
// can lag the transition by up to one watchdog tick; repeated polls do not
// extend the budget. Snapshot the initial marker so stale startup state does
// not count as new activity on the first poll.
func (a *watchdogActivity) trackTools(report func() (int32, time.Time)) func() int32 {
	_, previous := report()
	return func() int32 {
		count, at := report()
		if !at.IsZero() && !at.Equal(previous) {
			previous = at
			a.record()
		}
		return count
	}
}
