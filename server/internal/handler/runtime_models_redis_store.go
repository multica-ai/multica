package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis-backed implementation of ModelListStore. The wire layout matches
// runtime_local_skills_redis_store.go (which solves the same multi-node
// dispatch problem for skill lists/imports) so the operational story is
// identical: namespaced keys, ZSET-backed pending queue, atomic claim via
// the shared Lua script.
//
// Key layout:
//
//   mul:model_list:req:<request_id>           → JSON-encoded ModelListRequest, TTL = retention
//   mul:model_list:pending:<runtime_id>       → ZSET { member = request_id, score = created_at UnixNano }
//                                                TTL = retention*2 (kept alive long enough for
//                                                lazy sweep on PopPending)
//
// PopPending uses claimPendingScript (defined in
// runtime_local_skills_redis_store.go) to atomically ZREM the pending entry
// and SET the record to "running" — splitting those two writes would strand
// requests on a transient Redis hiccup between them.

const (
	// Namespaced under mul:model_list:* so the key set doesn't collide with
	// the realtime relay (ws:*) or the local-skill stores (mul:local_skill:*).
	modelListKeyPrefix          = "mul:model_list:req:"
	modelListPendingPrefix      = "mul:model_list:pending:"
	modelListRedisPopMaxRetries = 5
)

func modelListKey(id string) string               { return modelListKeyPrefix + id }
func modelListPendingKey(runtimeID string) string { return modelListPendingPrefix + runtimeID }

// RedisModelListStore stores model list requests in Redis so every API node
// agrees on the same pending / running / terminal state.
type RedisModelListStore struct {
	rdb *redis.Client
}

func NewRedisModelListStore(rdb *redis.Client) *RedisModelListStore {
	return &RedisModelListStore{rdb: rdb}
}

func (s *RedisModelListStore) Create(ctx context.Context, runtimeID string) (*ModelListRequest, error) {
	now := time.Now()
	req := &ModelListRequest{
		ID:        randomID(),
		RuntimeID: runtimeID,
		Status:    ModelListPending,
		Supported: true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	data, err := s.marshalRequest(req)
	if err != nil {
		return nil, err
	}

	pipe := s.rdb.TxPipeline()
	pipe.Set(ctx, modelListKey(req.ID), data, modelListStoreRetention)
	pipe.ZAdd(ctx, modelListPendingKey(runtimeID), redis.Z{
		Score:  float64(now.UnixNano()),
		Member: req.ID,
	})
	// Keep the pending zset alive past the per-record retention so stale
	// members can be lazily swept on PopPending.
	pipe.Expire(ctx, modelListPendingKey(runtimeID), modelListStoreRetention*2)
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, fmt.Errorf("persist model list request: %w", err)
	}
	return req, nil
}

func (s *RedisModelListStore) Get(ctx context.Context, id string) (*ModelListRequest, error) {
	return s.loadRequest(ctx, id)
}

