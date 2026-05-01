package service

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// emptyClaimCacheKey holds a "no queued task" verdict tagged with the
// per-runtime version it was observed under. emptyClaimVersionKey is
// the per-runtime monotonic counter that any enqueue path bumps. The
// verdict is trusted only when its value equals the current version,
// which closes the race where a slow claim writes an empty verdict
// AFTER an enqueue has already invalidated it:
//
//   T1 claim:   v0 := GET version
//               SELECT ... -> empty
//               (slow, e.g. GC pause)
//   T2 enqueue: INSERT row
//               INCR version  (-> v1)
//               wakeup
//   T1 claim:   SET empty = v0
//   T3 claim:   v1' := GET version (== v1)
//               GET empty (== v0) -> v0 != v1, treat as miss -> SELECT
//
// Without the version tag T3 would have hit the stale empty key and
// the just-queued task would sit idle until the empty key's TTL
// expired. With it, the only window left is one extra DB SELECT per
// runtime per concurrent enqueue, never a stalled task.
const (
	emptyClaimCachePrefix   = "mul:claim:runtime:empty:"
	emptyClaimVersionPrefix = "mul:claim:runtime:version:"
)

// EmptyClaimCacheTTL bounds how long a cached "no queued task" verdict
// stays believable. Enqueue invalidates the verdict by bumping the
// per-runtime version before waking the daemon, so a longer TTL keeps
// the steady-state idle poll path off Postgres. The TTL remains the
// safety net for a missed invalidation, e.g. a transient Redis failure
// during Bump.
const EmptyClaimCacheTTL = 3 * time.Minute

// emptyClaimVersionTTL keeps the version counter alive long enough that
// a rarely-polled runtime doesn't reset to 0 between an enqueue's
// INCR and the next claim's GET (which would let a stale tagged
// empty key suddenly look valid again). Sliding TTL is renewed on
// every Bump and every Get.
const emptyClaimVersionTTL = 24 * time.Hour

// emptyClaimRedisTimeout caps every Redis call from this cache. Enqueue
// paths use a background context so the cache outlives the request,
// but a wedged Redis must not stall enqueue indefinitely — bound the
// blast radius and degrade to "no cache" instead.
const emptyClaimRedisTimeout = 250 * time.Millisecond

// EmptyClaimCache caches "this runtime currently has no queued task"
// so the daemon's poll-based claim path can short-circuit before
// hitting Postgres. Only the negative result is cached; positive
// results always re-check the DB so concurrent claimers race fairly
// in `ClaimAgentTask`'s `FOR UPDATE SKIP LOCKED`.
//
// The cache is invalidated synchronously on every enqueue (see
// TaskService.notifyTaskAvailable). A nil *EmptyClaimCache is safe to
// use — every method becomes a no-op or reports a cache miss, so
// single-node dev / tests with no REDIS_URL degrade cleanly to direct
// DB lookups.
type EmptyClaimCache struct {
	rdb *redis.Client
}

// NewEmptyClaimCache returns a cache backed by rdb. Pass nil to
// disable caching; the returned *EmptyClaimCache is safe to call but
// never hits Redis.
func NewEmptyClaimCache(rdb *redis.Client) *EmptyClaimCache {
	if rdb == nil {
		return nil
	}
	return &EmptyClaimCache{rdb: rdb}
}

func emptyClaimKey(runtimeID string) string   { return emptyClaimCachePrefix + runtimeID }
func emptyClaimVersion(runtimeID string) string { return emptyClaimVersionPrefix + runtimeID }

func (c *EmptyClaimCache) bounded(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, emptyClaimRedisTimeout)
}

