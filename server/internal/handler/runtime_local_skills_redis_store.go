package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis-backed implementations of LocalSkillListStore / LocalSkillImportStore.
//
// Storage layout (for both list and import flows, differing only in key prefix):
//
//   <prefix>:<request_id>                 → JSON-encoded request, TTL = retention
//   <prefix>:pending:<runtime_id>         → ZSET { member = request_id, score = created_at UnixNano }
//                                           TTL = retention, refreshed on Create
//
// PopPending is the critical multi-node primitive. It MUST atomically:
//  1. pick the oldest pending request id for this runtime
//  2. claim it (remove from the pending zset) AND transition its record to
//     "running" in a single step — otherwise a crash / transient Redis error
//     between the two writes strands the request (no longer pending, record
//     still says pending; no node will ever re-dispatch it).
//
// Doing this as two round-trips is racy; we use a Lua script so Redis runs
// ZREM + SET atomically server-side. If ZREM returns 0 (another node already
// claimed it), the SET is skipped. This is the fix for the PR-1557 review
// finding about the "request disappears under Redis hiccups" path.

const (
	// Namespaced so we don't collide with the realtime relay's ws:* keys.
	localSkillListKeyPrefix       = "mul:local_skill:list:"
	localSkillListPendingPrefix   = "mul:local_skill:list:pending:"
	localSkillImportKeyPrefix     = "mul:local_skill:import:"
	localSkillImportPendingPrefix = "mul:local_skill:import:pending:"
	localSkillRedisPopMaxRetries  = 5
)

// claimPendingScript atomically claims a pending request:
//
//	KEYS[1] = pending zset    ARGV[1] = request id to claim
//	KEYS[2] = record key       ARGV[2] = new record JSON (status=running)
//	                           ARGV[3] = record TTL in seconds
//
// Returns 1 when this caller won the claim (zset entry removed, record
// updated), 0 when the entry was already gone (another node won).
// Either the ZREM and the SET both happen or neither does — Redis executes
// a Lua script as a single atomic unit.
var claimPendingScript = redis.NewScript(`
local removed = redis.call('ZREM', KEYS[1], ARGV[1])
if removed == 0 then
    return 0
end
redis.call('SET', KEYS[2], ARGV[2], 'EX', tonumber(ARGV[3]))
return 1
`)

func localSkillListKey(id string) string { return localSkillListKeyPrefix + id }
func localSkillListPendingKey(runtimeID string) string {
	return localSkillListPendingPrefix + runtimeID
}
func localSkillImportKey(id string) string { return localSkillImportKeyPrefix + id }
func localSkillImportPendingKey(runtimeID string) string {
	return localSkillImportPendingPrefix + runtimeID
}

// RedisLocalSkillListStore stores pending / running / completed list requests
// in Redis so every API node agrees on the same state.
type RedisLocalSkillListStore struct {
	rdb *redis.Client
}

func NewRedisLocalSkillListStore(rdb *redis.Client) *RedisLocalSkillListStore {
	return &RedisLocalSkillListStore{rdb: rdb}
}

func (s *RedisLocalSkillListStore) Create(ctx context.Context, runtimeID string) (*RuntimeLocalSkillListRequest, error) {
	now := time.Now()
	req := &RuntimeLocalSkillListRequest{
		ID:        randomID(),
		RuntimeID: runtimeID,
		Status:    RuntimeLocalSkillPending,
		Supported: true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal list request: %w", err)
	}

	pipe := s.rdb.TxPipeline()
	pipe.Set(ctx, localSkillListKey(req.ID), data, runtimeLocalSkillStoreRetention)
	pipe.ZAdd(ctx, localSkillListPendingKey(runtimeID), redis.Z{
		Score:  float64(now.UnixNano()),
		Member: req.ID,
	})
	// Keep the pending ZSET alive a bit longer than the individual request
	// so stale members still in the zset can be swept lazily on PopPending
	// without blocking the create path on deletion.
	pipe.Expire(ctx, localSkillListPendingKey(runtimeID), runtimeLocalSkillStoreRetention*2)
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, fmt.Errorf("persist list request: %w", err)
	}
	return req, nil
}