// loadRequest fetches a single record, applies timeout transitions if the
// stored state has aged past the threshold, and persists the transition so
// sibling nodes observe the same terminal state.
func (s *RedisModelListStore) loadRequest(ctx context.Context, id string) (*ModelListRequest, error) {
	raw, err := s.rdb.Get(ctx, modelListKey(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get model list request: %w", err)
	}
	req, err := s.unmarshalRequest(raw)
	if err != nil {
		return nil, err
	}
	if applyModelListTimeout(req, time.Now()) {
		if err := s.persistRequest(ctx, req); err != nil {
			return nil, err
		}
		// Drop from pending zset on terminal transition. PopPending would
		// also do this, but doing it here keeps the set clean for readers
		// that never call PopPending.
		s.rdb.ZRem(ctx, modelListPendingKey(req.RuntimeID), req.ID)
	}
	return req, nil
}

func (s *RedisModelListStore) persistRequest(ctx context.Context, req *ModelListRequest) error {
	data, err := s.marshalRequest(req)
	if err != nil {
		return err
	}
	if err := s.rdb.Set(ctx, modelListKey(req.ID), data, modelListStoreRetention).Err(); err != nil {
		return fmt.Errorf("persist model list request: %w", err)
	}
	return nil
}

// ModelListRequest tags RunStartedAt as `json:"-"` so the server-side
// bookkeeping field doesn't leak into the HTTP response (the UI only
// needs Status / UpdatedAt to drive its polling loop). Redis persistence
// has to keep that field, otherwise the running-timeout escape hatch
// silently breaks across nodes — every reader sees RunStartedAt=nil and
// applyModelListTimeout's running branch becomes a no-op. Wrap in an
// internal envelope that re-promotes the field on the wire.
type redisModelListEnvelope struct {
	Public       *ModelListRequest `json:"r"`
	RunStartedAt *time.Time        `json:"s,omitempty"`
}

func (s *RedisModelListStore) marshalRequest(req *ModelListRequest) ([]byte, error) {
	env := redisModelListEnvelope{Public: req, RunStartedAt: req.RunStartedAt}
	data, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("marshal model list request: %w", err)
	}
	return data, nil
}

func (s *RedisModelListStore) unmarshalRequest(raw []byte) (*ModelListRequest, error) {
	var env redisModelListEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("decode model list request: %w", err)
	}
	if env.Public == nil {
		return nil, fmt.Errorf("decode model list request: missing payload")
	}
	env.Public.RunStartedAt = env.RunStartedAt
	return env.Public, nil
}

// HasPending is a cheap read-only ZCARD probe used by the heartbeat hot path
// to decide whether to invoke the side-effecting PopPending.
func (s *RedisModelListStore) HasPending(ctx context.Context, runtimeID string) (bool, error) {
	cnt, err := s.rdb.ZCard(ctx, modelListPendingKey(runtimeID)).Result()
	if err != nil {
		return false, fmt.Errorf("zcard pending: %w", err)
	}
	return cnt > 0, nil
}

func (s *RedisModelListStore) PopPending(ctx context.Context, runtimeID string) (*ModelListRequest, error) {
	pendingKey := modelListPendingKey(runtimeID)

	for attempt := 0; attempt < modelListRedisPopMaxRetries; attempt++ {
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
		if req.Status != ModelListPending {
			// Either the timeout fired inside loadRequest or another node
			// already picked it up. Unlink from the pending set and retry.
			s.rdb.ZRem(ctx, pendingKey, id)
			continue
		}

		now := time.Now()
		req.Status = ModelListRunning
		req.RunStartedAt = &now
		req.UpdatedAt = now
		data, err := s.marshalRequest(req)
		if err != nil {
			return nil, err
		}

		result, err := claimPendingScript.Run(
			ctx, s.rdb,
			[]string{pendingKey, modelListKey(id)},
			id, data, int(modelListStoreRetention.Seconds()),
		).Int64()
		if err != nil {
			return nil, fmt.Errorf("claim pending: %w", err)
		}
		if result == 0 {
			// Another node won the race. The record is owned by the winner;
			// retry to pick up whatever else is queued (or nothing).
			continue
		}
		return req, nil
	}
	return nil, nil
}

func (s *RedisModelListStore) Complete(ctx context.Context, id string, models []ModelEntry, supported bool) error {
	req, err := s.loadRequest(ctx, id)
	if err != nil {
		return err
	}
	if req == nil {
		return nil
	}
	req.Status = ModelListCompleted
	req.Models = models
	req.Supported = supported
	req.UpdatedAt = time.Now()
	return s.persistRequest(ctx, req)
}

func (s *RedisModelListStore) Fail(ctx context.Context, id string, errMsg string) error {
	req, err := s.loadRequest(ctx, id)
	if err != nil {
		return err
	}
	if req == nil {
		return nil
	}
	req.Status = ModelListFailed
	req.Error = errMsg
	req.UpdatedAt = time.Now()
	return s.persistRequest(ctx, req)
}
