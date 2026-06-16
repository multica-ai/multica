// CEREBRO-PATCH(daemon-version-probe-cache): cerebro-only version-probe
// memoisation + timeout + concurrency cap. Net-new fork file in the
// upstream-zone path (TECH-3647).
//
// The daemon detects each configured agent's CLI version by shelling out to
// `<cli> --version`. That probe runs in two hot paths: once per workspace at
// runtime registration (registerRuntimesForWorkspace) and once per heartbeat
// tick per runtime (refreshAgentVersions). With many workspaces registered on
// one host the per-tick probes fan out across goroutines and run concurrently.
//
// On sara.local the hermes agent's `--version` blocked ~14s (SQLite state.db
// lock + an embedded update-check). The slow probes stacked across runtimes
// and pinned the host at load ~24, starving the staging deploy. A host-local
// shim that short-circuits `--version` was the emergency fix; this cache is the
// durable fix that lets the shim be removed.
//
// Three independent mitigations, each sufficient on its own to break the
// storm, applied together for defence in depth:
//
//   - Cache: a successful probe is memoised per (provider, exec path) with a
//     TTL, so the steady state is a map lookup instead of a process spawn.
//   - Timeout: every probe runs under a short context deadline, so a single
//     hung CLI can never block a heartbeat tick for more than versionProbeTimeout.
//   - Concurrency cap: a daemon-wide semaphore serialises the actual probes, so
//     even a burst of cache misses across runtimes cannot stack and pin the host.
//
// Degradation is graceful: a probe that times out or errors serves the last
// known-good version (if any) rather than wiping the version off the heartbeat.
package daemon

import (
	"context"
	"sync"
	"time"
)

const (
	// versionProbeTTL is how long a successful probe result is reused before a
	// re-probe. CLI upgrades are rare and a few minutes of staleness on the
	// reported version is harmless; the win is collapsing thousands of process
	// spawns to a handful per host. Within the 5-15 min band the issue asked for.
	versionProbeTTL = 10 * time.Minute
	// versionProbeTimeout caps a single `--version` exec. The deadline exists so
	// a hung probe (the pathological ~14s hermes case under SQLite contention)
	// can never block a heartbeat tick. It is deliberately generous: a healthy
	// hermes `--version` measured ~2.8s quiet on sara.local (Python startup +
	// state.db read), and mild contention pushes that higher, so a tight 2-3s
	// bound would time out a legitimate probe — freezing the reported version on
	// its first value (store() never runs) and, worse, failing the cold-cache
	// probe at registration (an errored probe skips registering the runtime).
	// 8s clears the real worst case with headroom while still being well under
	// both the 14s pile-up and the 15s heartbeat interval. On timeout the last
	// cached value is served, so the cap is a backstop, not the primary defence.
	versionProbeTimeout = 8 * time.Second
	// versionProbeConcurrency is the daemon-wide cap on simultaneous probes.
	// One means probes never stack — the property whose absence caused the load
	// storm. Because results are cached for versionProbeTTL the serialisation is
	// effectively free in steady state.
	versionProbeConcurrency = 1
)

// versionProbeEntry is one memoised probe result.
type versionProbeEntry struct {
	version  string
	detected time.Time
}

// versionProbeCache memoises agent version probes across the registration and
// heartbeat paths, bounds each probe with a timeout, and caps how many probes
// run at once. Safe for concurrent use from every per-runtime heartbeat
// goroutine.
type versionProbeCache struct {
	mu      sync.Mutex
	entries map[string]versionProbeEntry // (provider+path) -> last good probe
	ttl     time.Duration
	timeout time.Duration
	sem     chan struct{}    // buffered to versionProbeConcurrency
	now     func() time.Time // seam for tests; defaults to time.Now
}

func newVersionProbeCache() *versionProbeCache {
	return &versionProbeCache{
		entries: make(map[string]versionProbeEntry),
		ttl:     versionProbeTTL,
		timeout: versionProbeTimeout,
		sem:     make(chan struct{}, versionProbeConcurrency),
		now:     time.Now,
	}
}

func (c *versionProbeCache) lookup(key string) (versionProbeEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	return e, ok
}

func (c *versionProbeCache) fresh(e versionProbeEntry) bool {
	return c.now().Sub(e.detected) < c.ttl
}

func (c *versionProbeCache) store(key, version string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = versionProbeEntry{version: version, detected: c.now()}
}

// detectAgentVersionCached returns the agent CLI version for the given provider,
// reusing a cached result when fresh and otherwise running one bounded,
// rate-limited probe. It routes through the detectAgentVersion seam so tests can
// stub the real exec. When the cache is nil (defensive) it falls back to a plain
// probe so behaviour degrades to upstream rather than panicking.
func (d *Daemon) detectAgentVersionCached(ctx context.Context, provider, execPath string) (string, error) {
	c := d.versionProbes
	if c == nil {
		return detectAgentVersion(ctx, execPath)
	}
	key := provider + "\x00" + execPath

	// Fast path: a fresh cached value needs no probe and no slot.
	if e, ok := c.lookup(key); ok && c.fresh(e) {
		return e.version, nil
	}

	// Concurrency cap: never let version probes stack and pin the host. If the
	// context is cancelled before a slot frees, serve any cached value rather
	// than blocking the heartbeat that called us.
	select {
	case c.sem <- struct{}{}:
		defer func() { <-c.sem }()
	case <-ctx.Done():
		if e, ok := c.lookup(key); ok {
			return e.version, nil
		}
		return "", ctx.Err()
	}

	// Re-check after acquiring the slot: a concurrent probe for the same key may
	// have refreshed the entry while we waited, in which case we skip the exec.
	if e, ok := c.lookup(key); ok && c.fresh(e) {
		return e.version, nil
	}

	probeCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	v, err := detectAgentVersion(probeCtx, execPath)
	if err != nil {
		// Degrade gracefully: a transient timeout or error must not wipe a
		// previously good version off the heartbeat. Serve the last value if we
		// have one; only surface the error on a cold cache.
		if e, ok := c.lookup(key); ok {
			return e.version, nil
		}
		return "", err
	}
	c.store(key, v)
	return v, nil
}
