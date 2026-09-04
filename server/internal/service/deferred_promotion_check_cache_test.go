package service

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestDeferredPromotionCheckCache_NilFallsBackToEveryRuntime(t *testing.T) {
	var cache *DeferredPromotionCheckCache
	runtimes := []string{"rt-a", "rt-b"}
	due, cacheReady := cache.dueRuntimeIDs(context.Background(), runtimes, time.Now())
	if len(due) != len(runtimes) || due[0] != runtimes[0] || due[1] != runtimes[1] {
		t.Fatalf("nil cache due runtimes = %v, want %v", due, runtimes)
	}
	if cacheReady {
		t.Fatal("nil cache must not trigger DB reconciliation for cache refresh")
	}
	if cache.Track(context.Background(), "rt-a", "task-a", time.Now()) {
		t.Fatal("nil cache unexpectedly accepted a hint")
	}
	cache.Forget(context.Background(), "rt-a", "task-a")
	cache.MarkChecked(context.Background(), runtimes, time.Now(), time.Now().Add(DeferredPromotionCheckRetryInterval))
}

func TestNewDeferredPromotionCheckCache_NilRedisReturnsNil(t *testing.T) {
	if cache := NewDeferredPromotionCheckCache(nil); cache != nil {
		t.Fatalf("NewDeferredPromotionCheckCache(nil) = %#v, want nil", cache)
	}
}

func TestDeferredPromotionCheckCache_RedisFailureFallsBackToEveryRuntime(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	if err := rdb.Close(); err != nil {
		t.Fatalf("close Redis client: %v", err)
	}
	cache := NewDeferredPromotionCheckCache(rdb)
	runtimes := []string{"rt-a", "rt-b"}
	due, cacheReady := cache.dueRuntimeIDs(context.Background(), runtimes, time.Now())
	if len(due) != len(runtimes) || due[0] != runtimes[0] || due[1] != runtimes[1] {
		t.Fatalf("failed Redis due runtimes = %v, want %v", due, runtimes)
	}
	if cacheReady {
		t.Fatal("failed Redis must not trigger an extra DB reconciliation query")
	}
}

func TestDeferredPromotionCheckCache_TracksDeadlineAndRetriesDueHint(t *testing.T) {
	rdb := newRedisTestClient(t)
	cache := NewDeferredPromotionCheckCache(rdb)
	ctx := context.Background()
	now := time.Now()
	fireAt := now.Add(20 * time.Second)

	cache.MarkChecked(ctx, []string{"rt-a"}, now, now.Add(DeferredPromotionCheckRetryInterval))
	if !cache.Track(ctx, "rt-a", "task-a", fireAt) {
		t.Fatal("track failed")
	}
	newerFireAt := fireAt.Add(DeferredPromotionCheckRetryInterval + 10*time.Second)
	if !cache.Track(ctx, "rt-a", "task-b", newerFireAt) {
		t.Fatal("track newer task failed")
	}
	if due := cache.DueRuntimeIDs(ctx, []string{"rt-a"}, fireAt.Add(-time.Millisecond)); len(due) != 0 {
		t.Fatalf("task became due before fire_at: %v", due)
	}
	if due := cache.DueRuntimeIDs(ctx, []string{"rt-a"}, fireAt.Add(time.Millisecond)); len(due) != 1 {
		t.Fatalf("task did not become due after fire_at: %v", due)
	}

	retryAt := fireAt.Add(DeferredPromotionCheckRetryInterval)
	cache.MarkChecked(ctx, []string{"rt-a"}, fireAt, retryAt)
	if due := cache.DueRuntimeIDs(ctx, []string{"rt-a"}, retryAt.Add(-time.Millisecond)); len(due) != 0 {
		t.Fatalf("checked task retriggered before retry deadline: %v", due)
	}
	if due := cache.DueRuntimeIDs(ctx, []string{"rt-a"}, retryAt.Add(time.Millisecond)); len(due) != 1 {
		t.Fatalf("checked task did not retrigger after retry deadline: %v", due)
	}
	if score, err := rdb.ZScore(ctx, deferredPromotionCheckScheduleKey("rt-a"), "task-a").Result(); err != nil {
		t.Fatalf("read retry score: %v", err)
	} else if int64(score) != retryAt.UnixMilli() {
		t.Fatalf("retry score = %d, want %d", int64(score), retryAt.UnixMilli())
	}
	if score, err := rdb.ZScore(ctx, deferredPromotionCheckScheduleKey("rt-a"), "task-b").Result(); err != nil {
		t.Fatalf("read newer task score: %v", err)
	} else if int64(score) != newerFireAt.UnixMilli() {
		t.Fatalf("newer task score = %d, want %d", int64(score), newerFireAt.UnixMilli())
	}
}

