package service

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	deferredPromotionCheckSchedulePrefix = "mul:claim:runtime:deferred-promotion-schedule:"
	deferredPromotionCheckBackstopPrefix = "mul:claim:runtime:deferred-promotion-backstop:"
	deferredPromotionCheckRetryBatchSize = 256

	// Match the existing empty-claim safety net: enqueue-time hints normally
	// schedule the exact fire_at deadline, while this bounds a missed write or
	// rolling-deploy gap without restoring two SQL statements on every poll.
	DeferredPromotionCheckBackstopInterval = EmptyClaimCacheTTL
	// A due task that remains deferred because its runtime or issue+agent slot is
	// temporarily ineligible is retried at the daemon's normal polling cadence.
	DeferredPromotionCheckRetryInterval = 30 * time.Second
	// Track gives every schedule at least two backstop windows after its latest
	// known deadline. Longer-lived deferred tasks receive a correspondingly
	// longer TTL so their hints cannot expire before fire_at.
	DeferredPromotionCheckMinScheduleTTL = 2 * DeferredPromotionCheckBackstopInterval
)

// These scripts intentionally use only Redis 2.6-era primitives, matching
// ReclaimCheckCache's self-hosted compatibility floor.
const deferredPromotionCheckTrackScript = `
redis.call('ZADD', KEYS[1], ARGV[2], ARGV[1])
local current_ttl = redis.call('PTTL', KEYS[1])
if current_ttl < tonumber(ARGV[3]) then
    redis.call('PEXPIRE', KEYS[1], ARGV[3])
end
return 1
`

const deferredPromotionCheckDeferDueScript = `
local members = redis.call('ZRANGEBYSCORE', KEYS[1], '-inf', ARGV[1], 'LIMIT', '0', ARGV[3])
for _, member in ipairs(members) do
    redis.call('ZADD', KEYS[1], ARGV[2], member)
end
return #members
`

// DeferredPromotionCheckCache keeps claim polling from issuing the cancel and
// promote-deferred statements when no task can have reached fire_at yet.
//
// Each runtime owns a sorted set of deferred task IDs scored by fire_at. A
// separate last-checked timestamp forces a periodic PostgreSQL reconciliation
// for pre-deployment rows, rolling deploys, or a missed enqueue-time write.
// PostgreSQL remains authoritative for promotion eligibility; missing state and
// Redis errors always mean "check PostgreSQL now".
type DeferredPromotionCheckCache struct {
	rdb *redis.Client
}

func NewDeferredPromotionCheckCache(rdb *redis.Client) *DeferredPromotionCheckCache {
	if rdb == nil {
		return nil
	}
	return &DeferredPromotionCheckCache{rdb: rdb}
}

func deferredPromotionCheckScheduleKey(runtimeID string) string {
	return deferredPromotionCheckSchedulePrefix + runtimeID
}

func deferredPromotionCheckBackstopKey(runtimeID string) string {
	return deferredPromotionCheckBackstopPrefix + runtimeID
}

func (c *DeferredPromotionCheckCache) bounded(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, emptyClaimRedisTimeout)
}

// DueRuntimeIDs returns runtimes with a due task hint or elapsed backstop while
// preserving input order. A nil cache, missing/malformed backstop, or Redis
// failure fails open. Batch callers use any due member to check their complete
// machine-level runtime set so fixed SQL costs and backstops stay aligned.
func (c *DeferredPromotionCheckCache) DueRuntimeIDs(ctx context.Context, runtimeIDs []string, now time.Time) []string {
	due, _ := c.dueRuntimeIDs(ctx, runtimeIDs, now)
	return due
}

