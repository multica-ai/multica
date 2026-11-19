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
// Two request shapes share the request keys:
//
//   - A fail-fast request (QueueWhenBusy false) is stored as the release
//     before queued updates stores every request: the runtime's "pending" queue
//     (delivery marks it running and removes it), so replicas of both releases
//     deliver it once.
//   - A queued request (QueueWhenBusy true) lives in the runtime's "offers"
//     queue, scored by the earliest time (UnixNano) it may be offered again.
//     The previous release never reads that queue, so only this release offers
//     it.
//
// One machine has at most one unfinished request, and that must also hold
// while replicas of the previous release still serve traffic during a rolling
// deploy. That release only takes a per-runtime "active" slot (SET NX, before
// it writes the request). So every request here takes the machine's slot plus
// the "active" slot of every runtime the machine serves: a previous-release
// replica asked to update any of them sees the slot taken, and this release
// sees a request the previous one started on any of them.
//
// Every status change is a compare-and-set on the stored request bytes, so a
// cancel and a daemon starting the update cannot both win.

const (
	updateKeyPrefix          = "mul:" + runtimePendingRedisHashTag + ":update:req:"
	updatePendingPrefix      = "mul:" + runtimePendingRedisHashTag + ":update:pending:"
	updateActivePrefix       = "mul:" + runtimePendingRedisHashTag + ":update:active:"
	updateOffersPrefix       = "mul:" + runtimePendingRedisHashTag + ":update:offers:"
	updateMachinePrefix      = "mul:" + runtimePendingRedisHashTag + ":update:machine:"
	updateRedisPopMaxRetries = 5
	updateRedisCASMaxRetries = 8
)

func updateKey(id string) string                    { return updateKeyPrefix + id }
func updatePendingKey(runtimeID string) string      { return updatePendingPrefix + runtimeID }
func updateActiveKey(runtimeID string) string       { return updateActivePrefix + runtimeID }
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
		return updateStoreRetention
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

// createUpdateScript takes every slot the request needs and writes the request
// and its queue entry in one step: no reader ever sees a slot whose request is
// not written yet, and two runtimes of one machine cannot both take it. A
// script rather than MULTI, which some managed Redis deployments deny.
//
// KEYS: req, queue, slot... ARGV: id, data, TTL ms, queue score.
// Returns 0 when a slot is taken.
var createUpdateScript = redis.NewScript(`
for i = 3, #KEYS do
    if redis.call('EXISTS', KEYS[i]) == 1 then
        return 0
    end
end
for i = 3, #KEYS do
    redis.call('SET', KEYS[i], ARGV[1], 'PX', tonumber(ARGV[3]))
end
redis.call('SET', KEYS[1], ARGV[2], 'PX', tonumber(ARGV[3]))
redis.call('ZADD', KEYS[2], ARGV[4], ARGV[1])
redis.call('PEXPIRE', KEYS[2], tonumber(ARGV[3]) * 2)
return 1
`)

// claimOfferScript claims a due queued request for one delivery by pushing its
// next offer time forward. Returns 0 when another replica claimed it first.
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

// requestSlots lists the slot keys a request holds: the "active" slot of its
// runtime and of the machine's other runtimes, and the machine's slot.
func requestSlots(req *UpdateRequest) []string {
	slots := []string{updateActiveKey(req.RuntimeID)}
	for _, id := range req.MachineRuntimeIDs {
		if id != req.RuntimeID {
			slots = append(slots, updateActiveKey(id))
		}
	}
	if req.MachineKey != "" {
		slots = append(slots, updateMachineSlotKey(req.MachineKey))
	}
	return slots
}

func requestQueueKey(req *UpdateRequest) string {
	if req.QueueWhenBusy {
		return updateOffersKey(req.RuntimeID)
	}
	return updatePendingKey(req.RuntimeID)
}

