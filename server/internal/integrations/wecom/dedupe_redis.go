package wecom

// dedupe_redis.go — the at-most-once claim behind a routed delivery.
//
// It lives on the same Redis the relay itself runs on, which is the whole
// reason this is not a new dependency: no Redis means no relay, which means no
// cross-replica routing and nothing to deduplicate. A deployment that never
// needed Redis is not asked for it now.
//
// One key per turn, keyed on the same event id the stream entry carries, so a
// frame replayed after a restart and a frame read by two replicas
// mid-lease-move meet the same key. The value is the owner's token while the
// delivery is in flight, then "settled" (its holder recorded the outcome) or
// "lost" (the publisher did).

import (
	"context"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// redisDedupe is a DedupeStore backed by one Redis key per delivery.
type redisDedupe struct {
	rdb    *redis.Client
	log    *slog.Logger
	budget time.Duration
}

// NewRedisDedupe builds the production claim store. A nil client yields nil,
// which RelayOutbound reads as "no cross-restart claim" — correct only where
// there is no relay either. A budget of zero takes defaultClaimBudget; tests
// shrink it so the outcome grace derived from it stays a test's worth of time.
func NewRedisDedupe(rdb *redis.Client, budget time.Duration, log *slog.Logger) DedupeStore {
	if rdb == nil {
		return nil
	}
	if log == nil {
		log = slog.Default()
	}
	if budget <= 0 {
		budget = defaultClaimBudget
	}
	// The budgets below are only real if the client applies them to the wire.
	// go-redis does not by default: without ContextTimeoutEnabled a command
	// ignores its context's deadline and waits out the socket timeout instead
	// (baseClient.context), so ClaimBudget would be a number this package
	// states and nothing enforces — and a shutdown drain would overrun the
	// exit budget it promised by however far the socket timeout reaches.
	// Production passes a client built for this (cmd/server: newClaimRedisClient);
	// anything else is a wiring mistake, and a silent one, so it is named here.
	if !rdb.Options().ContextTimeoutEnabled {
		log.Warn("wecom relay: claim store client ignores context deadlines; " +
			"claim budgets and the shutdown drain budget will not be honoured " +
			"(set redis.Options.ContextTimeoutEnabled)")
	}
	return &redisDedupe{rdb: rdb, log: log, budget: budget}
}

// defaultClaimBudget bounds one round trip. A dispatcher worker waiting on
// Redis is a worker not delivering, and the frame is replayable, so failing
// fast and letting the replay bring it back beats holding the queue.
const defaultClaimBudget = 2 * time.Second

// ClaimBudget reports the bound this store actually applies, which is what
// sizes RelayOutbound.outcomeGrace.
func (d *redisDedupe) ClaimBudget() time.Duration { return d.budget }

// Every mutation is one Lua operation keyed on the owner token, the same
// shape engine.RedisLeaseStore uses for channel leases: compare and act in the
// server, so a command the client re-sends after a lost response can never
// act on a claim a later owner holds. That is what makes Release safe to
// retry and lets a DEL whose response was lost be answered by the next
// Claim rather than guessed at.
const (
	redisClaimSource = `
local v = redis.call('GET', KEYS[1])
if (not v) then
  redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[2])
  return 1
end
if v == ARGV[1] then
  return 1
end
return 0
`
	redisReleaseSource = `
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('DEL', KEYS[1])
end
return 0
`
	redisSettleSource = `
local v = redis.call('GET', KEYS[1])
if v == ARGV[1] then
  redis.call('SET', KEYS[1], ARGV[2], 'KEEPTTL')
  return 1
end
if v == ARGV[2] then
  return 1
end
return 0
`
	// Resolve reads the state and, for a key still held by a token, fences
	// it as lost in the same operation. Return codes are claimState values.
	//
	// A value that is not token-shaped is a claim from before this scheme —
	// the plain "1" of the SET NX that used to be the whole claim. Its holder
	// recorded its own outcome inline, so it is reported as settled and left
	// alone: fencing it would record a second outcome for a reply that was
	// already counted. Every token carries the "/" tokenFor puts there, so
	// the two are told apart without a version flag or a key migration.
	redisResolveSource = `
local v = redis.call('GET', KEYS[1])
if (not v) then
  return 0
end
if v == ARGV[1] then
  return 2
end
if v == ARGV[2] then
  return 3
end
if not string.find(v, '/', 1, true) then
  return 2
end
redis.call('SET', KEYS[1], ARGV[2], 'KEEPTTL')
return 1
`
)

var (
	redisClaim   = redis.NewScript(redisClaimSource)
	redisRelease = redis.NewScript(redisReleaseSource)
	redisSettle  = redis.NewScript(redisSettleSource)
	redisResolve = redis.NewScript(redisResolveSource)
)

func (d *redisDedupe) Claim(ctx context.Context, key, token string, ttl time.Duration) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, d.budget)
	defer cancel()
	n, err := redisClaim.Run(ctx, d.rdb, []string{key}, token, ttl.Milliseconds()).Int()
	return n == 1, err
}

// bookkeepingBudget bounds one claim-bookkeeping round trip.
//
// Cancellation is dropped on purpose: Release and Settle speak for a frame
// whose delivery has already been decided, and the shutdown that interrupted
// the work must not also erase the record of it.
//
// A DEADLINE is not dropped. A caller that sets one is bounding a whole
// sequence of these calls — drainRemaining gives the entire drain a single
// DrainBudget — so a round trip that helped itself to a fresh budget past that
// point would spend time the shutdown already promised away. The deadline is
// inherited and the store's own budget only ever makes the wait shorter.
//
// Both halves reach the wire only because the client is built to let them:
// see the ContextTimeoutEnabled note in NewRedisDedupe.
func (d *redisDedupe) bookkeepingBudget(ctx context.Context) (context.Context, context.CancelFunc) {
	detached := context.WithoutCancel(ctx)
	own := time.Now().Add(d.budget)
	if deadline, ok := ctx.Deadline(); ok && deadline.Before(own) {
		return context.WithDeadline(detached, deadline)
	}
	return context.WithDeadline(detached, own)
}

// Release is a compare-and-delete on the token. An error means the outcome is
// unknown; the caller reads it as exactly that (RelayOutbound.perform).
func (d *redisDedupe) Release(ctx context.Context, key, token string) (bool, error) {
	ctx, cancel := d.bookkeepingBudget(ctx)
	defer cancel()
	n, err := redisRelease.Run(ctx, d.rdb, []string{key}, token).Int()
	if err != nil {
		d.log.WarnContext(ctx, "wecom relay: claim release outcome unknown",
			"error", err, "key", key)
		return false, err
	}
	return n == 1, nil
}

func (d *redisDedupe) Settle(ctx context.Context, key, token string) (bool, error) {
	ctx, cancel := d.bookkeepingBudget(ctx)
	defer cancel()
	n, err := redisSettle.Run(ctx, d.rdb, []string{key}, token, claimSettledValue).Int()
	return n == 1, err
}

func (d *redisDedupe) Resolve(ctx context.Context, key string) (claimState, error) {
	ctx, cancel := context.WithTimeout(ctx, d.budget)
	defer cancel()
	n, err := redisResolve.Run(ctx, d.rdb, []string{key}, claimSettledValue, claimLostValue).Int()
	if err != nil {
		return claimAbsent, err
	}
	return claimState(n), nil
}