// dueRuntimeIDs also reports whether Redis answered the complete decision.
// Claim paths use that bit to avoid adding a reconciliation SELECT while Redis
// is unavailable: the fail-open path must cost no more SQL than it did before
// the cache existed.
func (c *DeferredPromotionCheckCache) dueRuntimeIDs(ctx context.Context, runtimeIDs []string, now time.Time) ([]string, bool) {
	if len(runtimeIDs) == 0 {
		return nil, false
	}
	validRuntimeIDs := make([]string, 0, len(runtimeIDs))
	for _, runtimeID := range runtimeIDs {
		if runtimeID != "" {
			validRuntimeIDs = append(validRuntimeIDs, runtimeID)
		}
	}
	if len(validRuntimeIDs) == 0 {
		return nil, false
	}
	if c == nil {
		return validRuntimeIDs, false
	}

	bctx, cancel := c.bounded(ctx)
	defer cancel()
	backstopKeys := make([]string, 0, len(validRuntimeIDs))
	for _, runtimeID := range validRuntimeIDs {
		backstopKeys = append(backstopKeys, deferredPromotionCheckBackstopKey(runtimeID))
	}
	backstops, err := c.rdb.MGet(bctx, backstopKeys...).Result()
	if err != nil {
		slog.Warn("deferred_promotion_check_cache: backstop read failed; falling back to DB", "error", err)
		return validRuntimeIDs, false
	}
	if len(backstops) != len(validRuntimeIDs) {
		slog.Warn("deferred_promotion_check_cache: incomplete backstop read; falling back to DB")
		return validRuntimeIDs, false
	}

	nowMillis := now.UnixMilli()
	type scheduleCommand struct {
		index int
		count *redis.IntCmd
	}
	due := make([]bool, len(validRuntimeIDs))
	pipe := c.rdb.Pipeline()
	scheduleCommands := make([]scheduleCommand, 0, len(validRuntimeIDs))
	for i, runtimeID := range validRuntimeIDs {
		backstop, ok := backstops[i].(string)
		if !ok {
			due[i] = true
			continue
		}
		checkedMillis, err := strconv.ParseInt(backstop, 10, 64)
		if err != nil {
			due[i] = true
			continue
		}
		nextBackstop := checkedMillis + DeferredPromotionCheckBackstopInterval.Milliseconds()
		if nextBackstop < checkedMillis || nextBackstop <= nowMillis {
			due[i] = true
			continue
		}
		scheduleCommands = append(scheduleCommands, scheduleCommand{
			index: i,
			count: pipe.ZCount(
				bctx,
				deferredPromotionCheckScheduleKey(runtimeID),
				"-inf",
				strconv.FormatInt(nowMillis, 10),
			),
		})
	}
	if len(scheduleCommands) > 0 {
		if _, err := pipe.Exec(bctx); err != nil {
			slog.Warn("deferred_promotion_check_cache: schedule read failed; falling back to DB", "error", err)
			return validRuntimeIDs, false
		}
		for _, command := range scheduleCommands {
			count, err := command.count.Result()
			if err != nil {
				slog.Warn("deferred_promotion_check_cache: schedule result failed; falling back to DB", "error", err)
				return validRuntimeIDs, false
			}
			if count > 0 {
				due[command.index] = true
			}
		}
	}

	dueRuntimeIDs := make([]string, 0, len(validRuntimeIDs))
	for i, runtimeID := range validRuntimeIDs {
		if due[i] {
			dueRuntimeIDs = append(dueRuntimeIDs, runtimeID)
		}
	}
	return dueRuntimeIDs, true
}

// Track records one committed deferred task at its application-clock fire_at.
// It returns true only when Redis accepted the hint, allowing a DB
// reconciliation caller to avoid publishing a misleading fresh backstop.
func (c *DeferredPromotionCheckCache) Track(ctx context.Context, runtimeID, taskID string, fireAt time.Time) bool {
	if c == nil || runtimeID == "" || taskID == "" || fireAt.IsZero() {
		return false
	}
	ttl := time.Until(fireAt) + DeferredPromotionCheckMinScheduleTTL
	if ttl < DeferredPromotionCheckMinScheduleTTL {
		ttl = DeferredPromotionCheckMinScheduleTTL
	}
	bctx, cancel := c.bounded(ctx)
	defer cancel()
	if err := c.rdb.Eval(
		bctx,
		deferredPromotionCheckTrackScript,
		[]string{deferredPromotionCheckScheduleKey(runtimeID)},
		taskID,
		fireAt.UnixMilli(),
		ttl.Milliseconds(),
	).Err(); err != nil {
		slog.Warn("deferred_promotion_check_cache: track failed; DB fallback remains active", "error", err)
		return false
	}
	return true
}

// Forget removes a task that is no longer deferred. Failure can only leave a
// stale hint that causes an extra DB check; it cannot promote an ineligible row.
func (c *DeferredPromotionCheckCache) Forget(ctx context.Context, runtimeID, taskID string) {
	if c == nil || runtimeID == "" || taskID == "" {
		return
	}
	bctx, cancel := c.bounded(ctx)
	defer cancel()
	if err := c.rdb.ZRem(bctx, deferredPromotionCheckScheduleKey(runtimeID), taskID).Err(); err != nil {
		slog.Warn("deferred_promotion_check_cache: forget failed; stale hint will expire", "error", err)
	}
}

// MarkChecked records a successful PostgreSQL cancel/promote/reconciliation
// pass. Hints due at the start of the pass move to retryAfter instead of being
// deleted: runtime health, occupied issue+agent slots, and concurrent rows can
// legitimately leave them deferred. Newer concurrent hints stay untouched.
func (c *DeferredPromotionCheckCache) MarkChecked(ctx context.Context, runtimeIDs []string, checkedThrough, retryAfter time.Time) {
	if c == nil || len(runtimeIDs) == 0 {
		return
	}
	if retryAfter.IsZero() || !retryAfter.After(checkedThrough) {
		retryAfter = checkedThrough.Add(DeferredPromotionCheckRetryInterval)
	}
	bctx, cancel := c.bounded(ctx)
	defer cancel()
	checkedMillis := strconv.FormatInt(checkedThrough.UnixMilli(), 10)
	retryMillis := strconv.FormatInt(retryAfter.UnixMilli(), 10)
	pipe := c.rdb.Pipeline()
	for _, runtimeID := range runtimeIDs {
		if runtimeID == "" {
			continue
		}
		pipe.Eval(
			bctx,
			deferredPromotionCheckDeferDueScript,
			[]string{deferredPromotionCheckScheduleKey(runtimeID)},
			checkedMillis,
			retryMillis,
			deferredPromotionCheckRetryBatchSize,
		)
		pipe.Set(
			bctx,
			deferredPromotionCheckBackstopKey(runtimeID),
			checkedMillis,
			DeferredPromotionCheckBackstopInterval,
		)
	}
	if _, err := pipe.Exec(bctx); err != nil {
		slog.Warn("deferred_promotion_check_cache: mark checked failed; falling back to DB", "error", err)
	}
}
