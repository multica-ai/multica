package wecom

// dedupe_redis.go — the at-most-once claim behind a routed delivery.
//
// It lives on the same Redis the relay itself runs on, which is the whole
// reason this is not a new dependency: no Redis means no relay, which means no
// cross-replica routing and nothing to deduplicate. A deployment that never
// needed Redis is not asked for it now.
//
// SET NX PX is the claim. One key per turn, keyed on the same event id the
// stream entry carries, so a frame replayed after a restart and a frame read
// by two replicas mid-lease-move meet the same key.

import (
	"context"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// redisDedupe is a DedupeStore backed by one Redis key per delivery.
type redisDedupe struct {
	rdb *redis.Client
	log *slog.Logger
}

// NewRedisDedupe builds the production claim store. A nil client yields nil,
// which RelayOutbound reads as "no cross-restart claim" — correct only where
// there is no relay either.
func NewRedisDedupe(rdb *redis.Client, log *slog.Logger) DedupeStore {
	if rdb == nil {
		return nil
	}
	if log == nil {
		log = slog.Default()
	}
	return &redisDedupe{rdb: rdb, log: log}
}

// claimTimeout bounds the round trip. A dispatcher worker waiting on Redis is
// a worker not delivering, and the frame is replayable, so failing fast and
// letting the replay bring it back beats holding the queue.
const claimTimeout = 2 * time.Second

func (d *redisDedupe) Claim(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, claimTimeout)
	defer cancel()
	return d.rdb.SetNX(ctx, key, "1", ttl).Result()
}

// Held reports whether a claim is currently taken. One EXISTS, on the same
// bounded budget as the claim itself: the caller is deciding whether to record
// a lost reply, and a Redis that cannot answer must not become evidence.
func (d *redisDedupe) Held(ctx context.Context, key string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, claimTimeout)
	defer cancel()
	n, err := d.rdb.Exists(ctx, key).Result()
	return n > 0, err
}

// Release gives a claim back for a delivery that provably did not happen.
// Best effort by design: a Release that fails leaves a key that expires on its
// own, which costs one un-retried delivery in the replay window rather than a
// duplicate in somebody's chat.
func (d *redisDedupe) Release(ctx context.Context, key string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), claimTimeout)
	defer cancel()
	if err := d.rdb.Del(ctx, key).Err(); err != nil {
		d.log.WarnContext(ctx, "wecom relay: could not release a delivery claim",
			"error", err, "key", key)
	}
}
