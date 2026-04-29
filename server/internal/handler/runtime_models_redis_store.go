package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis-backed implementation of ModelListStore.
//
// Storage layout (mirrors the local-skill Redis store pattern):
//
//   mul:model_list:<request_id>                → JSON-encoded ModelListRequest, TTL = retention
//   mul:model_list:pending:<runtime_id>        → ZSET { member = request_id, score = created_at UnixNano }
//                                                TTL = retention * 2, refreshed on Create
//
// PopPending uses the same atomic Lua claimPendingScript as the local-skill
// store (defined in runtime_local_skills_redis_store.go) — ZREM + SET in a
// single script so no request is stranded between the two writes.

const (
	modelListKeyPrefix     = "mul:model_list:"
	modelListPendingPrefix = "mul:model_list:pending:"
	modelListPopMaxRetries = 5
)

func modelListKey(id string) string              { return modelListKeyPrefix + id }
func modelListPendingKey(runtimeID string) string { return modelListPendingPrefix + runtimeID }

// RedisModelListStore stores pending / running / completed model-list requests
// in Redis so every API node agrees on the same state. This fixes the
// "discovery failed" bug where POST, GET-poll, and daemon-report land on
// different backend pods whose in-memory stores are disjoint.
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
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal model list request: %w", err)
	}

	pipe := s.rdb.TxPipeline()
	pipe.Set(ctx, modelListKey(req.ID), data, modelListStoreRetention)
	pipe.ZAdd(ctx, modelListPendingKey(runtimeID), redis.Z{
		Score:  float64(now.UnixNano()),
		Member: req.ID,
	})
	// Keep the pending ZSET alive longer than individual requests so stale
	// members can be swept lazily on PopPending.
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
// stored state has aged past the threshold, and persists the transition when
// applicable so sibling nodes observe the same terminal state.
func (s *RedisModelListStore) loadRequest(ctx context.Context, id string) (*ModelListRequest, error) {
	raw, err := s.rdb.Get(ctx, modelListKey(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get model list request: %w", err)
	}
	var req ModelListRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("decode model list request: %w", err)
	}
	if applyModelListTimeout(&req, time.Now()) {
		// Persist the timeout so subsequent Get / PopPending on any node see
		// the terminal state. Also drop the id from the pending zset.
		if err := s.persistRequest(ctx, &req); err != nil {
			return nil, err
		}
		s.rdb.ZRem(ctx, modelListPendingKey(req.RuntimeID), req.ID)
	}
	return &req, nil
}

func (s *RedisModelListStore) persistRequest(ctx context.Context, req *ModelListRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal model list request: %w", err)
	}
	if err := s.rdb.Set(ctx, modelListKey(req.ID), data, modelListStoreRetention).Err(); err != nil {
		return fmt.Errorf("persist model list request: %w", err)
	}
	return nil
}

func (s *RedisModelListStore) PopPending(ctx context.Context, runtimeID string) (*ModelListRequest, error) {
	pendingKey := modelListPendingKey(runtimeID)

	for attempt := 0; attempt < modelListPopMaxRetries; attempt++ {
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
		req.UpdatedAt = now
		data, err := json.Marshal(req)
		if err != nil {
			return nil, fmt.Errorf("marshal model list request: %w", err)
		}

		// Atomically claim: ZREM from pending + SET the updated record.
		// Uses the same Lua script as the local-skill store.
		result, err := claimPendingScript.Run(
			ctx, s.rdb,
			[]string{pendingKey, modelListKey(id)},
			id, data, int(modelListStoreRetention.Seconds()),
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
