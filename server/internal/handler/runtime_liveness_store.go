package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// LivenessStore tracks short-lived "this runtime heartbeated recently" records.
// It exists so the heartbeat hot path can write a TTL'd Redis key instead of
// rewriting agent_runtime.last_seen_at on every beat. The DB row is still the
// authority for state transitions and the fallback when the store is unavailable.
//
// The interface is deliberately small and side-effect-free on errors: callers
// that get an error from Touch or ok=false from IsAlive must fall back to the
// DB-only behavior (rewrite last_seen_at every beat; trust the SQL stale window
// in the sweeper). That keeps the system correct end-to-end whenever Redis is
// missing or unhealthy without any per-call configuration.
type LivenessStore interface {
	// Available reports whether the store is wired to a real backend. False
	// means callers should treat the DB as the only source of truth — the
	// other methods on a non-available store are no-ops.
	Available() bool

	// Touch records a fresh heartbeat for runtimeID with the given TTL.
	// Returns an error on backend failure; callers should fall back to a
	// DB heartbeat write on error.
	Touch(ctx context.Context, runtimeID string, ttl time.Duration) error

	// IsAliveBatch reports liveness for many runtime IDs at once. The
	// returned map covers every input ID (false for any not alive). ok=false
	// signals the backend errored or is unavailable; callers must fall back
	// to the DB stale window.
	IsAliveBatch(ctx context.Context, runtimeIDs []string) (alive map[string]bool, ok bool)

	// Forget drops the liveness record for runtimeID. Used on deregister
	// and after the sweeper confirms a runtime offline. Best-effort: errors
	// are logged but not returned, since the TTL will reap the key anyway.
	Forget(ctx context.Context, runtimeID string)
}

// noopLivenessStore is the default — used whenever no Redis client is wired in.
// All methods are no-ops; Available() returns false so callers know to use
// the DB path.
type noopLivenessStore struct{}

// NewNoopLivenessStore returns a LivenessStore that always reports unavailable.
// Callers should default to this and swap in a real store at wire time.
func NewNoopLivenessStore() LivenessStore { return noopLivenessStore{} }

func (noopLivenessStore) Available() bool { return false }

func (noopLivenessStore) Touch(_ context.Context, _ string, _ time.Duration) error {
	return nil
}

func (noopLivenessStore) IsAliveBatch(_ context.Context, _ []string) (map[string]bool, bool) {
	return nil, false
}

func (noopLivenessStore) Forget(_ context.Context, _ string) {}

// runtimeLivenessKeyPrefix is the Redis key prefix for runtime liveness
// records. Mirrors the namespacing used by the other runtime stores
// (mul:update:*, mul:model_list:*, mul:local_skill_list:*).
const runtimeLivenessKeyPrefix = "mul:runtime:hb:"

func runtimeLivenessKey(runtimeID string) string {
	return runtimeLivenessKeyPrefix + runtimeID
}

// RedisLivenessStore writes one TTL'd key per runtime heartbeat. The presence
// of an unexpired key is the signal "this runtime is alive right now"; the
// sweeper consults this before marking a stale-in-DB runtime offline.
type RedisLivenessStore struct {
	rdb *redis.Client
}

func NewRedisLivenessStore(rdb *redis.Client) *RedisLivenessStore {
	return &RedisLivenessStore{rdb: rdb}
}

func (s *RedisLivenessStore) Available() bool { return s != nil && s.rdb != nil }

func (s *RedisLivenessStore) Touch(ctx context.Context, runtimeID string, ttl time.Duration) error {
	if !s.Available() {
		return errors.New("redis liveness store: unavailable")
	}
	if runtimeID == "" {
		return errors.New("redis liveness store: empty runtime id")
	}
	if err := s.rdb.Set(ctx, runtimeLivenessKey(runtimeID), "1", ttl).Err(); err != nil {
		return fmt.Errorf("liveness touch: %w", err)
	}
	return nil
}

func (s *RedisLivenessStore) IsAliveBatch(ctx context.Context, runtimeIDs []string) (map[string]bool, bool) {
	if !s.Available() || len(runtimeIDs) == 0 {
		return map[string]bool{}, s.Available()
	}
	keys := make([]string, len(runtimeIDs))
	for i, id := range runtimeIDs {
		keys[i] = runtimeLivenessKey(id)
	}
	values, err := s.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		slog.Warn("liveness mget failed; falling back to DB",
			"error", err, "count", len(keys))
		return nil, false
	}
	out := make(map[string]bool, len(runtimeIDs))
	for i, id := range runtimeIDs {
		out[id] = values[i] != nil
	}
	return out, true
}

func (s *RedisLivenessStore) Forget(ctx context.Context, runtimeID string) {
	if !s.Available() || runtimeID == "" {
		return
	}
	if err := s.rdb.Del(ctx, runtimeLivenessKey(runtimeID)).Err(); err != nil {
		slog.Warn("liveness forget failed", "error", err, "runtime_id", runtimeID)
	}
}