// CurrentVersion returns the runtime's current invalidation version.
// Callers MUST read this BEFORE the DB SELECT they are about to cache,
// then pass it back to MarkEmpty so a concurrent Bump invalidates the
// would-be cache write. Returns 0 (treated as "unknown") on cache miss
// or any Redis error — the caller falls through to the DB path.
//
// The version key is read with a short Expire refresh so that a long
// idle runtime does not let the counter expire and reset to 0 between
// an enqueue's Bump and the next claim's MarkEmpty.
func (c *EmptyClaimCache) CurrentVersion(ctx context.Context, runtimeID string) int64 {
	if c == nil || runtimeID == "" {
		return 0
	}
	bctx, cancel := c.bounded(ctx)
	defer cancel()
	v, err := c.rdb.Get(bctx, emptyClaimVersion(runtimeID)).Int64()
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			slog.Warn("empty_claim_cache: version get failed; falling back to DB", "error", err)
		}
		return 0
	}
	// Refresh TTL so the counter doesn't expire and reset on a low-
	// traffic runtime. Errors here are best-effort.
	c.rdb.Expire(bctx, emptyClaimVersion(runtimeID), emptyClaimVersionTTL)
	return v
}

// IsEmpty returns true only when (a) an empty verdict is cached AND
// (b) it carries the runtime's current version. A stale verdict
// written before a concurrent Bump returns false so the caller falls
// through to the DB.
func (c *EmptyClaimCache) IsEmpty(ctx context.Context, runtimeID string) bool {
	if c == nil || runtimeID == "" {
		return false
	}
	bctx, cancel := c.bounded(ctx)
	defer cancel()
	// MGET returns []interface{} of either the value (string) or nil.
	vals, err := c.rdb.MGet(bctx, emptyClaimKey(runtimeID), emptyClaimVersion(runtimeID)).Result()
	if err != nil {
		slog.Warn("empty_claim_cache: mget failed; falling back to DB", "error", err)
		return false
	}
	if len(vals) != 2 || vals[0] == nil {
		return false
	}
	emptyVer, ok := vals[0].(string)
	if !ok {
		return false
	}
	// A missing version key means "no enqueue has ever bumped this
	// runtime", which is logically version 0 — i.e. the same value
	// CurrentVersion returns on miss. A MarkEmpty written with v=0
	// must match here, otherwise the fast path would never trigger
	// for fresh runtimes.
	curVer := "0"
	if vals[1] != nil {
		if s, ok := vals[1].(string); ok {
			curVer = s
		}
	}
	return emptyVer == curVer
}

// MarkEmpty stores the empty verdict tagged with observedVersion. The
// verdict is later trusted only if observedVersion still equals the
// current version (see IsEmpty). Pass the value returned by
// CurrentVersion BEFORE the SELECT that confirmed the runtime was
// empty; a concurrent Bump between the two will make the next reader
// reject this entry, forcing a fresh DB check.
//
// Errors are logged and swallowed — a cache write failure is not a
// request failure.
func (c *EmptyClaimCache) MarkEmpty(ctx context.Context, runtimeID string, observedVersion int64) {
	if c == nil || runtimeID == "" {
		return
	}
	bctx, cancel := c.bounded(ctx)
	defer cancel()
	if err := c.rdb.Set(bctx, emptyClaimKey(runtimeID), strconv.FormatInt(observedVersion, 10), EmptyClaimCacheTTL).Err(); err != nil {
		slog.Warn("empty_claim_cache: set failed", "error", err)
	}
}

// Bump increments the runtime's invalidation version. Called from
// every enqueue path BEFORE the daemon WS wakeup so any verdict
// written under the previous version is rejected on the next read,
// without needing a separate DEL on the empty key.
//
// Errors are logged and swallowed — a Redis hiccup must not stop a
// legitimate enqueue. The empty key still expires on its own TTL so
// the worst-case stall is bounded.
func (c *EmptyClaimCache) Bump(ctx context.Context, runtimeID string) {
	if c == nil || runtimeID == "" {
		return
	}
	bctx, cancel := c.bounded(ctx)
	defer cancel()
	pipe := c.rdb.Pipeline()
	pipe.Incr(bctx, emptyClaimVersion(runtimeID))
	pipe.Expire(bctx, emptyClaimVersion(runtimeID), emptyClaimVersionTTL)
	if _, err := pipe.Exec(bctx); err != nil {
		slog.Warn("empty_claim_cache: bump failed; entry will expire on TTL", "error", err)
	}
}
