package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis-backed implementation of UpdateStore. CLI updates have the same
// pending-request shape as model-list and runtime-local-skill requests:
// frontend creates the request, daemon claims it on heartbeat, daemon reports
// a terminal result, and the UI polls by request ID. In multi-node deploys all
// four calls can hit different API replicas, so the lifecycle must live in
// shared storage.

const (
	updateKeyPrefix          = "mul:" + runtimePendingRedisHashTag + ":update:req:"
	updatePendingPrefix      = "mul:" + runtimePendingRedisHashTag + ":update:pending:"
	updateActivePrefix       = "mul:" + runtimePendingRedisHashTag + ":update:active:"
	updateRedisPopMaxRetries = 5
	updateRedisCASMaxRetries = 10
)

func updateKey(id string) string               { return updateKeyPrefix + id }
func updatePendingKey(runtimeID string) string { return updatePendingPrefix + runtimeID }
func updateActiveKey(machineKey string) string { return updateActivePrefix + machineKey }

var deleteIfValueScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
    return redis.call('DEL', KEYS[1])
end
return 0
`)

// createUpdateScript claims the machine slot and writes the request and its
// pending-queue entry in one step, so a slot never points at a request that
// is still being written. Lua rather than MULTI: some managed Redis
// deployments deny MULTI.
var createUpdateScript = redis.NewScript(`
if not redis.call('SET', KEYS[1], ARGV[1], 'NX', 'EX', tonumber(ARGV[3])) then
    return 0
end
redis.call('SET', KEYS[2], ARGV[2], 'EX', tonumber(ARGV[3]))
redis.call('ZADD', KEYS[3], ARGV[4], ARGV[1])
redis.call('EXPIRE', KEYS[3], tonumber(ARGV[3]))
return 1
`)

// compareAndSetScript replaces KEYS[1] only if it still holds the bytes the
// caller read. Every state change of an update record goes through it, so a
// cancel and a start racing on one request cannot both win.
var compareAndSetScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) ~= ARGV[1] then
    return 0
end
redis.call('SET', KEYS[1], ARGV[2], 'EX', tonumber(ARGV[3]))
return 1
`)

type RedisUpdateStore struct {
	rdb redis.UniversalClient
}

func NewRedisUpdateStore(rdb redis.UniversalClient) *RedisUpdateStore {
	return &RedisUpdateStore{rdb: rdb}
}

func (s *RedisUpdateStore) Create(ctx context.Context, runtimeID, machineKey, targetVersion, initiatorUserID string, queueWhenBusy bool) (*UpdateRequest, error) {
	if machineKey == "" {
		machineKey = "runtime:" + runtimeID
	}
	now := time.Now()
	req := &UpdateRequest{
		ID:              randomID(),
		RuntimeID:       runtimeID,
		InitiatorUserID: initiatorUserID,
		MachineKey:      machineKey,
		QueueWhenBusy:   queueWhenBusy,
		Status:          UpdatePending,
		TargetVersion:   targetVersion,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	data, err := s.marshalRequest(req)
	if err != nil {
		return nil, err
	}

	activeKey := updateActiveKey(machineKey)
	for attempt := 0; attempt < 2; attempt++ {
		created, err := createUpdateScript.Run(ctx, s.rdb,
			[]string{activeKey, updateKey(req.ID), updatePendingKey(runtimeID)},
			req.ID, data, int(updateWaitingRetention.Seconds()), now.UnixNano(),
		).Int64()
		if err != nil {
			return nil, fmt.Errorf("persist update request: %w", err)
		}
		if created == 1 {
			return req, nil
		}
		// Creation is atomic, so a slot whose request is finished or gone is
		// stale (e.g. a lost slot release) and can be taken over.
		holder, err := s.rdb.Get(ctx, activeKey).Result()
		if errors.Is(err, redis.Nil) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("reserve active update: %w", err)
		}
		existing, err := s.Get(ctx, holder)
		if err != nil {
			return nil, err
		}
		if existing != nil && !updateRequestTerminal(existing.Status) {
			return nil, errUpdateInProgress
		}
		if err := s.clearActiveIfMatches(ctx, machineKey, holder); err != nil {
			return nil, err
		}
	}
	return nil, errUpdateInProgress
}

// Get loads a request, persisting a timeout transition if one is due.
func (s *RedisUpdateStore) Get(ctx context.Context, id string) (*UpdateRequest, error) {
	return s.Transition(ctx, id, nil)
}

func (s *RedisUpdateStore) GetActive(ctx context.Context, machineKey string) (*UpdateRequest, error) {
	id, err := s.rdb.Get(ctx, updateActiveKey(machineKey)).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get active update: %w", err)
	}
	req, err := s.Get(ctx, id)
	if err != nil || req == nil || updateRequestTerminal(req.Status) {
		return nil, err
	}
	return req, nil
}