func (s *RedisLocalSkillListStore) Get(ctx context.Context, id string) (*RuntimeLocalSkillListRequest, error) {
	return s.loadListRequest(ctx, id)
}

// loadListRequest fetches a single record, applies timeout transitions if the
// stored state has aged past the threshold, and persists the transition when
// applicable so sibling nodes observe the same terminal state.
func (s *RedisLocalSkillListStore) loadListRequest(ctx context.Context, id string) (*RuntimeLocalSkillListRequest, error) {
	raw, err := s.rdb.Get(ctx, localSkillListKey(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get list request: %w", err)
	}
	var req RuntimeLocalSkillListRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("decode list request: %w", err)
	}
	if applyLocalSkillListTimeout(&req, time.Now()) {
		// Persist the timeout so subsequent Get / PopPending on any node see
		// the terminal state. Also drop the id from the pending zset —
		// PopPending would do this itself, but doing it here keeps the set
		// clean even for readers that never call PopPending.
		if err := s.persistListRequest(ctx, &req); err != nil {
			return nil, err
		}
		s.rdb.ZRem(ctx, localSkillListPendingKey(req.RuntimeID), req.ID)
	}
	return &req, nil
}

func (s *RedisLocalSkillListStore) persistListRequest(ctx context.Context, req *RuntimeLocalSkillListRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal list request: %w", err)
	}
	if err := s.rdb.Set(ctx, localSkillListKey(req.ID), data, runtimeLocalSkillStoreRetention).Err(); err != nil {
		return fmt.Errorf("persist list request: %w", err)
	}
	return nil
}

// HasPending is a cheap read-only probe (ZCARD) used by hot paths to decide
// whether to invoke the side-effecting PopPending. It does NOT sweep
// expired / already-claimed entries — a spurious "true" is fine because the
// follow-up PopPending still handles the race correctly.
func (s *RedisLocalSkillListStore) HasPending(ctx context.Context, runtimeID string) (bool, error) {
	cnt, err := s.rdb.ZCard(ctx, localSkillListPendingKey(runtimeID)).Result()
	if err != nil {
		return false, fmt.Errorf("zcard pending: %w", err)
	}
	return cnt > 0, nil
}

func (s *RedisLocalSkillListStore) PopPending(ctx context.Context, runtimeID string) (*RuntimeLocalSkillListRequest, error) {
	pendingKey := localSkillListPendingKey(runtimeID)

	for attempt := 0; attempt < localSkillRedisPopMaxRetries; attempt++ {
		ids, err := s.rdb.ZRange(ctx, pendingKey, 0, 0).Result()
		if err != nil {
			return nil, fmt.Errorf("zrange pending: %w", err)
		}
		if len(ids) == 0 {
			return nil, nil
		}
		id := ids[0]

		req, err := s.loadListRequest(ctx, id)
		if err != nil {
			return nil, err
		}
		if req == nil {
			// Record expired but the zset still references it — drop and retry.
			s.rdb.ZRem(ctx, pendingKey, id)
			continue
		}
		if req.Status != RuntimeLocalSkillPending {
			// Either the timeout fired inside loadListRequest or another node
			// already picked it up. Either way, unlink from the pending set
			// and move on to the next one.
			s.rdb.ZRem(ctx, pendingKey, id)
			continue
		}

		now := time.Now()
		req.Status = RuntimeLocalSkillRunning
		req.RunStartedAt = &now
		req.UpdatedAt = now
		data, err := json.Marshal(req)
		if err != nil {
			return nil, fmt.Errorf("marshal list request: %w", err)
		}

		result, err := claimPendingScript.Run(
			ctx, s.rdb,
			[]string{pendingKey, localSkillListKey(id)},
			id, data, int(runtimeLocalSkillStoreRetention.Seconds()),
		).Int64()
		if err != nil {
			return nil, fmt.Errorf("claim pending: %w", err)
		}
		if result == 0 {
			// Another node won the race. The record still says pending and is
			// owned by the winner; we just retry to pick up whatever else is
			// queued (or nothing).
			continue
		}
		return req, nil
	}
	return nil, nil
}

