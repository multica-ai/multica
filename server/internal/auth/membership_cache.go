package auth

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

const membershipCachePrefix = "mul:auth:member:"

// MembershipCacheTTL bounds how long a workspace membership lookup stays
// cached before the handler goes back to Postgres. Short enough that a
// removed member loses access within minutes; long enough that a
// high-frequency caller (daemon heartbeat every ~15s) collapses from one
// DB round-trip per request to one per TTL window.
const MembershipCacheTTL = 5 * time.Minute

// MembershipCache caches workspace membership existence checks in Redis.
// It tracks ONLY whether a user is a member of a workspace — it does NOT
// store role information. Authorization decisions that depend on role
// (requireWorkspaceRole, RequireWorkspaceRoleFromURL) MUST always query
// the database directly.
//
// Revocation latency: a removed member may retain cached access for up to
// MembershipCacheTTL (5 min). Combined with PATCache (10 min), the
// worst-case revocation delay is max(10m, 5m) = 10 min — consistent with
// the original PATCache design decision.
//
// A nil *MembershipCache is safe to use — every method becomes a no-op or
// reports a cache miss, and the caller degrades to direct DB lookups.
type MembershipCache struct {
	rdb *redis.Client
}

// NewMembershipCache returns a cache backed by rdb. Pass nil to disable
// caching; the returned *MembershipCache is safe to call but never hits
// Redis.
func NewMembershipCache(rdb *redis.Client) *MembershipCache {
	if rdb == nil {
		return nil
	}
	return &MembershipCache{rdb: rdb}
}

func membershipKey(userID, workspaceID string) string {
	return membershipCachePrefix + userID + ":" + workspaceID
}

// Get returns whether the user is a cached member of the workspace.
// Returns false on miss or any Redis error.
func (c *MembershipCache) Get(ctx context.Context, userID, workspaceID string) (ok bool) {
	if c == nil {
		return false
	}
	_, err := c.rdb.Get(ctx, membershipKey(userID, workspaceID)).Result()
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			slog.Warn("membership_cache: get failed; falling back to DB", "error", err)
		}
		return false
	}
	return true
}

// Set caches the existence of membership for the given user+workspace pair.
func (c *MembershipCache) Set(ctx context.Context, userID, workspaceID string) {
	if c == nil {
		return
	}
	if err := c.rdb.Set(ctx, membershipKey(userID, workspaceID), "1", MembershipCacheTTL).Err(); err != nil {
		slog.Warn("membership_cache: set failed", "error", err)
	}
}

// Invalidate removes the cached entry for a specific user+workspace.
func (c *MembershipCache) Invalidate(ctx context.Context, userID, workspaceID string) {
	if c == nil {
		return
	}
	if err := c.rdb.Del(ctx, membershipKey(userID, workspaceID)).Err(); err != nil {
		slog.Warn("membership_cache: invalidate failed", "error", err)
	}
}
