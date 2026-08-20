package daemon

import (
	"context"
	"sync"
)

const defaultMaxConcurrentSkillDownloads = 4

// skillBundleDownloadCoordinator bounds bundle downloads across the whole
// daemon and coalesces concurrent cache misses for the same immutable ref. A
// flight owns its context: individual task cancellation only removes that
// waiter, while the shared operation is cancelled once nobody is waiting.
type skillBundleDownloadCoordinator struct {
	slots chan struct{}

	mu      sync.Mutex
	flights map[string]*skillBundleDownloadFlight
}

type skillBundleDownloadFlight struct {
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	waiters  int
	finished bool
	bundle   SkillData
	err      error
}

func newSkillBundleDownloadCoordinator(limit int) *skillBundleDownloadCoordinator {
	if limit <= 0 {
		limit = defaultMaxConcurrentSkillDownloads
	}
	return &skillBundleDownloadCoordinator{
		slots:   make(chan struct{}, limit),
		flights: make(map[string]*skillBundleDownloadFlight),
	}
}

func (c *skillBundleDownloadCoordinator) do(
	ctx context.Context,
	key string,
	fn func(context.Context) (SkillData, error),
) (SkillData, error) {
	if err := ctx.Err(); err != nil {
		return SkillData{}, context.Cause(ctx)
	}

	var flight *skillBundleDownloadFlight
	for {
		c.mu.Lock()
		flight = c.flights[key]
		if flight == nil {
			flightCtx, cancel := context.WithCancel(context.Background())
			flight = &skillBundleDownloadFlight{
				ctx:     flightCtx,
				cancel:  cancel,
				done:    make(chan struct{}),
				waiters: 1,
			}
			c.flights[key] = flight
			go c.run(key, flight, fn)
			c.mu.Unlock()
			break
		}
		if flight.waiters > 0 {
			flight.waiters++
			c.mu.Unlock()
			break
		}

		// The last waiter has cancelled this flight, but its HTTP transport may
		// still be unwinding. Wait for it to leave the map before starting a new
		// request so an immediate task retry cannot overlap the abandoned one.
		done := flight.done
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return SkillData{}, context.Cause(ctx)
		case <-done:
		}
	}

	select {
	case <-ctx.Done():
		c.leave(flight)
		return SkillData{}, context.Cause(ctx)
	case <-flight.done:
		c.leave(flight)
		return flight.bundle, flight.err
	}
}

func (c *skillBundleDownloadCoordinator) run(
	key string,
	flight *skillBundleDownloadFlight,
	fn func(context.Context) (SkillData, error),
) {
	bundle, err := fn(flight.ctx)

	c.mu.Lock()
	flight.bundle = bundle
	flight.err = err
	flight.finished = true
	if c.flights[key] == flight {
		delete(c.flights, key)
	}
	close(flight.done)
	flight.cancel()
	c.mu.Unlock()
}

func (c *skillBundleDownloadCoordinator) leave(flight *skillBundleDownloadFlight) {
	c.mu.Lock()
	defer c.mu.Unlock()

	flight.waiters--
	if flight.waiters == 0 && !flight.finished {
		flight.cancel()
	}
}

func (c *skillBundleDownloadCoordinator) acquire(ctx context.Context) error {
	select {
	case c.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

func (c *skillBundleDownloadCoordinator) release() {
	<-c.slots
}
