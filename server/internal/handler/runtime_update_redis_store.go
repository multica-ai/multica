package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis-backed implementation of UpdateStore.
//
// Storage layout (mirrors the model-list / local-skill Redis store patterns):
//
//   mul:update:<request_id>             → JSON-encoded UpdateRequest, TTL = retention
//   mul:update:pending:<runtime_id>     → ZSET { member = request_id, score = created_at UnixNano }
//                                         TTL = retention * 2, refreshed on Create
//   mul:update:active:<runtime_id>      → request_id of the currently pending or running
//                                         update for the runtime, TTL = retention. Used to
//                                         enforce the "one update at a time per runtime"
//                                         rule across nodes; deleted on terminal transitions.
//
// PopPending uses the same atomic claimPendingScript as the model-list and
// local-skill stores (defined in runtime_local_skills_redis_store.go) — ZREM
// the pending entry + SET the running record in a single Lua call so a
// transient error between the two cannot strand the request.

const (
	updateKeyPrefix     = "mul:update:"
	updatePendingPrefix = "mul:update:pending:"
	updateActivePrefix  = "mul:update:active:"
	updatePopMaxRetries = 5
)

func updateKey(id string) string                 { return updateKeyPrefix + id }
func updatePendingKey(runtimeID string) string   { return updatePendingPrefix + runtimeID }
func updateActiveKey(runtimeID string) string    { return updateActivePrefix + runtimeID }

// RedisUpdateStore stores pending / running / completed daemon CLI update
// requests in Redis so every API node agrees on the same state. Without this
// the frontend could create a request on Pod A while the daemon polls Pod B
// and never picks it up — see PR #51 for the equivalent fix on ModelListStore.
type RedisUpdateStore struct {
	rdb *redis.Client
}

func NewRedisUpdateStore(rdb *redis.Client) *RedisUpdateStore {
	return &RedisUpdateStore{rdb: rdb}
}

func (s *RedisUpdateStore) Create(ctx context.Context, runtimeID, targetVersion string) (*UpdateRequest, error) {
	// Best-effort enforcement of "one update at a time per runtime". Two nodes
	// can still race past this check (the in-memory store has the same race
	// across pods), but in practice updates are rare and operator-initiated,
	// so this is good enough.
	if existing, err := s.rdb.Get(ctx, updateActiveKey(runtimeID)).Result(); err == nil && existing != "" {
		req, err := s.loadRequest(ctx, existing)
		if err != nil {
			return nil, err
		}
		if req != nil && (req.Status == UpdatePending || req.Status == UpdateRunning) {
			return nil, errUpdateInProgress
		}
		// Stale active key (record gone or already in a terminal state) — fall
		// through; the SET below will overwrite it with the new request id.
	} else if err != nil && !errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("get active update: %w", err)
	}

	now := time.Now()
	req := &UpdateRequest{
		ID:            randomID(),
		RuntimeID:     runtimeID,
		Status:        UpdatePending,
		TargetVersion: targetVersion,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal update request: %w", err)
	}

	pipe := s.rdb.TxPipeline()
	pipe.Set(ctx, updateKey(req.ID), data, updateStoreRetention)
	pipe.Set(ctx, updateActiveKey(runtimeID), req.ID, updateStoreRetention)
	pipe.ZAdd(ctx, updatePendingKey(runtimeID), redis.Z{
		Score:  float64(now.UnixNano()),
		Member: req.ID,
	})
	// Keep the pending ZSET alive longer than individual requests so stale
	// members can be swept lazily on PopPending.
	pipe.Expire(ctx, updatePendingKey(runtimeID), updateStoreRetention*2)
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, fmt.Errorf("persist update request: %w", err)
	}
	return req, nil
}

func (s *RedisUpdateStore) Get(ctx context.Context, id string) (*UpdateRequest, error) {
	return s.loadRequest(ctx, id)
}

