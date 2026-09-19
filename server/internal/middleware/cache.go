package middleware

import (
	"sync"
	"time"
)

// slugCacheEntry holds a cached workspace UUID and its expiration time.
type slugCacheEntry struct {
	uuid      string
	expiresAt time.Time
}

// SlugCache is a thread-safe in-memory cache for workspace slug → UUID lookups.
// Since workspace slugs are immutable, entries can be cached safely with a TTL.
// The cache automatically evicts expired entries on access and during periodic cleanup.
type SlugCache struct {
	mu      sync.RWMutex
	entries map[string]slugCacheEntry
	ttl     time.Duration
	stop    chan struct{}
}

// NewSlugCache creates a new SlugCache with the given TTL and starts a
// background cleanup goroutine that runs every ttl/2 interval.
// Call Stop() when the cache is no longer needed to release resources.
func NewSlugCache(ttl time.Duration) *SlugCache {
	c := &SlugCache{
		entries: make(map[string]slugCacheEntry),
		ttl:     ttl,
		stop:    make(chan struct{}),
	}
	go c.cleanupLoop()
	return c
}

// Get retrieves a UUID from the cache. Returns ("", false) on miss or expiry.
func (c *SlugCache) Get(slug string) (string, bool) {
	c.mu.RLock()
	entry, ok := c.entries[slug]
	c.mu.RUnlock()
	if !ok {
		return "", false
	}
	if time.Now().After(entry.expiresAt) {
		// Entry expired; lazy eviction on next write.
		return "", false
	}
	return entry.uuid, true
}

// Set stores a UUID in the cache with the configured TTL.
func (c *SlugCache) Set(slug, uuid string) {
	c.mu.Lock()
	c.entries[slug] = slugCacheEntry{
		uuid:      uuid,
		expiresAt: time.Now().Add(c.ttl),
	}
	c.mu.Unlock()
}

// Stop terminates the background cleanup goroutine.
func (c *SlugCache) Stop() {
	close(c.stop)
}

// cleanupLoop periodically removes expired entries.
func (c *SlugCache) cleanupLoop() {
	ticker := time.NewTicker(c.ttl / 2)
	defer ticker.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-ticker.C:
			c.evictExpired()
		}
	}
}

// evictExpired removes all expired entries from the cache.
func (c *SlugCache) evictExpired() {
	now := time.Now()
	c.mu.Lock()
	for slug, entry := range c.entries {
		if now.After(entry.expiresAt) {
			delete(c.entries, slug)
		}
	}
	c.mu.Unlock()
}