// Transition applies fn (and any due timeout) to the stored request with a
// compare-and-set, retrying when another writer got there first. A nil fn
// only applies the timeout. Once the request is terminal its machine slot
// and pending-queue entry are released.
func (s *RedisUpdateStore) Transition(ctx context.Context, id string, fn updateTransition) (*UpdateRequest, error) {
	for attempt := 0; attempt < updateRedisCASMaxRetries; attempt++ {
		raw, err := s.rdb.Get(ctx, updateKey(id)).Bytes()
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("get update request: %w", err)
		}
		req, err := s.unmarshalRequest(raw)
		if err != nil {
			return nil, err
		}
		now := time.Now()
		changed := applyUpdateTimeout(req, now)
		var fnErr error
		if fn != nil {
			if fnErr = fn(req, now); fnErr == nil {
				changed = true
			}
		}
		if !changed {
			return req, fnErr
		}
		data, err := s.marshalRequest(req)
		if err != nil {
			return nil, err
		}
		swapped, err := compareAndSetScript.Run(ctx, s.rdb, []string{updateKey(id)},
			raw, data, int(updateRetention(req).Seconds())).Int64()
		if err != nil {
			return nil, fmt.Errorf("persist update request: %w", err)
		}
		if swapped == 0 {
			continue
		}
		if !updateRequestWaiting(req.Status) {
			s.rdb.ZRem(ctx, updatePendingKey(req.RuntimeID), req.ID)
		}
		if updateRequestTerminal(req.Status) {
			if err := s.clearActiveIfMatches(ctx, req.MachineKey, req.ID); err != nil {
				return nil, err
			}
		}
		return req, fnErr
	}
	return nil, fmt.Errorf("update request %s: too much write contention", id)
}

func (s *RedisUpdateStore) Complete(ctx context.Context, id string, output string) error {
	_, err := s.Transition(ctx, id, finishUpdate(UpdateCompleted, output, ""))
	return ignoreWithdrawn(err)
}

func (s *RedisUpdateStore) Fail(ctx context.Context, id string, errMsg string) error {
	_, err := s.Transition(ctx, id, finishUpdate(UpdateFailed, "", errMsg))
	return ignoreWithdrawn(err)
}

type redisUpdateEnvelope struct {
	Public          *UpdateRequest `json:"r"`
	RunStartedAt    *time.Time     `json:"s,omitempty"`
	InitiatorUserID string         `json:"u,omitempty"`
	MachineKey      string         `json:"m,omitempty"`
	QueueWhenBusy   bool           `json:"q,omitempty"`
}

func (s *RedisUpdateStore) marshalRequest(req *UpdateRequest) ([]byte, error) {
	env := redisUpdateEnvelope{
		Public:          req,
		RunStartedAt:    req.RunStartedAt,
		InitiatorUserID: req.InitiatorUserID,
		MachineKey:      req.MachineKey,
		QueueWhenBusy:   req.QueueWhenBusy,
	}
	data, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("marshal update request: %w", err)
	}
	return data, nil
}

func (s *RedisUpdateStore) unmarshalRequest(raw []byte) (*UpdateRequest, error) {
	var env redisUpdateEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("decode update request: %w", err)
	}
	if env.Public == nil {
		return nil, fmt.Errorf("decode update request: missing payload")
	}
	env.Public.RunStartedAt = env.RunStartedAt
	env.Public.InitiatorUserID = env.InitiatorUserID
	env.Public.MachineKey = env.MachineKey
	env.Public.QueueWhenBusy = env.QueueWhenBusy
	return env.Public, nil
}

func (s *RedisUpdateStore) HasPending(ctx context.Context, runtimeID string) (bool, error) {
	cnt, err := s.rdb.ZCard(ctx, updatePendingKey(runtimeID)).Result()
	if err != nil {
		return false, fmt.Errorf("zcard pending updates: %w", err)
	}
	return cnt > 0, nil
}

func (s *RedisUpdateStore) PopPending(ctx context.Context, runtimeID string, daemonCanQueue bool) (*UpdateRequest, error) {
	pendingKey := updatePendingKey(runtimeID)

	for attempt := 0; attempt < updateRedisPopMaxRetries; attempt++ {
		ids, err := s.rdb.ZRange(ctx, pendingKey, 0, 0).Result()
		if err != nil {
			return nil, fmt.Errorf("zrange pending updates: %w", err)
		}
		if len(ids) == 0 {
			return nil, nil
		}
		id := ids[0]

		req, err := s.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if req == nil || !updateRequestWaiting(req.Status) {
			s.rdb.ZRem(ctx, pendingKey, id)
			continue
		}
		if offerDeferrable(req, daemonCanQueue) {
			// Stays queued and is offered again on the next heartbeat.
			return req, nil
		}

		// Offered once: running from delivery. The CAS loses to a concurrent
		// cancel or to another heartbeat that already claimed it.
		claimed, err := s.Transition(ctx, id, func(r *UpdateRequest, now time.Time) error {
			if !updateRequestWaiting(r.Status) {
				return errUpdateWithdrawn
			}
			return startUpdate(r, now)
		})
		if errors.Is(err, errUpdateWithdrawn) || claimed == nil {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("claim pending update: %w", err)
		}
		return claimed, nil
	}
	return nil, nil
}

func (s *RedisUpdateStore) clearActiveIfMatches(ctx context.Context, machineKey, id string) error {
	if machineKey == "" || id == "" {
		return nil
	}
	if err := deleteIfValueScript.Run(ctx, s.rdb, []string{updateActiveKey(machineKey)}, id).Err(); err != nil {
		return fmt.Errorf("clear active update: %w", err)
	}
	return nil
}