// loadRequest fetches a single record, applies the timeout transition if the
// stored state has aged past the threshold, and persists the transition when
// applicable so sibling nodes observe the same terminal state.
func (s *RedisUpdateStore) loadRequest(ctx context.Context, id string) (*UpdateRequest, error) {
	raw, err := s.rdb.Get(ctx, updateKey(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get update request: %w", err)
	}
	var req UpdateRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("decode update request: %w", err)
	}
	if applyUpdateTimeout(&req, time.Now()) {
		// Persist the timeout so subsequent Get / PopPending on any node see
		// the terminal state, drop the id from the pending zset, and clear
		// the active key if it still points at this id.
		if err := s.persistRequest(ctx, &req); err != nil {
			return nil, err
		}
		s.rdb.ZRem(ctx, updatePendingKey(req.RuntimeID), req.ID)
		s.clearActiveIfMatches(ctx, req.RuntimeID, req.ID)
	}
	return &req, nil
}

func (s *RedisUpdateStore) persistRequest(ctx context.Context, req *UpdateRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal update request: %w", err)
	}
	if err := s.rdb.Set(ctx, updateKey(req.ID), data, updateStoreRetention).Err(); err != nil {
		return fmt.Errorf("persist update request: %w", err)
	}
	return nil
}

// clearActiveIfMatches deletes the per-runtime active key only if it still
// references the given request id. Prevents a terminal transition from
// clobbering a newer update that has already taken the active slot.
var clearActiveIfMatchesScript = redis.NewScript(`
local current = redis.call('GET', KEYS[1])
if current == ARGV[1] then
    redis.call('DEL', KEYS[1])
    return 1
end
return 0
`)

func (s *RedisUpdateStore) clearActiveIfMatches(ctx context.Context, runtimeID, id string) {
	clearActiveIfMatchesScript.Run(ctx, s.rdb, []string{updateActiveKey(runtimeID)}, id)
}

func (s *RedisUpdateStore) PopPending(ctx context.Context, runtimeID string) (*UpdateRequest, error) {
	pendingKey := updatePendingKey(runtimeID)

	for attempt := 0; attempt < updatePopMaxRetries; attempt++ {
		ids, err := s.rdb.ZRange(ctx, pendingKey, 0, 0).Result()
		if err != nil {
			return nil, fmt.Errorf("zrange pending: %w", err)
		}
		if len(ids) == 0 {
			return nil, nil
		}
		id := ids[0]

		req, err := s.loadRequest(ctx, id)
		if err != nil {
			return nil, err
		}
		if req == nil {
			// Record expired but the zset still references it — drop and retry.
			s.rdb.ZRem(ctx, pendingKey, id)
			continue
		}
		if req.Status != UpdatePending {
			// Either the timeout fired inside loadRequest or another node
			// already picked it up. Unlink from the pending set and retry.
			s.rdb.ZRem(ctx, pendingKey, id)
			continue
		}

		now := time.Now()
		req.Status = UpdateRunning
		req.UpdatedAt = now
		data, err := json.Marshal(req)
		if err != nil {
			return nil, fmt.Errorf("marshal update request: %w", err)
		}

		// Atomically claim: ZREM from pending + SET the updated record.
		// Uses the same Lua script as the model-list / local-skill stores.
		result, err := claimPendingScript.Run(
			ctx, s.rdb,
			[]string{pendingKey, updateKey(id)},
			id, data, int(updateStoreRetention.Seconds()),
		).Int64()
		if err != nil {
			return nil, fmt.Errorf("claim pending: %w", err)
		}
		if result == 0 {
			// Another node won the race — retry.
			continue
		}
		return req, nil
	}
	return nil, nil
}

func (s *RedisUpdateStore) Complete(ctx context.Context, id string, output string) error {
	req, err := s.loadRequest(ctx, id)
	if err != nil {
		return err
	}
	if req == nil {
		return nil
	}
	req.Status = UpdateCompleted
	req.Output = output
	req.UpdatedAt = time.Now()
	if err := s.persistRequest(ctx, req); err != nil {
		return err
	}
	s.clearActiveIfMatches(ctx, req.RuntimeID, req.ID)
	return nil
}

func (s *RedisUpdateStore) Fail(ctx context.Context, id string, errMsg string) error {
	req, err := s.loadRequest(ctx, id)
	if err != nil {
		return err
	}
	if req == nil {
		return nil
	}
	req.Status = UpdateFailed
	req.Error = errMsg
	req.UpdatedAt = time.Now()
	if err := s.persistRequest(ctx, req); err != nil {
		return err
	}
	s.clearActiveIfMatches(ctx, req.RuntimeID, req.ID)
	return nil
}
