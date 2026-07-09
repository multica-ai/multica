package auth

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// AuthCacheTTL bounds how long a token-hash lookup stays cached before
// the auth middleware goes back to Postgres. Shared by PATCache and
// DaemonTokenCache so both kinds of token follow the same revocation
// latency contract. Short enough that revocation lag from a missed
// invalidation is bounded; long enough that a high-frequency client
// (CLI, daemon) collapses from one DB round-trip per request to one
// per TTL window.
const AuthCacheTTL = 10 * time.Minute

// patCachePrefix namespaces auth-cache keys away from the realtime relay
// (ws:*) and local-skill (mul:local_skill:*) keys.
const patCachePrefix = "mul:auth:pat:"

// PATCache caches resolved PAT lookups in Redis. A nil *PATCache is safe
// to use — every method becomes a no-op or reports a cache miss, and the
// auth middleware degrades to direct DB lookups.
type PATCache struct {
	rdb *redis.Client
}

// NewPATCache returns a cache backed by rdb. Pass nil to disable caching;
// the returned *PATCache is safe to call but never hits Redis.
func NewPATCache(rdb *redis.Client) *PATCache {
	if rdb == nil {
		return nil
	}
	return &PATCache{rdb: rdb}
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

// Set populates the cache with the given TTL. Callers MUST pass a TTL no
// longer than the token's remaining lifetime — otherwise an entry could
// outlive the PAT's expires_at and let an expired token pass auth on
// cache hit. Use TTLForExpiry to compute it from a token's expires_at.
//
// Errors are logged and swallowed — a cache write failure is not a
// request failure.
func (c *PATCache) Set(ctx context.Context, hash, userID string, ttl time.Duration) {
	if c == nil || ttl <= 0 {
		return
	}
	if err := c.rdb.Set(ctx, patCacheKey(hash), userID, ttl).Err(); err != nil {
		slog.Warn("pat_cache: set failed", "error", err)
	}
}

// TTLForExpiry returns the cache TTL for a token given its expires_at.
//   - Zero expiresAt (token never expires) → full AuthCacheTTL.
//   - expiresAt in the future → min(AuthCacheTTL, time until expiry).
//   - expiresAt at or before now → 0 (caller should skip caching; the
//     middleware shouldn't reach here because the SELECT already
//     filters expired tokens, but a TOCTOU between SELECT and Set is
//     possible).
//
// Pass time.Time{} when the token has no expiry (pgtype.Timestamptz with
// Valid=false maps to a zero Time).
func TTLForExpiry(now, expiresAt time.Time) time.Duration {
	if expiresAt.IsZero() {
		return AuthCacheTTL
	}
	remaining := expiresAt.Sub(now)
	if remaining <= 0 {
		return 0
	}
	if remaining < AuthCacheTTL {
		return remaining
	}
	return AuthCacheTTL
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
