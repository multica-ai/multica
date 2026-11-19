package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis-backed implementation of UpdateStore. CLI updates have the same
// pending-request shape as model-list and runtime-local-skill requests:
// frontend creates the request, daemon claims it on heartbeat, daemon reports
// a terminal result, and the UI polls by request ID. In multi-node deploys all
// four calls can hit different API replicas, so the lifecycle must live in
// shared storage.
//
// Keys:
//   - req:<id>        the request envelope; every status change is a
//     compare-and-set on the stored bytes, so a cancel and a daemon starting
//     the update cannot both win.
//   - pending:<rt>    sorted set of the runtime's unstarted requests, scored by
//     the earliest time (UnixNano) they may be offered to the daemon again.
//   - active:<rt>     the runtime's one unfinished request.

const (
	updateKeyPrefix          = "mul:" + runtimePendingRedisHashTag + ":update:req:"
	updatePendingPrefix      = "mul:" + runtimePendingRedisHashTag + ":update:pending:"
	updateActivePrefix       = "mul:" + runtimePendingRedisHashTag + ":update:active:"
	updateRedisPopMaxRetries = 5
	updateRedisCASMaxRetries = 8
)

func updateKey(id string) string               { return updateKeyPrefix + id }
func updatePendingKey(runtimeID string) string { return updatePendingPrefix + runtimeID }
func updateActiveKey(runtimeID string) string  { return updateActivePrefix + runtimeID }

// updateRequestTTL keeps a queued request for its whole wait plus the read
// window after it ends; everything else only needs the read window.
func updateRequestTTL(status UpdateStatus) time.Duration {
	if status == UpdateScheduled {
		return updateScheduledTimeout + updateStoreRetention
	}
	return updateStoreRetention
}

var errUpdateStoreContention = errors.New("update request changed concurrently; retry")

var deleteIfValueScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
    return redis.call('DEL', KEYS[1])
end
return 0
`)

// compareAndSetScript writes ARGV[2] only when the key still holds ARGV[1].
var compareAndSetScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) ~= ARGV[1] then
    return 0
end
redis.call('SET', KEYS[1], ARGV[2], 'PX', tonumber(ARGV[3]))
return 1
`)

// claimOfferScript claims a due request for one delivery by pushing its next
// offer time forward. Returns 0 when another replica claimed it first.
var claimOfferScript = redis.NewScript(`
local score = redis.call('ZSCORE', KEYS[1], ARGV[1])
if not score or tonumber(score) > tonumber(ARGV[2]) then
    return 0
end
redis.call('ZADD', KEYS[1], ARGV[3], ARGV[1])
return 1
`)

type RedisUpdateStore struct {
	rdb redis.UniversalClient
}

func NewRedisUpdateStore(rdb redis.UniversalClient) *RedisUpdateStore {
	return &RedisUpdateStore{rdb: rdb}
}

func (s *RedisUpdateStore) Create(ctx context.Context, runtimeID, targetVersion, initiatorUserID string) (*UpdateRequest, error) {
	now := time.Now()
	req := &UpdateRequest{
		ID:              randomID(),
		RuntimeID:       runtimeID,
		InitiatorUserID: initiatorUserID,
		Status:          UpdatePending,
		TargetVersion:   targetVersion,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	data, err := s.marshalRequest(req)
	if err != nil {
		return nil, err
	}

	if err := s.reserveActive(ctx, runtimeID, req.ID); err != nil {
		return nil, err
	}

	// The active reservation above provides the concurrency guarantee this
	// write needs. Use a plain pipeline so managed Redis deployments that allow
	// SET/ZADD but deny MULTI can still create update requests. Exec failures
	// remain safe because the compensation below removes every partially
	// written key and releases the reservation.
	pipe := s.rdb.Pipeline()
	pipe.Set(ctx, updateKey(req.ID), data, updateRequestTTL(req.Status))
	pipe.ZAdd(ctx, updatePendingKey(runtimeID), redis.Z{
		Score:  float64(now.UnixNano()),
		Member: req.ID,
	})
	pipe.Expire(ctx, updatePendingKey(runtimeID), updateStoreRetention*2)
	if _, err := pipe.Exec(ctx); err != nil {
		_ = s.clearActiveIfMatches(ctx, runtimeID, req.ID)
		_ = s.rdb.Del(ctx, updateKey(req.ID)).Err()
		_ = s.rdb.ZRem(ctx, updatePendingKey(runtimeID), req.ID).Err()
		return nil, fmt.Errorf("persist update request: %w", err)
	}
	return req, nil
}

// reserveActive claims the runtime's single active slot. A slot still
// pointing at a request that already ended (a cleanup write that failed) is
// released and claimed again rather than blocking updates until it expires.
func (s *RedisUpdateStore) reserveActive(ctx context.Context, runtimeID, id string) error {
	activeKey := updateActiveKey(runtimeID)
	for attempt := 0; attempt < 2; attempt++ {
		ok, err := s.rdb.SetNX(ctx, activeKey, id, updateStoreRetention).Result()
		if err != nil {
			return fmt.Errorf("reserve active update: %w", err)
		}
		if ok {
			return nil
		}
		active, err := s.GetActive(ctx, runtimeID)
		if err != nil {
			return err
		}
		if active != nil {
			return errUpdateInProgress
		}
	}
	return errUpdateInProgress
}

func (s *RedisUpdateStore) Get(ctx context.Context, id string) (*UpdateRequest, error) {
	return s.loadRequest(ctx, id)
}

func (s *RedisUpdateStore) GetActive(ctx context.Context, runtimeID string) (*UpdateRequest, error) {
	id, err := s.rdb.Get(ctx, updateActiveKey(runtimeID)).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get active update: %w", err)
	}
	req, err := s.loadRequest(ctx, id)
	if err != nil {
		return nil, err
	}
	if req == nil || req.RuntimeID != runtimeID || updateRequestTerminal(req.Status) {
		if err := s.clearActiveIfMatches(ctx, runtimeID, id); err != nil {
			return nil, err
		}
		return nil, nil
	}
	return req, nil
}

