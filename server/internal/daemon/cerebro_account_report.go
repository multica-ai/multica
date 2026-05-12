// CEREBRO-PATCH(daemon-cerebro-account-report): cerebro-only identity-probe
// cache for the JEH-997 account flow. Net-new fork file in the upstream-zone
// path.
//
// Account info now rides on the heartbeat itself (server side: HTTP
// DaemonHeartbeat + WS HandleDaemonWSHeartbeat both invoke
// h.recordHeartbeatAccount on every beat). The daemon doesn't POST account
// data on a separate path anymore — it just needs to know what to put in
// the Account field of each outgoing heartbeat payload.
//
// heartbeatAccountFor returns that payload. It is cheap to call on every
// beat: detection is one file read + one JSON parse, results are cached
// per-runtime so the steady-state cost is a map lookup.
package daemon

import (
	"sync"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

// accountIdentityCache memoises the per-runtime identity probe across
// heartbeat ticks. It exists because both the HTTP and WS heartbeat paths
// call heartbeatAccountFor on every beat, and re-reading the auth-state
// file 60 times a minute per runtime would be wasteful. The cache is
// keyed on (provider, runtimeID); the daemon's runtimeIndex carries the
// provider, so the lookup composes cleanly.
//
// Cache invalidation is intentionally absent: a logged-out user produces
// a fresh detection (empty identity) which we still cache, and the
// heartbeat is content with reporting nothing. When the user logs in
// again, the file appears, but the cache is now sticky on "no identity".
// We sidestep that with a low-frequency forced re-probe (rebuildIdentity
// is wired into the heartbeat tick in daemon.go); the cache fills the gap
// between forced probes only.
type accountIdentityCache struct {
	mu      sync.Mutex
	entries map[string]protocol.DaemonHeartbeatAccount // runtime_id -> last detected
	probed  map[string]bool                            // runtime_id -> probe has run at least once
}

func newAccountIdentityCache() *accountIdentityCache {
	return &accountIdentityCache{
		entries: make(map[string]protocol.DaemonHeartbeatAccount),
		probed:  make(map[string]bool),
	}
}

func (c *accountIdentityCache) get(runtimeID string) (protocol.DaemonHeartbeatAccount, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	a, ok := c.entries[runtimeID]
	return a, ok
}

func (c *accountIdentityCache) set(runtimeID string, a protocol.DaemonHeartbeatAccount) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[runtimeID] = a
	c.probed[runtimeID] = true
}

func (c *accountIdentityCache) hasProbed(runtimeID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.probed[runtimeID]
}

// heartbeatAccountFor returns the Account payload to embed in this beat,
// or nil when no identity is available. Always safe to call from any
// goroutine.
//
// First call per runtime forces a probe; subsequent calls reuse the cache.
// The reprobe path is owned by refreshHeartbeatAccount, which the heartbeat
// loop drives at a slow cadence (once per N beats) so a fresh login is
// picked up without re-reading the file on every beat.
func (d *Daemon) heartbeatAccountFor(runtimeID string) *protocol.DaemonHeartbeatAccount {
	if d.accountIdentities == nil {
		return nil
	}
	if !d.accountIdentities.hasProbed(runtimeID) {
		d.refreshHeartbeatAccount(runtimeID)
	}
	a, ok := d.accountIdentities.get(runtimeID)
	if !ok || a.Provider == "" || a.LoginIdentity == "" {
		return nil
	}
	out := a // copy out so callers can't mutate the cache entry
	return &out
}

// refreshHeartbeatAccount re-runs the identity probe for the given runtime
// and updates the cache. Called once on first heartbeat (lazy) and again
// on every heartbeat tick (cheap re-probe — one fstat + one JSON parse).
// Eager re-probe is what lets a login change propagate within one beat
// instead of one daemon restart.
func (d *Daemon) refreshHeartbeatAccount(runtimeID string) {
	rt := d.findRuntime(runtimeID)
	if rt == nil {
		return
	}
	identity, source, err := DetectAccountIdentity(rt.Provider)
	if err != nil {
		d.logger.Warn("account identity probe failed",
			"runtime_id", runtimeID, "provider", rt.Provider,
			"source", source, "error", err)
		// Stamp the cache as probed so we don't retry the failing path
		// every heartbeat — the next forced refresh will try again.
		d.accountIdentities.set(runtimeID, protocol.DaemonHeartbeatAccount{Provider: rt.Provider})
		return
	}
	d.accountIdentities.set(runtimeID, protocol.DaemonHeartbeatAccount{
		Provider:      rt.Provider,
		LoginIdentity: identity,
	})
}
