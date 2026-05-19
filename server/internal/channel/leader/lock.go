// Package leader provides single-writer coordination across multi-process
// deployments using PostgreSQL session-level advisory locks.
//
// The motivating use case (DESIGN §6 risk 2) is the Feishu WS adapter:
// the SDK opens a long-lived WebSocket and Feishu only delivers each event
// to one subscriber. Running the adapter on every replica would either
// double-process events or have most replicas rejected by the platform.
// Rather than introduce a new coordination service (etcd / ZooKeeper /
// Redis Redlock), we lean on a Postgres primitive every deployment
// already runs:
//
//   - pg_try_advisory_lock(id) is non-blocking, session-scoped, and
//     automatically released when the holding connection drops.
//   - Crash-handover therefore costs zero application code: kill -9 on
//     the leader → TCP reset → backend exits → lock released → the next
//     follower's ping-tick finds it free.
//   - There is no leader lease to renew, no clock skew window, no
//     fencing token problem at the storage layer because we never use
//     the lock to gate writes to a shared resource — only to gate
//     starting a *single* WS subscription.
//
// The contract is intentionally narrow: callers register OnAcquire /
// OnRelease callbacks (typically channel.Adapter.Connect / Disconnect),
// hand the Elector a context, and call Run. Run blocks until the context
// is cancelled, draining through OnRelease before returning.
package leader

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ChannelFeishuLockID is the advisory-lock id the Feishu WS adapter uses
// across all replicas. The constant value spells "feishu_c" in ASCII —
// (f,e,i,s,h,u,_,c) packed into an int64 — so a DBA grepping
// pg_locks can recognise it on sight without a code lookup. Keep this in
// sync with the wiring site in cmd/server/main.go; never recompute it
// at runtime, never thread it through configuration.
const ChannelFeishuLockID int64 = 0x6665697368755f63

// Callback is the signature OnAcquire / OnRelease share. It mirrors
// port.Channel.Connect / Disconnect deliberately so an adapter can be
// wired in directly without a wrapper closure.
type Callback func(ctx context.Context) error

// Elector arbitrates leadership for a single advisory-lock id. It is
// reusable across Run calls only after the previous Run has returned —
// i.e. one Elector represents one logical leader slot, but the same
// process can re-enter Run after a context cancellation.
//
// Concurrency: methods may be called from any goroutine. OnAcquire /
// OnRelease must be set before Run is invoked; mutating callbacks while
// Run is active is undefined.
type Elector struct {
	pool         *pgxpool.Pool
	lockID       int64
	pingInterval time.Duration

	mu        sync.Mutex
	onAcquire Callback
	onRelease Callback

	// leader is updated atomically so IsLeader can be polled without
	// holding mu. It transitions false → true under the held lock and
	// true → false during release; the elector's own Run goroutine is
	// the only writer.
	leader atomic.Bool
}

// NewElector builds an Elector. pingInterval bounds the worst-case
// follower latency: when a leader dies, the follower will discover the
// lock free at most pingInterval after Postgres finishes releasing it
// (typically <50ms). 5 seconds is the production default and the upper
// bound that satisfies TC-leader-2.
func NewElector(pool *pgxpool.Pool, lockID int64, pingInterval time.Duration) *Elector {
	if pool == nil {
		// Fail-fast: a nil pool would only surface as a NPE inside the
		// Run goroutine where the panic is awkward to recover. The
		// wiring site is the right place to learn that the pool isn't
		// ready yet.
		panic("leader.NewElector: pool must not be nil")
	}
	if pingInterval <= 0 {
		panic("leader.NewElector: pingInterval must be > 0")
	}
	return &Elector{
		pool:         pool,
		lockID:       lockID,
		pingInterval: pingInterval,
	}
}

// OnAcquire registers the callback fired when this Elector becomes the
// leader. The callback runs on the same goroutine as Run, so a long /
// blocking callback delays nothing observable to outside watchers
// (IsLeader is already true by the time it fires) but does delay the
// Run goroutine's progress. If the callback wants to start its own
// goroutines, it must do so explicitly. A non-nil error from the
// callback is surfaced via Run's return value AFTER the lock is
// released; it does NOT cause the leader to release early — see
// holdLeadership for the rationale (failure thrash vs. localising the
// alarm).
func (e *Elector) OnAcquire(fn Callback) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onAcquire = fn
}

// OnRelease registers the callback fired when this Elector relinquishes
// leadership (context cancelled). It is called BEFORE pg_advisory_unlock
// so the adapter can drain in-flight work or send a "going down"
// message while the lock still names this process as the holder. The
// callback receives a fresh context with a 5-second deadline — Run's
// outer ctx is by definition cancelled by the time we get here, so
// reusing it would abort any I/O the callback attempts.
func (e *Elector) OnRelease(fn Callback) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onRelease = fn
}

// IsLeader reports whether this Elector currently holds the lock. It is
// safe to call from any goroutine. Useful for liveness probes and tests
// that need to target the lock holder rather than guess.
func (e *Elector) IsLeader() bool {
	return e.leader.Load()
}

