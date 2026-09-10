package service

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	reclaimCheckSchedulePrefix = "mul:claim:runtime:reclaim-schedule:"
	reclaimCheckBackstopPrefix = "mul:claim:runtime:reclaim-backstop:"
	reclaimCheckRetryBatchSize = 256

	// The backstop remains at or below the PostgreSQL recovery window so a
	// missed dispatch-side Redis write cannot delay recovery beyond one window.
	ReclaimCheckBackstopInterval = claimResponseRecoveryWindow
	// A due hint that produces no reclaimed row is retried at the daemon's next
	// normal polling cadence instead of becoming inert until the backstop.
	ReclaimCheckRetryInterval = 30 * time.Second
	// The Redis hint is captured immediately before the PostgreSQL dispatch
	// command. Leave a small margin for that command to commit so a hint cannot
	// fire just before the database's strict recovery-window predicate does.
	ReclaimCheckHintSafetyMargin = 5 * time.Second
	// A fresh task's first hint is one recovery window plus the safety margin in
	// the future. Keep the containing ZSET alive for another full window so the
	// hint survives its deadline and several bounded retries.
	ReclaimCheckScheduleTTL = 2*claimResponseRecoveryWindow + ReclaimCheckHintSafetyMargin
)

// These scripts use only Redis 2.6-era primitives. In particular, they avoid
// ZADD GT (Redis 6.2+) so self-hosted installations do not silently lose the
// scheduling optimization on older Redis versions.
const reclaimCheckTrackLaterScript = `
local current = redis.call('ZSCORE', KEYS[1], ARGV[1])
if not current then
    return 0
end
if tonumber(current) < tonumber(ARGV[2]) then
    redis.call('ZADD', KEYS[1], ARGV[2], ARGV[1])
end
redis.call('PEXPIRE', KEYS[1], ARGV[3])
return 1
`

const reclaimCheckDeferDueScript = `
local members = redis.call('ZRANGEBYSCORE', KEYS[1], '-inf', ARGV[1], 'LIMIT', '0', ARGV[3])
for _, member in ipairs(members) do
    redis.call('ZADD', KEYS[1], ARGV[2], member)
end
return #members
`

// ReclaimCheckCache keeps the batch/singular claim hot paths from issuing a
// stale-dispatch UPDATE when no task can possibly be reclaimed yet.
//
// Each runtime owns a sorted set of task IDs scored by their earliest known
// reclaim time. A separate last-checked timestamp forces a periodic DB check
// for cache loss, pre-deployment rows, or a missed write. Task scores and the
// last-checked timestamp are both application-clock values; PostgreSQL remains
// authoritative for actual eligibility when the query runs. Missing keys and
// Redis errors always mean "check PostgreSQL now", so the cache can only reduce
// load; it is never authoritative for task recovery.
type ReclaimCheckCache struct {
	rdb *redis.Client
}

func NewReclaimCheckCache(rdb *redis.Client) *ReclaimCheckCache {
	if rdb == nil {
		return nil
	}
	return &ReclaimCheckCache{rdb: rdb}
}

func reclaimCheckScheduleKey(runtimeID string) string {
	return reclaimCheckSchedulePrefix + runtimeID
}

func reclaimCheckBackstopKey(runtimeID string) string {
	return reclaimCheckBackstopPrefix + runtimeID
}

func (c *ReclaimCheckCache) bounded(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, emptyClaimRedisTimeout)
}

