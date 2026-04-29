package auth

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// daemonTokenCachePrefix namespaces daemon-token cache keys separately
// from PAT (mul:auth:pat:*) so the two key spaces can't collide and an
// invalidation on one kind of token doesn't accidentally hit the other.
const daemonTokenCachePrefix = "mul:auth:daemon:"

// DaemonTokenIdentity is what DaemonAuth needs from the cached lookup —
// the workspace_id and daemon_id that the middleware injects into the
// request context. We deliberately omit token_hash, expires_at, and the
// row id; cache entries should leak the minimum.
type DaemonTokenIdentity struct {
	WorkspaceID string `json:"w"`
	DaemonID    string `json:"d"`
}

// DaemonTokenCache caches resolved daemon-token (mdt_) lookups in Redis.
// A nil *DaemonTokenCache is safe to use — every method becomes a no-op
// or reports a cache miss, so single-node dev / tests with no REDIS_URL
// degrade cleanly to direct DB lookups.
type DaemonTokenCache struct {
	rdb *redis.Client
}

// NewDaemonTokenCache returns a cache backed by rdb. Pass nil to disable
// caching; the returned *DaemonTokenCache is safe to call but never hits
// Redis.
func NewDaemonTokenCache(rdb *redis.Client) *DaemonTokenCache {
	if rdb == nil {
		return nil
	}
	return &DaemonTokenCache{rdb: rdb}
}

func daemonTokenCacheKey(hash string) string { return daemonTokenCachePrefix + hash }

// Get returns the cached identity for a token hash. ok=false on cache
// miss or any Redis / decode error — a dead Redis must not take down
// auth.
func (c *DaemonTokenCache) Get(ctx context.Context, hash string) (DaemonTokenIdentity, bool) {
	if c == nil {
		return DaemonTokenIdentity{}, false
	}
	raw, err := c.rdb.Get(ctx, daemonTokenCacheKey(hash)).Bytes()
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			slog.Warn("daemon_token_cache: get failed; falling back to DB", "error", err)
		}
		return DaemonTokenIdentity{}, false
	}
	var id DaemonTokenIdentity
	if err := json.Unmarshal(raw, &id); err != nil {
		slog.Warn("daemon_token_cache: malformed entry; falling back to DB", "error", err)
		return DaemonTokenIdentity{}, false
	}
	return id, true
}

// Set populates the cache with the given TTL. Use TTLForExpiry to clamp
// the TTL to the token's remaining lifetime so a daemon token expiring
// in <AuthCacheTTL can't outlive its expires_at on a cache hit.
//
// Errors are logged and swallowed — a cache write failure is not a
// request failure.
func (c *DaemonTokenCache) Set(ctx context.Context, hash string, id DaemonTokenIdentity, ttl time.Duration) {
	if c == nil || ttl <= 0 {
		return
	}
	raw, err := json.Marshal(id)
	if err != nil {
		slog.Warn("daemon_token_cache: marshal failed", "error", err)
		return
	}
	if err := c.rdb.Set(ctx, daemonTokenCacheKey(hash), raw, ttl).Err(); err != nil {
		slog.Warn("daemon_token_cache: set failed", "error", err)
	}
}

// Invalidate removes the entry for hash. Called when a daemon token is
// deleted so the deletion takes effect immediately rather than waiting
// for the TTL.
func (c *DaemonTokenCache) Invalidate(ctx context.Context, hash string) {
	if c == nil {
		return
	}
	if err := c.rdb.Del(ctx, daemonTokenCacheKey(hash)).Err(); err != nil {
		slog.Warn("daemon_token_cache: invalidate failed; entry will expire on TTL", "error", err)
	}
}
