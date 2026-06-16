// CEREBRO-PATCH(daemon-version-probe-cache): TECH-3647 tests for the cerebro
// version-probe cache+timeout+cap. Net-new fork file in the upstream-zone path.
package daemon

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTestVersionProbeDaemon builds the minimal Daemon needed to exercise
// detectAgentVersionCached, with a cache whose clock is driven by the returned
// pointer so TTL behaviour is deterministic.
func newTestVersionProbeDaemon(ttl, timeout time.Duration, concurrency int) (*Daemon, *atomic.Int64) {
	var nowNanos atomic.Int64
	nowNanos.Store(time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC).UnixNano())
	c := &versionProbeCache{
		entries: make(map[string]versionProbeEntry),
		ttl:     ttl,
		timeout: timeout,
		sem:     make(chan struct{}, concurrency),
		now:     func() time.Time { return time.Unix(0, nowNanos.Load()) },
	}
	return &Daemon{versionProbes: c}, &nowNanos
}

func TestVersionProbeCache_MemoisesWithinTTL(t *testing.T) {
	cleanup := stubAgentVersion(t)
	defer cleanup()

	var calls atomic.Int64
	detectAgentVersion = func(_ context.Context, _ string) (string, error) {
		calls.Add(1)
		return "1.2.3", nil
	}

	d, _ := newTestVersionProbeDaemon(10*time.Minute, time.Second, 1)
	for i := 0; i < 5; i++ {
		v, err := d.detectAgentVersionCached(context.Background(), "hermes", "/bin/hermes")
		if err != nil || v != "1.2.3" {
			t.Fatalf("call %d: got (%q, %v), want (\"1.2.3\", nil)", i, v, err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("exec count = %d, want 1 (cache should collapse repeats)", got)
	}
}

func TestVersionProbeCache_ReprobesAfterTTL(t *testing.T) {
	cleanup := stubAgentVersion(t)
	defer cleanup()

	var calls atomic.Int64
	detectAgentVersion = func(_ context.Context, _ string) (string, error) {
		calls.Add(1)
		return "1.2.3", nil
	}

	d, nowNanos := newTestVersionProbeDaemon(10*time.Minute, time.Second, 1)
	if _, err := d.detectAgentVersionCached(context.Background(), "hermes", "/bin/hermes"); err != nil {
		t.Fatalf("first probe: %v", err)
	}
	// Advance the clock past the TTL and probe again.
	nowNanos.Add(int64(11 * time.Minute))
	if _, err := d.detectAgentVersionCached(context.Background(), "hermes", "/bin/hermes"); err != nil {
		t.Fatalf("post-TTL probe: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("exec count = %d, want 2 (TTL expiry should force a re-probe)", got)
	}
}

func TestVersionProbeCache_ConcurrencyCapPreventsStacking(t *testing.T) {
	cleanup := stubAgentVersion(t)
	defer cleanup()

	var inFlight atomic.Int64
	var maxInFlight atomic.Int64
	release := make(chan struct{})
	detectAgentVersion = func(_ context.Context, _ string) (string, error) {
		cur := inFlight.Add(1)
		for {
			old := maxInFlight.Load()
			if cur <= old || maxInFlight.CompareAndSwap(old, cur) {
				break
			}
		}
		<-release // hold the slot until the test lets go
		inFlight.Add(-1)
		return "1.2.3", nil
	}

	// concurrency cap of 1 — no two probes may exec at once even on distinct keys.
	d, _ := newTestVersionProbeDaemon(10*time.Minute, time.Minute, 1)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		path := "/bin/agent" + string(rune('a'+i)) // distinct key => guaranteed cache miss
		go func(p string) {
			defer wg.Done()
			_, _ = d.detectAgentVersionCached(context.Background(), "p", p)
		}(path)
	}
	// Give the goroutines time to pile up on the semaphore, then drain them.
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := maxInFlight.Load(); got > 1 {
		t.Fatalf("max concurrent probes = %d, want <= 1 (cap must prevent stacking)", got)
	}
}

func TestVersionProbeCache_TimeoutServesStaleValue(t *testing.T) {
	cleanup := stubAgentVersion(t)
	defer cleanup()

	// First probe succeeds and seeds the cache.
	detectAgentVersion = func(_ context.Context, _ string) (string, error) {
		return "1.2.3", nil
	}
	d, nowNanos := newTestVersionProbeDaemon(10*time.Minute, 20*time.Millisecond, 1)
	if v, err := d.detectAgentVersionCached(context.Background(), "hermes", "/bin/hermes"); err != nil || v != "1.2.3" {
		t.Fatalf("seed probe: got (%q, %v)", v, err)
	}

	// Now make the probe hang past the timeout and expire the cached entry.
	detectAgentVersion = func(ctx context.Context, _ string) (string, error) {
		<-ctx.Done() // respect the probe deadline
		return "", ctx.Err()
	}
	nowNanos.Add(int64(11 * time.Minute))

	v, err := d.detectAgentVersionCached(context.Background(), "hermes", "/bin/hermes")
	if err != nil {
		t.Fatalf("stale-serve probe returned error %v, want nil", err)
	}
	if v != "1.2.3" {
		t.Fatalf("stale-serve probe = %q, want last good value \"1.2.3\"", v)
	}
}

func TestVersionProbeCache_ColdTimeoutReturnsError(t *testing.T) {
	cleanup := stubAgentVersion(t)
	defer cleanup()

	detectAgentVersion = func(ctx context.Context, _ string) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}
	d, _ := newTestVersionProbeDaemon(10*time.Minute, 20*time.Millisecond, 1)

	_, err := d.detectAgentVersionCached(context.Background(), "hermes", "/bin/hermes")
	if err == nil {
		t.Fatalf("cold-cache timeout returned nil error, want a deadline error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cold-cache timeout error = %v, want context.DeadlineExceeded", err)
	}
}
