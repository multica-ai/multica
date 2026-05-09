package handler

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// WebhookRateLimit is a coarse per-token sliding-window limiter.
//
// Defaults: 60 requests per 60s (1 RPS sustained, with bursts up to 60). The
// goal is "stop a misconfigured or malicious sender from hammering us
// indefinitely" — not "shape traffic to a precise budget" — so the
// implementation aims for cheap and good-enough rather than exact.
type WebhookRateLimit struct {
	Limit  int           // maximum requests per window
	Window time.Duration // sliding window length
}

func DefaultWebhookRateLimit() WebhookRateLimit {
	return WebhookRateLimit{Limit: 60, Window: time.Minute}
}

// WebhookRateLimiter is the contract implemented by both the in-memory and
// Redis-backed limiters.
//
// Allow returns true when the request is within budget for the given key,
// false when it should be rejected (HTTP 429).
type WebhookRateLimiter interface {
	Allow(ctx context.Context, key string) bool
}

// ── In-memory implementation ────────────────────────────────────────────────

// memoryWebhookRateLimiter keeps per-key timestamps in a slice and prunes them
// on every call. Adequate for single-node dev / tests; production multi-node
// deployments should use the Redis-backed implementation so rate budgets are
// shared across pods.
type memoryWebhookRateLimiter struct {
	cfg WebhookRateLimit
	mu  sync.Mutex
	hit map[string][]time.Time
}

func NewMemoryWebhookRateLimiter(cfg WebhookRateLimit) WebhookRateLimiter {
	return &memoryWebhookRateLimiter{cfg: cfg, hit: make(map[string][]time.Time)}
}

func (l *memoryWebhookRateLimiter) Allow(_ context.Context, key string) bool {
	if l.cfg.Limit <= 0 {
		return true
	}
	now := time.Now()
	cutoff := now.Add(-l.cfg.Window)

	l.mu.Lock()
	defer l.mu.Unlock()

	hits := l.hit[key]
	// Trim entries that fell out of the window.
	keep := hits[:0]
	for _, t := range hits {
		if t.After(cutoff) {
			keep = append(keep, t)
		}
	}
	if len(keep) >= l.cfg.Limit {
		l.hit[key] = keep
		return false
	}
	keep = append(keep, now)
	l.hit[key] = keep
	return true
}

// ── Redis implementation ────────────────────────────────────────────────────

// webhookLimiterKey:<token> is the ZSET we keep timestamps in. Members are
// nanosecond timestamps as strings. Score = same value, so ZREMRANGEBYSCORE
// can drop everything older than the cutoff, then ZCARD tells us the
// remaining count.
const webhookLimiterKeyPrefix = "mul:webhook:rate:"

// allowScript runs the slide-window check atomically on Redis:
//
//	KEYS[1] = ZSET key
//	ARGV[1] = now (unix nanos as string)
//	ARGV[2] = cutoff (unix nanos as string)
//	ARGV[3] = limit
//	ARGV[4] = expiry seconds (TTL refresh, larger than window)
//
// Returns 1 when the request is admitted, 0 when it should be rejected.
//
// We trim first, then count, then optionally insert. Doing all three in a
// single Lua call avoids the classic "two pods both see count=limit-1 and
// both insert" race.
var webhookLimiterAllowScript = redis.NewScript(`
local key = KEYS[1]
local now = tonumber(ARGV[1])
local cutoff = tonumber(ARGV[2])
local limit = tonumber(ARGV[3])
local ttl = tonumber(ARGV[4])
redis.call('ZREMRANGEBYSCORE', key, '-inf', cutoff)
local count = redis.call('ZCARD', key)
if count >= limit then
    return 0
end
redis.call('ZADD', key, now, tostring(now))
redis.call('EXPIRE', key, ttl)
return 1
`)

type redisWebhookRateLimiter struct {
	cfg WebhookRateLimit
	rdb *redis.Client
}

func NewRedisWebhookRateLimiter(rdb *redis.Client, cfg WebhookRateLimit) WebhookRateLimiter {
	return &redisWebhookRateLimiter{cfg: cfg, rdb: rdb}
}

func (l *redisWebhookRateLimiter) Allow(ctx context.Context, key string) bool {
	if l.cfg.Limit <= 0 || l.rdb == nil {
		return true
	}
	now := time.Now().UnixNano()
	cutoff := time.Now().Add(-l.cfg.Window).UnixNano()
	ttlSeconds := int64(l.cfg.Window/time.Second) * 2
	if ttlSeconds < 1 {
		ttlSeconds = 1
	}
	res, err := webhookLimiterAllowScript.Run(
		ctx,
		l.rdb,
		[]string{webhookLimiterKeyPrefix + key},
		now, cutoff, l.cfg.Limit, ttlSeconds,
	).Int()
	if err != nil {
		// Fail open on Redis errors — webhook ingress should keep working
		// when the cache hiccups, since the rate limit is a safety net,
		// not a correctness requirement.
		return true
	}
	return res == 1
}
