package handler

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
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

// DefaultWebhookIPRateLimit is the per-IP coarse budget applied BEFORE the
// trigger lookup. Set lower than the per-token budget on purpose: a single
// IP should rarely sustain more than 30 webhook deliveries / minute across
// all its tokens, while a malicious IP spraying random tokens hits this
// gate before it can probe Postgres.
func DefaultWebhookIPRateLimit() WebhookRateLimit {
	return WebhookRateLimit{Limit: 30, Window: time.Minute}
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

// webhookLimiterKey:<token> is the ZSET we keep timestamps in. The score is
// the request's nanosecond timestamp so ZREMRANGEBYSCORE can drop everything
// older than the cutoff and ZCARD tells us the remaining count. The member is
// a per-request unique id (NOT the timestamp): two requests landing in the
// same nanosecond would otherwise collide on an identical member, ZADD would
// update-in-place instead of inserting, and the window would under-count.
const (
	webhookLimiterKeyPrefix   = "mul:webhook:rate:"
	webhookIPLimiterKeyPrefix = "mul:webhook:ip:"
)

// webhookLimiterAllowSrc runs the slide-window check atomically on Redis:
//
//	KEYS[1] = ZSET key
//	ARGV[1] = now (unix nanos as string, used as the entry score)
//	ARGV[2] = cutoff (unix nanos as string)
//	ARGV[3] = limit
//	ARGV[4] = expiry seconds (TTL refresh, larger than window)
//	ARGV[5] = unique member id for this request
//
// Returns 1 when the request is admitted, 0 when it should be rejected.
//
// We trim first, then count, then optionally insert. Doing all three in a
// single Lua call avoids the classic "two pods both see count=limit-1 and
// both insert" race.
const webhookLimiterAllowSrc = `
local key = KEYS[1]
local now = tonumber(ARGV[1])
local cutoff = tonumber(ARGV[2])
local limit = tonumber(ARGV[3])
local ttl = tonumber(ARGV[4])
local member = ARGV[5]
redis.call('ZREMRANGEBYSCORE', key, '-inf', cutoff)
local count = redis.call('ZCARD', key)
if count >= limit then
    return 0
end
redis.call('ZADD', key, now, member)
redis.call('EXPIRE', key, ttl)
return 1
`

var webhookLimiterAllowScript = redis.NewScript(webhookLimiterAllowSrc)

// webhookLimiterAllowSource exposes the script body for tests that want to
// assert structural invariants (e.g. trim before count before insert)
// without spinning up a real Redis. Lower-cased "Source" makes the
// test-only intent explicit.
func webhookLimiterAllowSource() string { return webhookLimiterAllowSrc }

type redisWebhookRateLimiter struct {
	cfg       WebhookRateLimit
	rdb       *redis.Client
	keyPrefix string
}

func NewRedisWebhookRateLimiter(rdb *redis.Client, cfg WebhookRateLimit) WebhookRateLimiter {
	return &redisWebhookRateLimiter{cfg: cfg, rdb: rdb, keyPrefix: webhookLimiterKeyPrefix}
}

// NewRedisWebhookIPRateLimiter is the per-IP variant: same sliding-window
// Lua script, different key namespace so the two budgets don't interfere.
func NewRedisWebhookIPRateLimiter(rdb *redis.Client, cfg WebhookRateLimit) WebhookRateLimiter {
	return &redisWebhookRateLimiter{cfg: cfg, rdb: rdb, keyPrefix: webhookIPLimiterKeyPrefix}
}

// NewMemoryWebhookIPRateLimiter is the in-memory per-IP variant used when no
// Redis client is configured. Same per-key semantics as the per-token memory
// limiter — single-node only.
func NewMemoryWebhookIPRateLimiter(cfg WebhookRateLimit) WebhookRateLimiter {
	return NewMemoryWebhookRateLimiter(cfg)
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
	prefix := l.keyPrefix
	if prefix == "" {
		prefix = webhookLimiterKeyPrefix
	}
	// Unique member per request: the score carries the timestamp for the
	// sliding-window trim, but two requests in the same nanosecond must not
	// collapse onto one ZSET member, or the window under-counts.
	member := uuid.NewString()
	res, err := webhookLimiterAllowScript.Run(
		ctx,
		l.rdb,
		[]string{prefix + key},
		now, cutoff, l.cfg.Limit, ttlSeconds, member,
	).Int()
	if err != nil {
		// Fail open on Redis errors — webhook ingress should keep working
		// when the cache hiccups, since the rate limit is a safety net,
		// not a correctness requirement.
		return true
	}
	return res == 1
}
