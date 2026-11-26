// Package lru is a concurrency-safe LRU cache with an optional TTL.
package lru

import "time"

// Reason says why an entry left the cache.
type Reason int

const (
	Capacity Reason = iota
	Expired
)

func (r Reason) String() string { panic("not implemented") }

// Cache is safe for concurrent use.
type Cache[K comparable, V any] struct{}

// New returns a cache holding at most capacity entries. A ttl of zero means
// entries never expire; otherwise an entry expires ttl after it was last Set.
func New[K comparable, V any](capacity int, ttl time.Duration, now func() time.Time) *Cache[K, V] {
	panic("not implemented")
}

// OnEvict registers a callback for capacity evictions and expirations.
// Delete and replacing a value do not call it.
func (c *Cache[K, V]) OnEvict(fn func(K, V, Reason)) { panic("not implemented") }

func (c *Cache[K, V]) Set(k K, v V) { panic("not implemented") }

func (c *Cache[K, V]) Get(k K) (V, bool) { panic("not implemented") }

func (c *Cache[K, V]) Delete(k K) bool { panic("not implemented") }

// Len returns the number of live entries.
func (c *Cache[K, V]) Len() int { panic("not implemented") }

// Keys returns live keys, most recently used first.
func (c *Cache[K, V]) Keys() []K { panic("not implemented") }
