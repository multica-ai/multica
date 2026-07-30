package handler

import (
	"context"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Runtime model catalog cache (stale-while-revalidate)
// ---------------------------------------------------------------------------
//
// Listing a runtime's models is a round trip to the user's machine: the request
// waits for the daemon's next heartbeat, the daemon shells out to the provider
// CLI (or drives an ACP handshake) and only then reports back. Even with the
// pending-work push hint (MUL-5444) that is seconds of latency on a UI surface
// people open repeatedly while filling in one form — switch runtime, look at the
// models, switch back.
//
// The catalog itself changes only when the user upgrades a CLI, logs into a
// different account, or edits a provider config, so it is a textbook
// stale-while-revalidate candidate: answer from the last known good snapshot
// immediately, and refresh in the background so the NEXT open is also warm.
//
// The two windows do different jobs, and only one of them governs freshness:
//   - modelCatalogRevalidateAfter is the freshness knob. Serving a snapshot
//     older than this also queues a background refresh, so a CLI upgrade
//     converges after one open no matter how long the serve window is.
//   - modelCatalogServeWindow only bounds how long an UNUSED snapshot survives,
//     and how stale the answer is for someone who opens the picker exactly once
//     and never returns. It is deliberately day-scale: nothing keeps an entry
//     warm in the background, the browser's own react-query cache dies with the
//     tab, and agent CLIs are upgraded on a scale of days — so a minutes-scale
//     window made every first-open-of-the-day a cold miss (the exact multi-second
//     wait this cache exists to remove) while buying no real freshness.
//
// The window is not unbounded because a *failed* report deliberately leaves the
// snapshot in place (a transient discovery failure must not empty the picker).
// For a runtime whose discovery keeps failing — CLI uninstalled, logged out —
// expiry is the only thing that eventually retires the stale catalog.
//
// Only successful, non-empty, `supported` catalogs are cached. An empty list is
// almost always a transient discovery failure (CLI not logged in, timeout) —
// caching it would pin the picker empty (same reasoning as agent.cachedDiscovery
// in the daemon).

const (
	// modelCatalogServeWindow is how long a cached catalog may answer a
	// list-models request without waiting for the daemon. Day-scale on purpose
	// (MUL-5444): see the freshness discussion above — every served snapshot
	// past modelCatalogRevalidateAfter queues its own refresh, so this bounds
	// unused-entry lifetime and the open-once worst case, not staleness for an
	// active user.
	modelCatalogServeWindow = 24 * time.Hour
	// modelCatalogRevalidateAfter is the age past which serving from cache also
	// enqueues a background refresh. This is the knob that actually keeps the
	// catalog honest; keep it short.
	modelCatalogRevalidateAfter = 60 * time.Second
)

// ModelCatalogSnapshot is the last known good model list for one runtime.
type ModelCatalogSnapshot struct {
	RuntimeID string       `json:"runtime_id"`
	Models    []ModelEntry `json:"models"`
	Supported bool         `json:"supported"`
	StoredAt  time.Time    `json:"stored_at"`
}

// Age reports how long ago the snapshot was captured.
func (s *ModelCatalogSnapshot) Age(now time.Time) time.Duration {
	if s == nil {
		return 0
	}
	return now.Sub(s.StoredAt)
}

// ModelCatalogCache stores the last successful model catalog per runtime. Both
// methods are best-effort from the caller's perspective: a Get error means
// "answer the slow way" and a Put error means "the next open is cold". Neither
// may fail a request.
//
// Implementations must be safe for concurrent use.
type ModelCatalogCache interface {
	Get(ctx context.Context, runtimeID string) (*ModelCatalogSnapshot, error)
	Put(ctx context.Context, runtimeID string, models []ModelEntry, supported bool) error
	// Invalidate drops any snapshot for the runtime. Used when the cached
	// catalog can no longer be trusted (e.g. the runtime row was deleted).
	Invalidate(ctx context.Context, runtimeID string) error
}

// cacheableModelCatalog reports whether a completed discovery result is worth
// remembering. `supported=false` runtimes have no picker at all, and an empty
// catalog is treated as a transient failure rather than an authoritative
// "this runtime has no models".
func cacheableModelCatalog(models []ModelEntry, supported bool) bool {
	return supported && len(models) > 0
}

// cloneModelEntries deep-copies a catalog so the in-memory backend hands out
// values a caller cannot mutate into the shared cache. A shallow slice copy is
// not enough: ModelEntry carries a *ModelThinking (with its own level slice) and
// a ServiceTiers slice, all of which would still alias the cached objects. The
// Redis backend gets this for free by round-tripping through JSON, and the two
// implementations must not differ in whether the returned value is independent.
func cloneModelEntries(models []ModelEntry) []ModelEntry {
	if models == nil {
		return nil
	}
	out := make([]ModelEntry, len(models))
	for i, m := range models {
		clone := m
		if m.Thinking != nil {
			thinking := *m.Thinking
			if m.Thinking.SupportedLevels != nil {
				thinking.SupportedLevels = append([]ThinkingLevel(nil), m.Thinking.SupportedLevels...)
			}
			clone.Thinking = &thinking
		}
		if m.ServiceTiers != nil {
			clone.ServiceTiers = append([]ModelServiceTier(nil), m.ServiceTiers...)
		}
		out[i] = clone
	}
	return out
}

// InMemoryModelCatalogCache is the single-node implementation. Adequate for
// self-hosted and tests; multi-node deploys should use the Redis backend so
// every API replica shares one warm catalog.
type InMemoryModelCatalogCache struct {
	mu        sync.Mutex
	entries   map[string]ModelCatalogSnapshot
	retainFor time.Duration
}

func NewInMemoryModelCatalogCache() *InMemoryModelCatalogCache {
	return &InMemoryModelCatalogCache{
		entries:   make(map[string]ModelCatalogSnapshot),
		retainFor: modelCatalogServeWindow,
	}
}

func (c *InMemoryModelCatalogCache) Get(_ context.Context, runtimeID string) (*ModelCatalogSnapshot, error) {
	if runtimeID == "" {
		return nil, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[runtimeID]
	if !ok {
		return nil, nil
	}
	if time.Since(entry.StoredAt) > c.retainFor {
		delete(c.entries, runtimeID)
		return nil, nil
	}
	// Copy so a caller mutating the response cannot corrupt the cache.
	snapshot := entry
	snapshot.Models = cloneModelEntries(entry.Models)
	return &snapshot, nil
}

func (c *InMemoryModelCatalogCache) Put(_ context.Context, runtimeID string, models []ModelEntry, supported bool) error {
	if runtimeID == "" || !cacheableModelCatalog(models, supported) {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	// Garbage-collect expired entries so the map can't grow unbounded as
	// runtimes come and go.
	now := time.Now()
	for id, entry := range c.entries {
		if now.Sub(entry.StoredAt) > c.retainFor {
			delete(c.entries, id)
		}
	}

	c.entries[runtimeID] = ModelCatalogSnapshot{
		RuntimeID: runtimeID,
		Models:    cloneModelEntries(models),
		Supported: supported,
		StoredAt:  now,
	}
	return nil
}

func (c *InMemoryModelCatalogCache) Invalidate(_ context.Context, runtimeID string) error {
	if runtimeID == "" {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, runtimeID)
	return nil
}