// loadRequest reads a request, persisting a timeout that has come due.
func (s *RedisUpdateStore) loadRequest(ctx context.Context, id string) (*UpdateRequest, error) {
	req, err := s.mutate(ctx, id, nil)
	if err != nil || req == nil {
		return req, err
	}
	if updateRequestWaiting(req.Status) {
		// Restore a waiting request's offer entry if it is missing, e.g. after
		// a replica running the previous release dropped it during a rolling
		// deploy.
		s.syncIndexes(ctx, req)
	}
	return req, nil
}

// mutate applies fn to the stored request with compare-and-set and retries
// when another replica changed it in between. fn may be nil to only apply a
// due timeout. The returned error is fn's when the write itself succeeded.
func (s *RedisUpdateStore) mutate(ctx context.Context, id string, fn func(*UpdateRequest, time.Time) error) (*UpdateRequest, error) {
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
			before, err := s.marshalRequest(req)
			if err != nil {
				return nil, err
			}
			fnErr = fn(req, now)
			after, err := s.marshalRequest(req)
			if err != nil {
				return nil, err
			}
			changed = changed || !bytes.Equal(before, after)
		}
		if !changed {
			return req, fnErr
		}

		data, err := s.marshalRequest(req)
		if err != nil {
			return nil, err
		}
		ok, err := compareAndSetScript.Run(
			ctx, s.rdb,
			[]string{updateKey(id)},
			raw, data, updateRequestTTL(req.Status).Milliseconds(),
		).Int64()
		if err != nil {
			return nil, fmt.Errorf("persist update request: %w", err)
		}
		if ok == 0 {
			continue
		}
		s.syncIndexes(ctx, req)
		return req, fnErr
	}
	return nil, errUpdateStoreContention
}

// syncIndexes brings the offer queue and active slot in line with a request's
// status. Each write is idempotent, so a failed write here is repaired by the
// next read or transition of the same request.
func (s *RedisUpdateStore) syncIndexes(ctx context.Context, req *UpdateRequest) {
	pendingKey := updatePendingKey(req.RuntimeID)
	switch {
	case updateRequestTerminal(req.Status):
		_ = s.clearActiveIfMatches(ctx, req.RuntimeID, req.ID)
		s.rdb.ZRem(ctx, pendingKey, req.ID)
	case req.Status == UpdateRunning:
		// Installing: never offer it again.
		s.rdb.ZRem(ctx, pendingKey, req.ID)
	case updateRequestWaiting(req.Status):
		// NX keeps an existing offer time; it only restores a missing entry.
		s.rdb.ZAddNX(ctx, pendingKey, redis.Z{Score: float64(time.Now().UnixNano()), Member: req.ID})
		if req.Status == UpdateScheduled {
			ttl := updateRequestTTL(UpdateScheduled)
			s.rdb.Expire(ctx, pendingKey, ttl)
			s.rdb.Expire(ctx, updateActiveKey(req.RuntimeID), ttl)
		}
	}
}