// DueRuntimeIDs returns only runtimes with a due task hint or whose periodic
// backstop has elapsed. It preserves the relative order of non-empty input IDs.
// MarkChecked advances hints seen by a completed DB check to a bounded retry
// deadline, so an unrecovered task is tried again without retriggering every
// poll. The schedule TTL remains a hard bound on stale cleanup hints.
//
// A nil cache, missing/malformed backstop, or Redis failure fails open. Batch
// callers treat any due runtime as a reason to query their complete runtime set.
func (c *ReclaimCheckCache) DueRuntimeIDs(ctx context.Context, runtimeIDs []string, now time.Time) []string {
	if len(runtimeIDs) == 0 {
		return nil
	}
	validRuntimeIDs := make([]string, 0, len(runtimeIDs))
	for _, runtimeID := range runtimeIDs {
		if runtimeID == "" {
			continue
		}
		validRuntimeIDs = append(validRuntimeIDs, runtimeID)
	}
	if len(validRuntimeIDs) == 0 {
		return nil
	}
	if c == nil {
		return validRuntimeIDs
	}

	bctx, cancel := c.bounded(ctx)
	defer cancel()
	backstopKeys := make([]string, 0, len(validRuntimeIDs))
	for _, runtimeID := range validRuntimeIDs {
		backstopKeys = append(backstopKeys, reclaimCheckBackstopKey(runtimeID))
	}
	backstops, err := c.rdb.MGet(bctx, backstopKeys...).Result()
	if err != nil {
		slog.Warn("reclaim_check_cache: backstop read failed; falling back to DB", "error", err)
		return validRuntimeIDs
	}
	if len(backstops) != len(validRuntimeIDs) {
		slog.Warn("reclaim_check_cache: incomplete backstop read; falling back to DB")
		return validRuntimeIDs
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
			// A missing or non-string value means this runtime has no trustworthy
			// successful-check marker.
			due[i] = true
			continue
		}
		checkedMillis, err := strconv.ParseInt(backstop, 10, 64)
		if err != nil {
			due[i] = true
			continue
		}
		nextBackstop := checkedMillis + ReclaimCheckBackstopInterval.Milliseconds()
		if nextBackstop < checkedMillis || nextBackstop <= nowMillis {
			due[i] = true
			continue
		}
		scheduleCommands = append(scheduleCommands, scheduleCommand{
			index: i,
			count: pipe.ZCount(
				bctx,
				reclaimCheckScheduleKey(runtimeID),
				"-inf",
				strconv.FormatInt(nowMillis, 10),
			),
		})
	}
	if len(scheduleCommands) > 0 {
		if _, err := pipe.Exec(bctx); err != nil {
			slog.Warn("reclaim_check_cache: schedule read failed; falling back to DB", "error", err)
			return validRuntimeIDs
		}
		for _, command := range scheduleCommands {
			count, err := command.count.Result()
			if err != nil {
				slog.Warn("reclaim_check_cache: schedule result failed; falling back to DB", "error", err)
				return validRuntimeIDs
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
	return dueRuntimeIDs
}

// Track records the earliest known reclaim time for one dispatched task. ZADD
// replaces the task's score after a fresh dispatch/reclaim; multiple tasks on
// one runtime remain independent, avoiding a runtime-level timestamp losing a
// second task's earlier recovery deadline.
func (c *ReclaimCheckCache) Track(ctx context.Context, runtimeID, taskID string, checkAfter time.Time) {
	c.track(ctx, runtimeID, taskID, checkAfter, false)
}

// TrackLater advances an existing task hint only when a prepare-lease extension
// protects it beyond its current recovery deadline. It deliberately does not
// create a missing member: without the initial dispatch deadline, a short lease
// could schedule an early failed check and delay recovery until the next
// backstop. Missing state already fails open through that bounded backstop.
func (c *ReclaimCheckCache) TrackLater(ctx context.Context, runtimeID, taskID string, checkAfter time.Time) {
	c.track(ctx, runtimeID, taskID, checkAfter, true)
}

func (c *ReclaimCheckCache) track(ctx context.Context, runtimeID, taskID string, checkAfter time.Time, onlyLater bool) {
	if c == nil || runtimeID == "" || taskID == "" || checkAfter.IsZero() {
		return
	}
	bctx, cancel := c.bounded(ctx)
	defer cancel()
	key := reclaimCheckScheduleKey(runtimeID)
	if onlyLater {
		if err := c.rdb.Eval(
			bctx,
			reclaimCheckTrackLaterScript,
			[]string{key},
			taskID,
			checkAfter.UnixMilli(),
			ReclaimCheckScheduleTTL.Milliseconds(),
		).Err(); err != nil {
			slog.Warn("reclaim_check_cache: extend hint failed; DB fallback remains active", "error", err)
		}
		return
	}
	pipe := c.rdb.Pipeline()
	member := redis.Z{Score: float64(checkAfter.UnixMilli()), Member: taskID}
	pipe.ZAdd(bctx, key, member)
	pipe.Expire(bctx, key, ReclaimCheckScheduleTTL)
	if _, err := pipe.Exec(bctx); err != nil {
		slog.Warn("reclaim_check_cache: track failed; DB fallback remains active", "error", err)
	}
}

// Forget removes a task that can no longer be reclaimed (started, requeued,
// waiting on a local directory, or terminal). Failures are harmless: the stale
// hint can cause one extra DB check but cannot make an ineligible task run.
func (c *ReclaimCheckCache) Forget(ctx context.Context, runtimeID, taskID string) {
	if c == nil || runtimeID == "" || taskID == "" {
		return
	}
	bctx, cancel := c.bounded(ctx)
	defer cancel()
	if err := c.rdb.ZRem(bctx, reclaimCheckScheduleKey(runtimeID), taskID).Err(); err != nil {
		slog.Warn("reclaim_check_cache: forget failed; stale hint will expire", "error", err)
	}
}

// MarkChecked records a successful PostgreSQL reclaim pass for every runtime in
// the machine-level polling set. Hints that actually triggered this pass are
// atomically moved to retryAfter instead of deleted or made inert: SKIP LOCKED
// and runtime-health predicates mean an empty result cannot prove those tasks no
// longer need recovery. Concurrent hints newer than checkedThrough are left
// untouched. The bounded batch prevents a pathological ZSET from monopolizing
// Redis; any excess due hints remain due and fail open to another DB pass.
func (c *ReclaimCheckCache) MarkChecked(ctx context.Context, runtimeIDs []string, checkedThrough, retryAfter time.Time) {
	if c == nil || len(runtimeIDs) == 0 {
		return
	}
	if retryAfter.IsZero() || !retryAfter.After(checkedThrough) {
		retryAfter = checkedThrough.Add(ReclaimCheckRetryInterval)
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
			reclaimCheckDeferDueScript,
			[]string{reclaimCheckScheduleKey(runtimeID)},
			checkedMillis,
			retryMillis,
			reclaimCheckRetryBatchSize,
		)
		pipe.Set(bctx, reclaimCheckBackstopKey(runtimeID), checkedMillis, ReclaimCheckBackstopInterval)
	}
	if _, err := pipe.Exec(bctx); err != nil {
		slog.Warn("reclaim_check_cache: mark checked failed; falling back to DB", "error", err)
	}
}