func TestDeferredPromotionCheckCache_LongDeadlineOutlivesFireAt(t *testing.T) {
	rdb := newRedisTestClient(t)
	cache := NewDeferredPromotionCheckCache(rdb)
	ctx := context.Background()
	fireAt := time.Now().Add(24 * time.Hour)

	if !cache.Track(ctx, "rt-a", "task-a", fireAt) {
		t.Fatal("track failed")
	}
	// Adding an earlier task must not shorten the shared ZSET's TTL below the
	// later task's deadline.
	if !cache.Track(ctx, "rt-a", "task-b", time.Now().Add(time.Minute)) {
		t.Fatal("track earlier task failed")
	}
	ttl, err := rdb.PTTL(ctx, deferredPromotionCheckScheduleKey("rt-a")).Result()
	if err != nil {
		t.Fatalf("schedule PTTL: %v", err)
	}
	if ttl <= time.Until(fireAt) {
		t.Fatalf("schedule TTL %v does not outlive fire_at %v", ttl, fireAt)
	}
}

func TestDeferredPromotionCheckCache_ForgetAndBackstop(t *testing.T) {
	rdb := newRedisTestClient(t)
	cache := NewDeferredPromotionCheckCache(rdb)
	ctx := context.Background()
	now := time.Now()

	if due, cacheReady := cache.dueRuntimeIDs(ctx, []string{"rt-a"}, now); len(due) != 1 || !cacheReady {
		t.Fatalf("missing backstop decision = due:%v cacheReady:%v, want due + refreshable", due, cacheReady)
	}
	cache.MarkChecked(ctx, []string{"rt-a"}, now, now.Add(DeferredPromotionCheckRetryInterval))
	if due := cache.DueRuntimeIDs(ctx, []string{"rt-a"}, now.Add(time.Second)); len(due) != 0 {
		t.Fatalf("fresh backstop unexpectedly due: %v", due)
	}
	if !cache.Track(ctx, "rt-a", "task-a", now.Add(time.Second)) {
		t.Fatal("track failed")
	}
	cache.Forget(ctx, "rt-a", "task-a")
	if _, err := rdb.ZScore(ctx, deferredPromotionCheckScheduleKey("rt-a"), "task-a").Result(); !errors.Is(err, redis.Nil) {
		t.Fatalf("forgotten task score error = %v, want redis.Nil", err)
	}
	if due := cache.DueRuntimeIDs(ctx, []string{"rt-a"}, now.Add(DeferredPromotionCheckBackstopInterval+time.Millisecond)); len(due) != 1 {
		t.Fatalf("elapsed backstop did not force DB check: %v", due)
	}
}

func TestDeferredPromotionCheckCache_DueRuntimeIDsPreservesInputOrder(t *testing.T) {
	rdb := newRedisTestClient(t)
	cache := NewDeferredPromotionCheckCache(rdb)
	ctx := context.Background()
	now := time.Now()

	cache.MarkChecked(ctx, []string{"rt-b"}, now, now.Add(DeferredPromotionCheckRetryInterval))
	cache.Track(ctx, "rt-b", "task-b", now.Add(time.Second))
	cache.MarkChecked(
		ctx,
		[]string{"rt-a"},
		now.Add(-DeferredPromotionCheckBackstopInterval-time.Second),
		now.Add(DeferredPromotionCheckRetryInterval),
	)

	runtimes := []string{"rt-b", "rt-a"}
	if due := cache.DueRuntimeIDs(ctx, runtimes, now.Add(2*time.Second)); len(due) != 2 || due[0] != runtimes[0] || due[1] != runtimes[1] {
		t.Fatalf("due runtimes = %v, want input order %v", due, runtimes)
	}
}

func TestDeferredPromotionCheckCache_MarkCheckedBatchLimitFailsOpen(t *testing.T) {
	rdb := newRedisTestClient(t)
	cache := NewDeferredPromotionCheckCache(rdb)
	ctx := context.Background()
	now := time.Now()

	members := make([]redis.Z, 0, deferredPromotionCheckRetryBatchSize+1)
	for i := 0; i <= deferredPromotionCheckRetryBatchSize; i++ {
		members = append(members, redis.Z{
			Score:  float64(now.Add(-time.Second).UnixMilli()),
			Member: "task-" + strconv.Itoa(i),
		})
	}
	key := deferredPromotionCheckScheduleKey("rt-a")
	if err := rdb.ZAdd(ctx, key, members...).Err(); err != nil {
		t.Fatalf("seed due hints: %v", err)
	}
	if err := rdb.Expire(ctx, key, DeferredPromotionCheckMinScheduleTTL).Err(); err != nil {
		t.Fatalf("expire due hints: %v", err)
	}
	cache.MarkChecked(ctx, []string{"rt-a"}, now, now.Add(DeferredPromotionCheckRetryInterval))

	if due := cache.DueRuntimeIDs(ctx, []string{"rt-a"}, now.Add(time.Second)); len(due) != 1 {
		t.Fatalf("overflow hint did not fail open to another check: %v", due)
	}
}
