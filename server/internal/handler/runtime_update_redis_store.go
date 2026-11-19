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
//   - update:req:<id>          the request envelope; every status change is a
//     compare-and-set on the stored bytes, so a cancel and a daemon starting
//     the update cannot both win.
//   - update:offers:<rt>       sorted set of the runtime's unfinished
//     requests, scored by the earliest time (UnixNano) they may be offered to
//     the daemon again.
//   - update:machine:<machine> the machine's one unfinished request.
//
// Request keys are shared with the release before queued updates, so every
// replica can read and report on every request during a rolling deploy. The
// offer queue and the machine slot are new keys that release never touches:
// it keeps its own "update:pending:" queue and "update:active:" slots for the
// requests it creates, and can neither drop a queued request from the offer
// queue nor deliver one. What it can do — answer a daemon's "running" report
// with a bare "ok" without checking for cancellation — the daemon guards
// against by installing only on an explicit acceptance.

const (
	updateKeyPrefix          = "mul:" + runtimePendingRedisHashTag + ":update:req:"
	updateOffersPrefix       = "mul:" + runtimePendingRedisHashTag + ":update:offers:"
	updateMachinePrefix      = "mul:" + runtimePendingRedisHashTag + ":update:machine:"
	updateRedisPopMaxRetries = 5
	updateRedisCASMaxRetries = 8
)

func updateKey(id string) string                    { return updateKeyPrefix + id }
func updateOffersKey(runtimeID string) string       { return updateOffersPrefix + runtimeID }
func updateMachineSlotKey(machineKey string) string { return updateMachinePrefix + machineKey }

