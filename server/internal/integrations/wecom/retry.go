package wecom

import (
	"context"
	"sync"
	"time"
)

// AuthFailDelays is the installation-scoped backoff streak applied when
// aibot_subscribe returns a non-zero errcode. The last entry repeats
// indefinitely; the streak is reset by a successful subscribe.
var AuthFailDelays = []time.Duration{
	5 * time.Minute,
	15 * time.Minute,
	30 * time.Minute,
	1 * time.Hour,
}

// KickDelays is the installation-scoped backoff streak applied when the
// WeCom server pushes a disconnected_event kick. The last entry repeats
// indefinitely; the streak is reset once the *next* connection stays up
// for KickStableWindow (see NoteKick).
var KickDelays = []time.Duration{
	60 * time.Second,
	2 * time.Minute,
	4 * time.Minute,
	8 * time.Minute,
	15 * time.Minute,
}

// KickStableWindow is the minimum uptime that resets the kick streak.
const KickStableWindow = 5 * time.Minute

// RetryState tracks installation-scoped auth and kick streaks. Every
// concurrent WeCom Connect for the same installation must share one
// RetryState so Supervisor rebuilds do not clear the backoff. All methods
// are safe for concurrent use.
type RetryState struct {
	mu          sync.Mutex
	now         func() time.Time
	authStreak  int
	kickStreak  int
	connectedAt time.Time
}

// NewRetryState returns a RetryState with time.Now as its clock.
func NewRetryState() *RetryState {
	return &RetryState{now: time.Now}
}

// NewRetryStateWithClock returns a RetryState wired to a custom clock;
// exported so unit tests can advance time deterministically.
func NewRetryStateWithClock(now func() time.Time) *RetryState {
	if now == nil {
		now = time.Now
	}
	return &RetryState{now: now}
}

// NoteAuthFail advances the auth streak and returns the delay to sleep
// before the next Connect attempt.
func (r *RetryState) NoteAuthFail() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	d := lookupStreak(AuthFailDelays, r.authStreak)
	r.authStreak++
	return d
}

// NoteAuthSuccess clears the auth streak and records the timestamp used
// to decide whether the *next* kick should reset the kick streak.
func (r *RetryState) NoteAuthSuccess() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.authStreak = 0
	r.connectedAt = r.now()
}

// NoteKick advances the kick streak and returns the delay to sleep.
// If the connection stayed up for at least KickStableWindow before the
// kick, the streak is reset to zero *before* the advance so the delay
// starts again from KickDelays[0].
func (r *RetryState) NoteKick() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.connectedAt.IsZero() && r.now().Sub(r.connectedAt) >= KickStableWindow {
		r.kickStreak = 0
	}
	d := lookupStreak(KickDelays, r.kickStreak)
	r.kickStreak++
	return d
}

// Reset clears both streaks. Intended for revoked installations or tests.
func (r *RetryState) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.authStreak = 0
	r.kickStreak = 0
	r.connectedAt = time.Time{}
}

// AuthStreak returns the current auth streak counter (exported for tests).
func (r *RetryState) AuthStreak() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.authStreak
}

// KickStreak returns the current kick streak counter (exported for tests).
func (r *RetryState) KickStreak() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.kickStreak
}

func lookupStreak(delays []time.Duration, i int) time.Duration {
	if len(delays) == 0 {
		return 0
	}
	if i < 0 {
		i = 0
	}
	if i >= len(delays)-1 {
		return delays[len(delays)-1]
	}
	return delays[i]
}

// RetryRegistry hands out one *RetryState per installation and remembers it
// across calls. It exists because the engine.Supervisor calls the channel
// Factory fresh on every reconnect attempt (see supervisor.go's supervise
// loop): without a registry shared by the Factory closure, each rebuilt
// Channel would start a brand new RetryState and the auth-fail / kick
// backoff streak documented on RetryState would reset on every attempt.
// Safe for concurrent use.
type RetryRegistry struct {
	mu     sync.Mutex
	states map[string]*RetryState
}

// NewRetryRegistry returns an empty RetryRegistry.
func NewRetryRegistry() *RetryRegistry {
	return &RetryRegistry{states: make(map[string]*RetryState)}
}

// Get returns the RetryState for installationID, creating one with
// NewRetryState on first use.
func (r *RetryRegistry) Get(installationID string) *RetryState {
	r.mu.Lock()
	defer r.mu.Unlock()
	if rs, ok := r.states[installationID]; ok {
		return rs
	}
	rs := NewRetryState()
	r.states[installationID] = rs
	return rs
}

// SleepCtx sleeps for d or returns early when ctx is cancelled. It
// returns ctx.Err() if ctx expired, else nil.
func SleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