func (s *RedisLocalSkillListStore) Complete(ctx context.Context, id string, skills []RuntimeLocalSkillSummary, supported bool) error {
	req, err := s.loadListRequest(ctx, id)
	if err != nil {
		return err
	}
	if req == nil {
		return nil
	}
	req.Status = RuntimeLocalSkillCompleted
	req.Skills = skills
	req.Supported = supported
	req.UpdatedAt = time.Now()
	return s.persistListRequest(ctx, req)
}

func (s *RedisLocalSkillListStore) Fail(ctx context.Context, id string, errMsg string) error {
	req, err := s.loadListRequest(ctx, id)
	if err != nil {
		return err
	}
	if req == nil {
		return nil
	}
	req.Status = RuntimeLocalSkillFailed
	req.Error = errMsg
	req.UpdatedAt = time.Now()
	return s.persistListRequest(ctx, req)
}

// RedisLocalSkillImportStore mirrors RedisLocalSkillListStore for import
// requests. Kept as a separate type (rather than a generic) because the
// request shape carries import-specific fields (skill_key, optional rename,
// creator id) and Go generics don't buy us much for two concrete impls.
type RedisLocalSkillImportStore struct {
	rdb *redis.Client
}

func NewRedisLocalSkillImportStore(rdb *redis.Client) *RedisLocalSkillImportStore {
	return &RedisLocalSkillImportStore{rdb: rdb}
}

func (s *RedisLocalSkillImportStore) Create(ctx context.Context, runtimeID, creatorID, skillKey string, name, description *string) (*RuntimeLocalSkillImportRequest, error) {
	now := time.Now()
	req := &RuntimeLocalSkillImportRequest{
		ID:          randomID(),
		RuntimeID:   runtimeID,
		SkillKey:    skillKey,
		Name:        name,
		Description: description,
		Status:      RuntimeLocalSkillPending,
		CreatedAt:   now,
		UpdatedAt:   now,
		CreatorID:   creatorID,
	}
	data, err := s.marshalImport(req)
	if err != nil {
		return nil, err
	}

	pipe := s.rdb.TxPipeline()
	pipe.Set(ctx, localSkillImportKey(req.ID), data, runtimeLocalSkillStoreRetention)
	pipe.ZAdd(ctx, localSkillImportPendingKey(runtimeID), redis.Z{
		Score:  float64(now.UnixNano()),
		Member: req.ID,
	})
	pipe.Expire(ctx, localSkillImportPendingKey(runtimeID), runtimeLocalSkillStoreRetention*2)
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, fmt.Errorf("persist import request: %w", err)
	}
	return req, nil
}

func (s *RedisLocalSkillImportStore) Get(ctx context.Context, id string) (*RuntimeLocalSkillImportRequest, error) {
	return s.loadImportRequest(ctx, id)
}

func (s *RedisLocalSkillImportStore) loadImportRequest(ctx context.Context, id string) (*RuntimeLocalSkillImportRequest, error) {
	raw, err := s.rdb.Get(ctx, localSkillImportKey(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get import request: %w", err)
	}
	req, err := s.unmarshalImport(raw)
	if err != nil {
		return nil, err
	}
	if applyLocalSkillImportTimeout(req, time.Now()) {
		if err := s.persistImportRequest(ctx, req); err != nil {
			return nil, err
		}
		s.rdb.ZRem(ctx, localSkillImportPendingKey(req.RuntimeID), req.ID)
	}
	return req, nil
}

func (s *RedisLocalSkillImportStore) persistImportRequest(ctx context.Context, req *RuntimeLocalSkillImportRequest) error {
	data, err := s.marshalImport(req)
	if err != nil {
		return err
	}
	if err := s.rdb.Set(ctx, localSkillImportKey(req.ID), data, runtimeLocalSkillStoreRetention).Err(); err != nil {
		return fmt.Errorf("persist import request: %w", err)
	}
	return nil
}

// The RuntimeLocalSkillImportRequest type marks CreatorID / RunStartedAt as
// `json:"-"` so those fields survive HTTP responses without leaking state.
// For Redis persistence we need those fields, so we wrap in an internal
// envelope that re-promotes them.
type redisImportEnvelope struct {
	Public       *RuntimeLocalSkillImportRequest `json:"r"`
	CreatorID    string                          `json:"c"`
	RunStartedAt *time.Time                      `json:"s"`
}

func (s *RedisLocalSkillImportStore) marshalImport(req *RuntimeLocalSkillImportRequest) ([]byte, error) {
	env := redisImportEnvelope{
		Public:       req,
		CreatorID:    req.CreatorID,
		RunStartedAt: req.RunStartedAt,
	}
	data, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("marshal import request: %w", err)
	}
	return data, nil
}