// redisUpdateEnvelope stores the fields the API payload leaves out beside it.
// RunStartedAt is now part of the payload too; it is still written and read
// here so replicas on the previous release keep it during a rolling deploy.
type redisUpdateEnvelope struct {
	Public          *UpdateRequest `json:"r"`
	RunStartedAt    *time.Time     `json:"s,omitempty"`
	InitiatorUserID string         `json:"u,omitempty"`
}

func (s *RedisUpdateStore) marshalRequest(req *UpdateRequest) ([]byte, error) {
	env := redisUpdateEnvelope{
		Public:          req,
		RunStartedAt:    req.RunStartedAt,
		InitiatorUserID: req.InitiatorUserID,
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
	if env.Public.RunStartedAt == nil {
		env.Public.RunStartedAt = env.RunStartedAt
	}
	env.Public.InitiatorUserID = env.InitiatorUserID
	return env.Public, nil
}

func (s *RedisUpdateStore) HasPending(ctx context.Context, runtimeID string) (bool, error) {
	cnt, err := s.rdb.ZCount(ctx, updatePendingKey(runtimeID), "-inf", strconv.FormatInt(time.Now().UnixNano(), 10)).Result()
	if err != nil {
		return false, fmt.Errorf("zcount pending updates: %w", err)
	}
	return cnt > 0, nil
}

func (s *RedisUpdateStore) PopPending(ctx context.Context, runtimeID string) (*UpdateRequest, error) {
	pendingKey := updatePendingKey(runtimeID)

	for attempt := 0; attempt < updateRedisPopMaxRetries; attempt++ {
		now := time.Now()
		ids, err := s.rdb.ZRangeByScore(ctx, pendingKey, &redis.ZRangeBy{
			Min:   "-inf",
			Max:   strconv.FormatInt(now.UnixNano(), 10),
			Count: 1,
		}).Result()
		if err != nil {
			return nil, fmt.Errorf("zrange pending updates: %w", err)
		}
		if len(ids) == 0 {
			return nil, nil
		}
		id := ids[0]

		req, err := s.loadRequest(ctx, id)
		if err != nil {
			return nil, err
		}
		if req == nil || req.RuntimeID != runtimeID || !updateRequestWaiting(req.Status) {
			s.rdb.ZRem(ctx, pendingKey, id)
			continue
		}

		claimed, err := claimOfferScript.Run(
			ctx, s.rdb,
			[]string{pendingKey},
			id,
			strconv.FormatInt(now.UnixNano(), 10),
			strconv.FormatInt(now.Add(updateReofferInterval).UnixNano(), 10),
		).Int64()
		if err != nil {
			return nil, fmt.Errorf("claim pending update: %w", err)
		}
		if claimed == 0 {
			continue
		}
		return req, nil
	}
	return nil, nil
}

func (s *RedisUpdateStore) MarkRunning(ctx context.Context, id string) (*UpdateRequest, error) {
	return s.mutate(ctx, id, transitionToRunning)
}

func (s *RedisUpdateStore) Schedule(ctx context.Context, id string, activeTasks *int) (*UpdateRequest, error) {
	return s.mutate(ctx, id, func(req *UpdateRequest, now time.Time) error {
		return transitionToScheduled(req, activeTasks, now)
	})
}

func (s *RedisUpdateStore) Cancel(ctx context.Context, id string) (*UpdateRequest, error) {
	return s.mutate(ctx, id, transitionToCancelled)
}

func (s *RedisUpdateStore) Complete(ctx context.Context, id string, output string) error {
	_, err := s.mutate(ctx, id, func(req *UpdateRequest, now time.Time) error {
		transitionToResult(req, UpdateCompleted, output, "", now)
		return nil
	})
	return err
}

func (s *RedisUpdateStore) Fail(ctx context.Context, id string, errMsg string) error {
	_, err := s.mutate(ctx, id, func(req *UpdateRequest, now time.Time) error {
		transitionToResult(req, UpdateFailed, "", errMsg, now)
		return nil
	})
	return err
}

func (s *RedisUpdateStore) clearActiveIfMatches(ctx context.Context, runtimeID, id string) error {
	if runtimeID == "" || id == "" {
		return nil
	}
	if err := deleteIfValueScript.Run(ctx, s.rdb, []string{updateActiveKey(runtimeID)}, id).Err(); err != nil {
		return fmt.Errorf("clear active update: %w", err)
	}
	return nil
}
