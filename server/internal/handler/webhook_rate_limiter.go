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

// DefaultWebhookAbsoluteIPRateLimit is the high emergency ceiling applied to
// every webhook request before token lookup. Normal valid traffic should sit
// far below this budget; unlike the lower bad-credential debt limiter it is
// intentionally consumed by successful deliveries too.
func DefaultWebhookAbsoluteIPRateLimit() WebhookRateLimit {
	return WebhookRateLimit{Limit: 600, Window: time.Minute}
}

// WebhookRateLimiter is the contract implemented by both the in-memory and
// Redis-backed limiters.
//
// Check is non-consuming and is used to reject an IP that has already built
// up bad-credential debt. Allow consumes one unit. RetryAfter returns a safe
// client retry interval after either gate rejects.
type WebhookRateLimiter interface {
	Allow(ctx context.Context, key string) bool
	// Implementations may also satisfy webhookRateLimiterInspector. Keeping
	// the base contract at Allow preserves compatibility with billing and
	// tests that inject simple one-method safety gates.
}

type webhookRateLimiterInspector interface {
	Check(ctx context.Context, key string) bool
	RetryAfter(ctx context.Context, key string) time.Duration
}

func webhookLimiterCheck(ctx context.Context, limiter WebhookRateLimiter, key string) bool {
	if inspector, ok := limiter.(webhookRateLimiterInspector); ok {
		return inspector.Check(ctx, key)
	}
	return true
}

func webhookLimiterRetryAfter(ctx context.Context, limiter WebhookRateLimiter, key string) time.Duration {
	if inspector, ok := limiter.(webhookRateLimiterInspector); ok {
		return inspector.RetryAfter(ctx, key)
	}
	return time.Minute
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
	return l.evaluate(key, true)
}

func (l *memoryWebhookRateLimiter) Check(_ context.Context, key string) bool {
	return l.evaluate(key, false)
}

func (l *memoryWebhookRateLimiter) evaluate(key string, consume bool) bool {
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
	if consume {
		keep = append(keep, now)
	}
	l.hit[key] = keep
	return true
}

func (l *memoryWebhookRateLimiter) RetryAfter(_ context.Context, key string) time.Duration {
	if l.cfg.Limit <= 0 {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	hits := l.hit[key]
	if len(hits) == 0 {
		return l.cfg.Window
	}
	retry := time.Until(hits[0].Add(l.cfg.Window))
	if retry <= 0 {
		return time.Second
	}
	return retry
}

// ── Redis implementation ────────────────────────────────────────────────────

// webhookLimiterKey:<token> is the ZSET we keep timestamps in. The score is
// the request's nanosecond timestamp so ZREMRANGEBYSCORE can drop everything
// older than the cutoff and ZCARD tells us the remaining count. The member is
// a per-request unique id (NOT the timestamp): two requests landing in the
// same nanosecond would otherwise collide on an identical member, ZADD would
// update-in-place instead of inserting, and the window would under-count.
const (
	webhookLimiterKeyPrefix           = "mul:webhook:rate:"
	webhookIPLimiterKeyPrefix         = "mul:webhook:ip:"
	webhookAbsoluteIPLimiterKeyPrefix = "mul:webhook:absolute-ip:"
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

const webhookLimiterCheckSrc = `
local key = KEYS[1]
local cutoff = tonumber(ARGV[1])
local limit = tonumber(ARGV[2])
redis.call('ZREMRANGEBYSCORE', key, '-inf', cutoff)
local count = redis.call('ZCARD', key)
if count >= limit then
    return 0
end
return 1
`

var webhookLimiterCheckScript = redis.NewScript(webhookLimiterCheckSrc)

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

func NewRedisWebhookAbsoluteIPRateLimiter(rdb *redis.Client, cfg WebhookRateLimit) WebhookRateLimiter {
	return &redisWebhookRateLimiter{cfg: cfg, rdb: rdb, keyPrefix: webhookAbsoluteIPLimiterKeyPrefix}
}

// NewMemoryWebhookIPRateLimiter is the in-memory per-IP variant used when no
// Redis client is configured. Same per-key semantics as the per-token memory
// limiter — single-node only.
func NewMemoryWebhookIPRateLimiter(cfg WebhookRateLimit) WebhookRateLimiter {
	return NewMemoryWebhookRateLimiter(cfg)
}

func NewMemoryWebhookAbsoluteIPRateLimiter(cfg WebhookRateLimit) WebhookRateLimiter {
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

func (l *redisWebhookRateLimiter) Check(ctx context.Context, key string) bool {
	if l.cfg.Limit <= 0 || l.rdb == nil {
		return true
	}
	cutoff := time.Now().Add(-l.cfg.Window).UnixNano()
	prefix := l.keyPrefix
	if prefix == "" {
		prefix = webhookLimiterKeyPrefix
	}
	res, err := webhookLimiterCheckScript.Run(
		ctx,
		l.rdb,
		[]string{prefix + key},
		cutoff, l.cfg.Limit,
	).Int()
	if err != nil {
		return true
	}
	return res == 1
}

func (l *redisWebhookRateLimiter) RetryAfter(_ context.Context, _ string) time.Duration {
	if l.cfg.Window <= 0 {
		return time.Second
	}
	// A full window is conservative and remains correct across replicas even
	// if the oldest ZSET member changes between rejection and this response.
	return l.cfg.Window
}