func (s *RedisUpdateStore) Create(ctx context.Context, runtimeID, machineKey, targetVersion, initiatorUserID string, queueWhenBusy bool, machineRuntimeIDs ...string) (*UpdateRequest, error) {
	now := time.Now()
	req := &UpdateRequest{
		ID:                randomID(),
		RuntimeID:         runtimeID,
		MachineKey:        machineKey,
		MachineRuntimeIDs: machineRuntimeIDs,
		InitiatorUserID:   initiatorUserID,
		Status:            UpdatePending,
		TargetVersion:     targetVersion,
		CreatedAt:         now,
		UpdatedAt:         now,
		QueueWhenBusy:     queueWhenBusy,
	}
	data, err := s.marshalRequest(req)
	if err != nil {
		return nil, err
	}

	keys := append([]string{updateKey(req.ID), requestQueueKey(req)}, requestSlots(req)...)
	for attempt := 0; attempt < 2; attempt++ {
		created, err := createUpdateScript.Run(
			ctx, s.rdb, keys,
			req.ID, data, updateRequestTTL(req).Milliseconds(),
			strconv.FormatInt(now.UnixNano(), 10),
		).Int64()
		if err != nil {
			return nil, fmt.Errorf("persist update request: %w", err)
		}
		if created == 1 {
			return req, nil
		}
		// A slot is taken. Release the ones that point at a request that
		// already ended or expired; then one more try.
		busy := false
		for _, slot := range keys[2:] {
			live, err := s.releaseStaleSlot(ctx, slot)
			if err != nil {
				return nil, err
			}
			busy = busy || live
		}
		if busy {
			return nil, errUpdateInProgress
		}
	}
	return nil, errUpdateInProgress
}

// releaseStaleSlot clears slot when its request ended or is gone, and reports
// whether it still holds an unfinished one.
//
// A slot whose request is missing may belong to a previous-release replica
// that took the slot and has not written the request yet. That release gives
// the slot the request's own short lifetime (updateStoreRetention), so such a
// slot is left to expire. A longer-lived one was written by this release
// together with its request, and the request is really gone.
func (s *RedisUpdateStore) releaseStaleSlot(ctx context.Context, slot string) (bool, error) {
	id, err := s.rdb.Get(ctx, slot).Result()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("get update slot: %w", err)
	}
	req, err := s.loadRequest(ctx, id)
	if err != nil {
		return false, err
	}
	if req != nil && !updateRequestTerminal(req.Status) {
		return true, nil
	}
	if req == nil {
		ttl, err := s.rdb.PTTL(ctx, slot).Result()
		if err != nil {
			return false, fmt.Errorf("get update slot ttl: %w", err)
		}
		// -2: the slot expired meanwhile. -1 (no TTL) is not a slot any
		// release writes.
		if ttl >= 0 && ttl <= updateStoreRetention {
			return true, nil
		}
	}
	if err := s.deleteIfValue(ctx, slot, id); err != nil {
		return false, err
	}
	return false, nil
}

func (s *RedisUpdateStore) Get(ctx context.Context, id string) (*UpdateRequest, error) {
	return s.loadRequest(ctx, id)
}

func (s *RedisUpdateStore) GetActive(ctx context.Context, machineKey string, machineRuntimeIDs ...string) (*UpdateRequest, error) {
	slot := updateMachineSlotKey(machineKey)
	id, err := s.rdb.Get(ctx, slot).Result()
	switch {
	case errors.Is(err, redis.Nil):
	case err != nil:
		return nil, fmt.Errorf("get active update: %w", err)
	default:
		req, err := s.loadRequest(ctx, id)
		if err != nil {
			return nil, err
		}
		if req != nil && req.MachineKey == machineKey && !updateRequestTerminal(req.Status) {
			return req, nil
		}
		if err := s.deleteIfValue(ctx, slot, id); err != nil {
			return nil, err
		}
	}
	// A request a previous-release replica created holds only its runtime's
	// slot.
	for _, runtimeID := range machineRuntimeIDs {
		id, err := s.rdb.Get(ctx, updateActiveKey(runtimeID)).Result()
		if errors.Is(err, redis.Nil) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("get active update: %w", err)
		}
		req, err := s.loadRequest(ctx, id)
		if err != nil {
			return nil, err
		}
		if req != nil && req.RuntimeID == runtimeID && !updateRequestTerminal(req.Status) {
			return req, nil
		}
	}
	return nil, nil
}