// Run blocks until ctx is cancelled. It alternates between two states:
//
//  1. Follower: try pg_try_advisory_lock; on success, transition to
//     leader. On failure, sleep pingInterval and try again.
//  2. Leader: hold the connection, fire OnAcquire, wait for
//     ctx.Done(); on exit fire OnRelease, run pg_advisory_unlock, and
//     return the connection to the pool.
//
// Run returns nil for a clean context-cancel shutdown. Other errors
// (callback returns, unexpected pool errors) are wrapped and returned
// after the lock has been released so the caller is never told "you
// failed" while still holding leadership.
func (e *Elector) Run(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}

		acquired, conn, err := e.tryAcquire(ctx)
		if err != nil {
			// Pool acquisition or query error. Log via the returned
			// error chain: the caller can decide whether to keep
			// retrying or surface it. We intentionally do not retry
			// here because a misconfigured pool would otherwise
			// busy-loop; callers who want resilience can wrap Run.
			return fmt.Errorf("leader: try-acquire: %w", err)
		}

		if !acquired {
			// Lock held elsewhere. Sleep a tick and retry.
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(e.pingInterval):
			}
			continue
		}

		// We are the leader. Hold the connection until ctx is cancelled.
		runErr := e.holdLeadership(ctx, conn)
		// holdLeadership always releases the lock + returns the conn.
		if runErr != nil {
			return runErr
		}
		// Clean shutdown — ctx is cancelled, no further loop iterations.
		return nil
	}
}

// tryAcquire attempts a single pg_try_advisory_lock. On success it
// returns the held connection so the caller can pin it for the lifetime
// of leadership; on failure it releases the connection back to the pool.
func (e *Elector) tryAcquire(ctx context.Context) (bool, *pgxpool.Conn, error) {
	conn, err := e.pool.Acquire(ctx)
	if err != nil {
		return false, nil, err
	}

	var got bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", e.lockID).Scan(&got); err != nil {
		conn.Release()
		return false, nil, err
	}

	if !got {
		conn.Release()
		return false, nil, nil
	}
	return true, conn, nil
}

// holdLeadership owns the connection for the rest of the leadership
// term. The release path is the most subtle part of the package — see
// inline comments.
func (e *Elector) holdLeadership(ctx context.Context, conn *pgxpool.Conn) error {
	// Order: lock held (caller's invariant) → flag flipped → callback runs.
	// The flag is set BEFORE OnAcquire so a watcher polling IsLeader
	// sees true the instant Postgres handed us the lock, matching the
	// godoc contract ("holds the lock"). A racer that polls between
	// the flag flip and OnAcquire's first side effect sees the lock
	// held without the side effect yet — that is intentional, since
	// "I have the lock but my Connect is mid-flight" is the honest
	// state of the world for those few microseconds.
	e.leader.Store(true)

	e.mu.Lock()
	onAcquire := e.onAcquire
	e.mu.Unlock()

	var acquireErr error
	if onAcquire != nil {
		acquireErr = onAcquire(ctx)
	}

	// Even if OnAcquire failed, we still proceed to the wait loop. The
	// rationale: releasing the lock now would just mean another
	// replica picks it up and probably fails for the same reason
	// (e.g. Feishu credentials misconfigured), causing a thrash where
	// every replica takes a turn failing. Letting this replica hold
	// the lock surfaces the problem in one place and makes it visible
	// in monitoring as "no replica is sending Feishu messages".
	// If the failure is transient, the application-level callback can
	// retry internally; if structural, an operator will see the alert
	// and restart with fixed config.

	// Wait for context cancellation. We could also poll the connection
	// here to detect a backend-side death (e.g. DBA terminated us); in
	// that case OnRelease + the unlock query will both fail, but the
	// pool's auto-reconnection of subsequent acquires keeps the
	// follower election healthy. Adding active conn-poll polling
	// would only matter if the connection silently goes half-open,
	// which TCP keepalives on the pool already cover.
	<-ctx.Done()

	// Release path. The order is:
	//   (1) flip leader flag false — IsLeader observers see the
	//       transition before any side effects rewind.
	//   (2) call OnRelease while still holding the lock — gives the
	//       adapter a chance to drain or send a "going down" message
	//       under the implicit "I am the leader" identity.
	//   (3) pg_advisory_unlock — tell Postgres we are done. Use a
	//       fresh background context so a cancelled ctx (the common
	//       case) does not abort the unlock query.
	//   (4) Release the conn to the pool.
	//
	// If the unlock query fails (connection already torn down by the
	// server, network blip), pg_locks will still clean up when the
	// backend exits. We return the error so callers can log it but
	// the practical impact is bounded by Postgres's own session
	// cleanup.
	e.leader.Store(false)

	e.mu.Lock()
	onRelease := e.onRelease
	e.mu.Unlock()

	var releaseErr error
	if onRelease != nil {
		// Use a fresh, short-deadline context — the elector's outer
		// ctx is by definition cancelled here, but the callback may
		// need to do brief I/O (close a WS, flush a buffer).
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		releaseErr = onRelease(releaseCtx)
		cancel()
	}

	unlockCtx, cancelUnlock := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelUnlock()
	_, unlockErr := conn.Exec(unlockCtx, "SELECT pg_advisory_unlock($1)", e.lockID)
	conn.Release()

	// Aggregate every non-nil error from the acquire/release lifecycle.
	// errors.Join with a single non-nil arg returns that arg; with all
	// nils it returns nil; with several it returns a wrapper whose
	// Unwrap() []error preserves each one for downstream errors.Is /
	// errors.As. This avoids the previous nested switch's tendency to
	// silently drop releaseErr when acquireErr was also set.
	var (
		wrapAcquire error
		wrapRelease error
		wrapUnlock  error
	)
	if acquireErr != nil {
		wrapAcquire = fmt.Errorf("leader: on-acquire: %w", acquireErr)
	}
	if releaseErr != nil {
		wrapRelease = fmt.Errorf("leader: on-release: %w", releaseErr)
	}
	if unlockErr != nil {
		wrapUnlock = fmt.Errorf("leader: pg_advisory_unlock: %w", unlockErr)
	}
	return errors.Join(wrapAcquire, wrapRelease, wrapUnlock)
}