func (s *RedisLocalSkillImportStore) unmarshalImport(raw []byte) (*RuntimeLocalSkillImportRequest, error) {
	var env redisImportEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("decode import request: %w", err)
	}
	if env.Public == nil {
		return nil, fmt.Errorf("decode import request: missing payload")
	}
	env.Public.CreatorID = env.CreatorID
	env.Public.RunStartedAt = env.RunStartedAt
	return env.Public, nil
}

// HasPending mirrors RedisLocalSkillListStore.HasPending — cheap ZCARD probe
// for hot-path gating.
func (s *RedisLocalSkillImportStore) HasPending(ctx context.Context, runtimeID string) (bool, error) {
	cnt, err := s.rdb.ZCard(ctx, localSkillImportPendingKey(runtimeID)).Result()
	if err != nil {
		return false, fmt.Errorf("zcard pending: %w", err)
	}
	return cnt > 0, nil
}

func (s *RedisLocalSkillImportStore) PopPending(ctx context.Context, runtimeID string) (*RuntimeLocalSkillImportRequest, error) {
	pendingKey := localSkillImportPendingKey(runtimeID)

	for attempt := 0; attempt < localSkillRedisPopMaxRetries; attempt++ {
		ids, err := s.rdb.ZRange(ctx, pendingKey, 0, 0).Result()
		if err != nil {
			return nil, fmt.Errorf("zrange pending: %w", err)
		}
		if len(ids) == 0 {
			return nil, nil
		}
		id := ids[0]

		req, err := s.loadImportRequest(ctx, id)
		if err != nil {
			return nil, err
		}
		if req == nil {
			s.rdb.ZRem(ctx, pendingKey, id)
			continue
		}
		if req.Status != RuntimeLocalSkillPending {
			s.rdb.ZRem(ctx, pendingKey, id)
			continue
		}

		now := time.Now()
		req.Status = RuntimeLocalSkillRunning
		req.RunStartedAt = &now
		req.UpdatedAt = now
		data, err := s.marshalImport(req)
		if err != nil {
			return nil, err
		}

		result, err := claimPendingScript.Run(
			ctx, s.rdb,
			[]string{pendingKey, localSkillImportKey(id)},
			id, data, int(runtimeLocalSkillStoreRetention.Seconds()),
		).Int64()
		if err != nil {
			return nil, fmt.Errorf("claim pending: %w", err)
		}
		if result == 0 {
			continue
		}
		return req, nil
	}
	return nil, nil
}

func (s *RedisLocalSkillImportStore) Complete(ctx context.Context, id string, skill SkillResponse) error {
	req, err := s.loadImportRequest(ctx, id)
	if err != nil {
		return err
	}
	if req == nil {
		return nil
	}
	req.Status = RuntimeLocalSkillCompleted
	req.Skill = &skill
	req.UpdatedAt = time.Now()
	return s.persistImportRequest(ctx, req)
}

func (s *RedisLocalSkillImportStore) Fail(ctx context.Context, id string, errMsg string) error {
	req, err := s.loadImportRequest(ctx, id)
	if err != nil {
		return err
	}
	if req == nil {
		return nil
	}
	req.Status = RuntimeLocalSkillFailed
	req.Error = errMsg
	req.UpdatedAt = time.Now()
	return s.persistImportRequest(ctx, req)
}