// loadRequest reads a request, persisting a timeout that has come due.
func (s *RedisUpdateStore) loadRequest(ctx context.Context, id string) (*UpdateRequest, error) {
	req, err := s.mutate(ctx, id, nil)
	if err != nil || req == nil {
		return req, err
	}
	if req.QueueWhenBusy && !updateRequestTerminal(req.Status) {
		// Restore a queued request's offer entry if a failed write lost it.
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

// syncIndexes brings the queues and slots in line with a request's status.
// Each write is idempotent, so a failed write here is repaired by the next
// read or transition of the same request.
func (s *RedisUpdateStore) syncIndexes(ctx context.Context, req *UpdateRequest) {
	queue := requestQueueKey(req)
	switch {
	case updateRequestTerminal(req.Status):
		for _, slot := range requestSlots(req) {
			_ = s.deleteIfValue(ctx, slot, req.ID)
		}
		s.rdb.ZRem(ctx, queue, req.ID)
	case !req.QueueWhenBusy:
		// The previous release's shape: delivery already took it off the
		// queue, and the slots expire with it.
	default:
		// NX keeps an existing offer time; it only restores a missing entry.
		s.rdb.ZAddNX(ctx, queue, redis.Z{Score: float64(time.Now().UnixNano()), Member: req.ID})
		ttl := updateRequestTTL(req)
		s.rdb.Expire(ctx, queue, ttl)
		for _, slot := range requestSlots(req) {
			s.rdb.Expire(ctx, slot, ttl)
		}
	}
}

// redisUpdateEnvelope stores the fields the API payload leaves out beside it.
// The previous release reads only r, s and u.
type redisUpdateEnvelope struct {
	Public           *UpdateRequest `json:"r"`
	RunStartedAt     *time.Time     `json:"s,omitempty"`
	InitiatorUserID  string         `json:"u,omitempty"`
	QueueWhenBusy    bool           `json:"q,omitempty"`
	MachineKey       string         `json:"m,omitempty"`
	LegacyOfferAt    *time.Time     `json:"lo,omitempty"`
	LegacyQuietUntil *time.Time     `json:"lq,omitempty"`
	MachineRuntimes  []string       `json:"mr,omitempty"`
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
		MachineRuntimes:  req.MachineRuntimeIDs,
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
	env.Public.MachineRuntimeIDs = env.MachineRuntimes
	env.Public.Cancellable = false
	return env.Public, nil
}

func (s *RedisUpdateStore) HasPending(ctx context.Context, runtimeID string) (bool, error) {
	pipe := s.rdb.Pipeline()
	legacy := pipe.ZCard(ctx, updatePendingKey(runtimeID))
	offers := pipe.ZCount(ctx, updateOffersKey(runtimeID), "-inf", strconv.FormatInt(time.Now().UnixNano(), 10))
	if _, err := pipe.Exec(ctx); err != nil {
		return false, fmt.Errorf("count pending updates: %w", err)
	}
	return legacy.Val() > 0 || offers.Val() > 0, nil
}

func (s *RedisUpdateStore) PopPending(ctx context.Context, runtimeID string, deferrableDaemon bool) (*UpdateRequest, error) {
	req, err := s.popFailFast(ctx, runtimeID)
	if err != nil || req != nil {
		return req, err
	}
	return s.popQueued(ctx, runtimeID, deferrableDaemon)
}

// popFailFast delivers the runtime's oldest fail-fast request exactly as the
// previous release does: the claim removes it from the queue and marks it
// running in one script, so replicas of either release deliver it once.
func (s *RedisUpdateStore) popFailFast(ctx context.Context, runtimeID string) (*UpdateRequest, error) {
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

		req, err := s.loadRequest(ctx, id)
		if err != nil {
			return nil, err
		}
		if req == nil || req.Status != UpdatePending {
			s.rdb.ZRem(ctx, pendingKey, id)
			continue
		}

		now := time.Now()
		req.Status = UpdateRunning
		req.RunStartedAt = &now
		req.UpdatedAt = now
		data, err := s.marshalRequest(req)
		if err != nil {
			return nil, err
		}
		result, err := claimPendingScript.Run(
			ctx, s.rdb,
			[]string{pendingKey, updateKey(id)},
			id, data, int(updateRequestTTL(req).Seconds()),
		).Int64()
		if err != nil {
			return nil, fmt.Errorf("claim pending update: %w", err)
		}
		if result == 0 {
			continue
		}
		return req, nil
	}
	return nil, nil
}

func (s *RedisUpdateStore) popQueued(ctx context.Context, runtimeID string, deferrableDaemon bool) (*UpdateRequest, error) {
	offersKey := updateOffersKey(runtimeID)
	for attempt := 0; attempt < updateRedisPopMaxRetries; attempt++ {
		now := time.Now()
		ids, err := s.rdb.ZRangeByScore(ctx, offersKey, &redis.ZRangeBy{
			Min:   "-inf",
			Max:   strconv.FormatInt(now.UnixNano(), 10),
			Count: 1,
		}).Result()
		if err != nil {
			return nil, fmt.Errorf("zrange queued updates: %w", err)
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
			return nil, fmt.Errorf("claim queued update: %w", err)
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
			// Not for this daemon right now; its offer time already moved on.
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

func (s *RedisUpdateStore) deleteIfValue(ctx context.Context, key, id string) error {
	if id == "" {
		return nil
	}
	if err := deleteIfValueScript.Run(ctx, s.rdb, []string{key}, id).Err(); err != nil {
		return fmt.Errorf("clear update slot: %w", err)
	}
	return nil
}