// updateRequestTTL keeps an unfinished request for as long as it may still
// run, and a finished one for the read window.
func updateRequestTTL(req *UpdateRequest) time.Duration {
	switch {
	case updateRequestTerminal(req.Status):
		return updateStoreRetention
	case req.QueueWhenBusy:
		return updateScheduledTimeout + updateLegacyOfferTimeout + updateStoreRetention
	default:
		return updateLegacyOfferTimeout + updateStoreRetention
	}
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

// createUpdateScript reserves the machine's slot and writes the request and
// its offer entry in one step, so no reader ever sees a slot whose request is
// not written yet (and mistakes it for a leftover), and two runtimes of one
// machine cannot both take it. A script rather than MULTI, which some managed
// Redis deployments deny.
//
// KEYS: machine slot, req, offers. ARGV: id, data, TTL ms, offer score,
// offers TTL ms. Returns 0 when the slot is taken.
var createUpdateScript = redis.NewScript(`
if not redis.call('SET', KEYS[1], ARGV[1], 'NX', 'PX', tonumber(ARGV[3])) then
    return 0
end
redis.call('SET', KEYS[2], ARGV[2], 'PX', tonumber(ARGV[3]))
redis.call('ZADD', KEYS[3], ARGV[4], ARGV[1])
redis.call('PEXPIRE', KEYS[3], tonumber(ARGV[5]))
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

func (s *RedisUpdateStore) Create(ctx context.Context, runtimeID, machineKey, targetVersion, initiatorUserID string, queueWhenBusy bool) (*UpdateRequest, error) {
	now := time.Now()
	req := &UpdateRequest{
		ID:              randomID(),
		RuntimeID:       runtimeID,
		MachineKey:      machineKey,
		InitiatorUserID: initiatorUserID,
		Status:          UpdatePending,
		TargetVersion:   targetVersion,
		CreatedAt:       now,
		UpdatedAt:       now,
		QueueWhenBusy:   queueWhenBusy,
	}
	data, err := s.marshalRequest(req)
	if err != nil {
		return nil, err
	}

	keys := []string{updateMachineSlotKey(machineKey), updateKey(req.ID), updateOffersKey(runtimeID)}
	ttl := updateRequestTTL(req).Milliseconds()
	for attempt := 0; attempt < 2; attempt++ {
		created, err := createUpdateScript.Run(
			ctx, s.rdb, keys,
			req.ID, data, ttl,
			strconv.FormatInt(now.UnixNano(), 10),
			ttl,
		).Int64()
		if err != nil {
			return nil, fmt.Errorf("persist update request: %w", err)
		}
		if created == 1 {
			return req, nil
		}
		// The slot is taken. GetActive releases it when it points at a request
		// that already ended or expired; then one more try.
		active, err := s.GetActive(ctx, machineKey)
		if err != nil {
			return nil, err
		}
		if active != nil {
			return nil, errUpdateInProgress
		}
	}
	return nil, errUpdateInProgress
}

func (s *RedisUpdateStore) Get(ctx context.Context, id string) (*UpdateRequest, error) {
	return s.loadRequest(ctx, id)
}

func (s *RedisUpdateStore) GetActive(ctx context.Context, machineKey string) (*UpdateRequest, error) {
	id, err := s.rdb.Get(ctx, updateMachineSlotKey(machineKey)).Result()
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
	if req == nil || req.MachineKey != machineKey || updateRequestTerminal(req.Status) {
		if err := s.clearMachineIfMatches(ctx, machineKey, id); err != nil {
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
	if !updateRequestTerminal(req.Status) {
		// Restore an unfinished request's offer entry if a failed write lost it.
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
			raw, data, updateRequestTTL(req).Milliseconds(),
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

// syncIndexes brings the offer queue and machine slot in line with a
// request's status. Each write is idempotent, so a failed write here is
// repaired by the next read or transition of the same request.
func (s *RedisUpdateStore) syncIndexes(ctx context.Context, req *UpdateRequest) {
	offersKey := updateOffersKey(req.RuntimeID)
	switch {
	case updateRequestTerminal(req.Status):
		_ = s.clearMachineIfMatches(ctx, req.MachineKey, req.ID)
		s.rdb.ZRem(ctx, offersKey, req.ID)
	case req.Status == UpdateRunning && !req.QueueWhenBusy:
		// Installing, and no daemon will be offered it again (see prepareOffer).
		s.rdb.ZRem(ctx, offersKey, req.ID)
	default:
		// NX keeps an existing offer time; it only restores a missing entry.
		s.rdb.ZAddNX(ctx, offersKey, redis.Z{Score: float64(time.Now().UnixNano()), Member: req.ID})
		ttl := updateRequestTTL(req)
		s.rdb.Expire(ctx, offersKey, ttl)
		if req.MachineKey != "" {
			s.rdb.Expire(ctx, updateMachineSlotKey(req.MachineKey), ttl)
		}
	}
}

// redisUpdateEnvelope stores the fields the API payload leaves out beside it.
// A replica of the previous release reads only r, s and u; a request it
// rewrites (it can only end one) loses the rest, which a finished request no
// longer needs.
type redisUpdateEnvelope struct {
	Public           *UpdateRequest `json:"r"`
	RunStartedAt     *time.Time     `json:"s,omitempty"`
	InitiatorUserID  string         `json:"u,omitempty"`
	QueueWhenBusy    bool           `json:"q,omitempty"`
	MachineKey       string         `json:"m,omitempty"`
	LegacyOfferAt    *time.Time     `json:"lo,omitempty"`
	LegacyQuietUntil *time.Time     `json:"lq,omitempty"`
}

func (s *RedisUpdateStore) marshalRequest(req *UpdateRequest) ([]byte, error) {
	env := redisUpdateEnvelope{
		Public:           req,
		RunStartedAt:     req.RunStartedAt,
		InitiatorUserID:  req.InitiatorUserID,
		QueueWhenBusy:    req.QueueWhenBusy,
		MachineKey:       req.MachineKey,
		LegacyOfferAt:    req.LegacyOfferAt,
		LegacyQuietUntil: req.LegacyQuietUntil,
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
	env.Public.QueueWhenBusy = env.QueueWhenBusy
	env.Public.MachineKey = env.MachineKey
	env.Public.LegacyOfferAt = env.LegacyOfferAt
	env.Public.LegacyQuietUntil = env.LegacyQuietUntil
	env.Public.Cancellable = false
	return env.Public, nil
}

func (s *RedisUpdateStore) HasPending(ctx context.Context, runtimeID string) (bool, error) {
	cnt, err := s.rdb.ZCount(ctx, updateOffersKey(runtimeID), "-inf", strconv.FormatInt(time.Now().UnixNano(), 10)).Result()
	if err != nil {
		return false, fmt.Errorf("zcount pending updates: %w", err)
	}
	return cnt > 0, nil
}

func (s *RedisUpdateStore) PopPending(ctx context.Context, runtimeID string, deferrableDaemon bool) (*UpdateRequest, error) {
	offersKey := updateOffersKey(runtimeID)

	for attempt := 0; attempt < updateRedisPopMaxRetries; attempt++ {
		now := time.Now()
		ids, err := s.rdb.ZRangeByScore(ctx, offersKey, &redis.ZRangeBy{
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

		// Claim this delivery among replicas by moving the next offer time.
		claimed, err := claimOfferScript.Run(
			ctx, s.rdb,
			[]string{offersKey},
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

		// Record the offer with compare-and-set, so a cancel that lands first
		// wins and the request is not delivered.
		offered := false
		req, err := s.mutate(ctx, id, func(req *UpdateRequest, now time.Time) error {
			offered = req.RuntimeID == runtimeID && prepareOffer(req, deferrableDaemon, now)
			return nil
		})
		if err != nil {
			return nil, err
		}
		if req == nil || req.RuntimeID != runtimeID || updateRequestTerminal(req.Status) {
			s.rdb.ZRem(ctx, offersKey, id)
			continue
		}
		if !offered {
			// Not for this daemon (a running request for a non-deferrable
			// daemon); its offer time already moved on.
			return nil, nil
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

func (s *RedisUpdateStore) clearMachineIfMatches(ctx context.Context, machineKey, id string) error {
	if machineKey == "" || id == "" {
		return nil
	}
	if err := deleteIfValueScript.Run(ctx, s.rdb, []string{updateMachineSlotKey(machineKey)}, id).Err(); err != nil {
		return fmt.Errorf("clear active update: %w", err)
	}
	return nil
}
