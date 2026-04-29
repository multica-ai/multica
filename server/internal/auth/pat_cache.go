package auth

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// PATCacheTTL bounds how long a (token_hash → user_id) lookup stays cached
// before the auth middleware goes back to Postgres. Short enough that
// revocation lag from a missed invalidation is bounded; long enough that
// a high-frequency PAT client (CLI, daemon) collapses from one DB
// round-trip per request to roughly one per minute.
const PATCacheTTL = 60 * time.Second

// patCachePrefix namespaces auth-cache keys away from the realtime relay
// (ws:*) and local-skill (mul:local_skill:*) keys.
const patCachePrefix = "mul:auth:pat:"

// PATCache caches resolved PAT lookups in Redis. A nil *PATCache is safe
// to use — every method becomes a no-op or reports a cache miss, and the
// auth middleware degrades to direct DB lookups.
type PATCache struct {
	rdb *redis.Client
	ttl time.Duration
}

// NewPATCache returns a cache backed by rdb. Pass nil to disable caching;
// the returned *PATCache is safe to call but never hits Redis.
func NewPATCache(rdb *redis.Client) *PATCache {
	if rdb == nil {
		return nil
	}
	return &PATCache{rdb: rdb, ttl: PATCacheTTL}
}

func patCacheKey(hash string) string { return patCachePrefix + hash }

// Get returns the cached user_id for a token hash. ok=false on cache miss
// or any Redis error — a dead Redis must not take down auth.
func (c *PATCache) Get(ctx context.Context, hash string) (userID string, ok bool) {
	if c == nil {
		return "", false
	}
	v, err := c.rdb.Get(ctx, patCacheKey(hash)).Result()
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			slog.Warn("pat_cache: get failed; falling back to DB", "error", err)
		}
		return "", false
	}
	return v, true
}

// Set populates the cache with TTL = PATCacheTTL. Errors are logged and
// swallowed — a cache write failure is not a request failure.
func (c *PATCache) Set(ctx context.Context, hash, userID string) {
	if c == nil {
		return
	}
	if err := c.rdb.Set(ctx, patCacheKey(hash), userID, c.ttl).Err(); err != nil {
		slog.Warn("pat_cache: set failed", "error", err)
	}
}

// Invalidate removes the entry for hash. Called on PAT revocation so the
// revoke takes effect immediately rather than waiting for the TTL.
func (c *PATCache) Invalidate(ctx context.Context, hash string) {
	if c == nil {
		return
	}
	if err := c.rdb.Del(ctx, patCacheKey(hash)).Err(); err != nil {
		slog.Warn("pat_cache: invalidate failed; entry will expire on TTL", "error", err)
	}
}
