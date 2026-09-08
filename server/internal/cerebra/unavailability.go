package cerebra

import (
	"context"
	"sync"
	"time"
)

// UnavailabilityStore tracks models that have been temporarily marked
// unavailable due to quota exhaustion or rate-limit errors. The router
// calls IsAvailable() during candidate filtering (before selection).
//
// Storage: in-memory with optional DB persistence extension point.
// Default TTL: 1 hour (configurable via NewUnavailabilityStore).
type UnavailabilityStore struct {
	mu      sync.RWMutex
	entries map[unavailKey]unavailEntry
	ttl     time.Duration
}

// DefaultUnavailabilityTTL is the default duration a model stays marked unavailable.
const DefaultUnavailabilityTTL = time.Hour

type unavailKey struct {
	runtimeID string
	model     string
}

type unavailEntry struct {
	markedAt time.Time
	ttl      time.Duration
}

// NewUnavailabilityStore creates a store with the given TTL.
func NewUnavailabilityStore(ttl time.Duration) *UnavailabilityStore {
	if ttl <= 0 {
		ttl = DefaultUnavailabilityTTL
	}
	return &UnavailabilityStore{
		entries: make(map[unavailKey]unavailEntry),
		ttl:     ttl,
	}
}

// MarkUnavailable records that the given model on runtimeID is temporarily
// unavailable. Pass 0 for ttl to use the store's configured default.
func (u *UnavailabilityStore) MarkUnavailable(_ context.Context, runtimeID, model string, ttl time.Duration) {
	if ttl <= 0 {
		ttl = u.ttl
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.entries[unavailKey{runtimeID: runtimeID, model: model}] = unavailEntry{
		markedAt: time.Now(),
		ttl:      ttl,
	}
}

// IsAvailable returns true if the model is not currently marked unavailable
// (or if its TTL has expired, in which case the entry is lazily evicted).
func (u *UnavailabilityStore) IsAvailable(_ context.Context, runtimeID, model string) bool {
	u.mu.RLock()
	entry, ok := u.entries[unavailKey{runtimeID: runtimeID, model: model}]
	u.mu.RUnlock()

	if !ok {
		return true // no record → available
	}
	if time.Since(entry.markedAt) > entry.ttl {
		// TTL expired — evict lazily.
		u.mu.Lock()
		delete(u.entries, unavailKey{runtimeID: runtimeID, model: model})
		u.mu.Unlock()
		return true
	}
	return false
}

// Clear removes all unavailability records. Useful in tests.
func (u *UnavailabilityStore) Clear() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.entries = make(map[unavailKey]unavailEntry)
}
