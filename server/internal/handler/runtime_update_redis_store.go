package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis-backed implementation of UpdateStore. Web POST, daemon heartbeat,
// daemon reports and UI polling can all hit different API replicas, so the
// lifecycle lives in shared storage.
//
// Keys (all share one cluster hash tag so the scripts below can touch them
// together):
//
//   - req:<id>          the request record
//   - offers:<runtime>  sorted set of the runtime's undelivered or re-offered
//     requests, scored by the next offer time (unix ms)
//   - machine:<key>     the machine's unfinished request ID (see
//     updateMachineKey); the one-update-per-machine guard
//
// Every change to a record is a compare-and-set against the bytes the change
// was computed from (updateCASScript), and the same script keeps the offer
// queue and the machine slot in step with the record. A transition that lost a
// race recomputes from the new record, so concurrent cancel / start / timeout /
// result writes can never overwrite each other.
//
// The keyspace is "update2". The fail-fast-only records of earlier releases
// used "update" with a different shape; the two never mix.

const (
	updateKeyPrefix         = "mul:" + runtimePendingRedisHashTag + ":update2:req:"
	updateOffersPrefix      = "mul:" + runtimePendingRedisHashTag + ":update2:offers:"
	updateMachineSlotPrefix = "mul:" + runtimePendingRedisHashTag + ":update2:machine:"
	updateRedisMaxRetries   = 8
)

func updateKey(id string) string                    { return updateKeyPrefix + id }
func updateOffersKey(runtimeID string) string       { return updateOffersPrefix + runtimeID }
func updateMachineSlotKey(machineKey string) string { return updateMachineSlotPrefix + machineKey }
func updateTTLMillis(d time.Duration) int64         { return d.Milliseconds() }
func updateOfferScore(t time.Time) string           { return strconv.FormatInt(t.UnixMilli(), 10) }
func updateRetentionFor(req *UpdateRequest) time.Duration {
	if updateRequestTerminal(req.Status) {
		return updateStoreRetention
	}
	return updateActiveRetention
}

// updateCreateScript takes the machine slot and writes the request and its
// first offer in one step, so there is never a slot without a request.
//
// KEYS: machine slot, request, offers. ARGV: id, record, ttl ms, offer score.
var updateCreateScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 1 then
    return 0
end
redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[3])
redis.call('SET', KEYS[2], ARGV[2], 'PX', ARGV[3])
redis.call('ZADD', KEYS[3], ARGV[4], ARGV[1])
redis.call('PEXPIRE', KEYS[3], ARGV[3])
return 1
`)

// updateCASScript replaces a request record only if it still holds the bytes
// the caller read. A terminal record releases the machine slot (if it still
// names this request) and leaves the offer queue; an unfinished one keeps the
// slot alive as long as itself and moves its offer as told.
//
// KEYS: request, machine slot, offers.
// ARGV: expected record, new record, ttl ms, id, terminal ("1"/"0"),
// offer ("keep", "remove", or a score).
var updateCASScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) ~= ARGV[1] then
    return 0
end
redis.call('SET', KEYS[1], ARGV[2], 'PX', ARGV[3])
if ARGV[5] == '1' then
    if redis.call('GET', KEYS[2]) == ARGV[4] then
        redis.call('DEL', KEYS[2])
    end
    redis.call('ZREM', KEYS[3], ARGV[4])
    return 1
end
if redis.call('GET', KEYS[2]) == ARGV[4] then
    redis.call('PEXPIRE', KEYS[2], ARGV[3])
end
if ARGV[6] == 'remove' then
    redis.call('ZREM', KEYS[3], ARGV[4])
elseif ARGV[6] ~= 'keep' then
    redis.call('ZADD', KEYS[3], ARGV[6], ARGV[4])
    redis.call('PEXPIRE', KEYS[3], ARGV[3])
end
return 1
`)

var deleteIfValueScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
    return redis.call('DEL', KEYS[1])
end
return 0
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
	slotKey := updateMachineSlotKey(machineKey)

	// A request that should have timed out still holds the slot until
	// something reads it; reading it here ends it and frees the slot.
	if err := s.settleSlot(ctx, slotKey); err != nil {
		return nil, err
	}

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
	ok, err := updateCreateScript.Run(ctx, s.rdb,
		[]string{slotKey, updateKey(req.ID), updateOffersKey(runtimeID)},
		req.ID, data, updateTTLMillis(updateActiveRetention), updateOfferScore(now),
	).Int64()
	if err != nil {
		return nil, fmt.Errorf("reserve active update: %w", err)
	}
	if ok == 0 {
		return nil, errUpdateInProgress
	}
	return req, nil
}

// settleSlot reads the request holding a machine slot so a timed-out one ends
// (which releases the slot), and drops a slot whose request is gone.
func (s *RedisUpdateStore) settleSlot(ctx context.Context, slotKey string) error {
	id, err := s.rdb.Get(ctx, slotKey).Result()
	if errors.Is(err, redis.Nil) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reserve active update: %w", err)
	}
	req, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if req == nil {
		if err := deleteIfValueScript.Run(ctx, s.rdb, []string{slotKey}, id).Err(); err != nil {
			return fmt.Errorf("reserve active update: %w", err)
		}
	}
	return nil
}

func (s *RedisUpdateStore) Get(ctx context.Context, id string) (*UpdateRequest, error) {
	return s.mutate(ctx, id, func(*UpdateRequest, time.Time) (updateOfferPlan, error) {
		return updateOfferPlan{}, nil
	})
}

func (s *RedisUpdateStore) GetActive(ctx context.Context, machineKey string) (*UpdateRequest, error) {
	if machineKey == "" {
		return nil, nil
	}
	slotKey := updateMachineSlotKey(machineKey)
	id, err := s.rdb.Get(ctx, slotKey).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get active update: %w", err)
	}
	req, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if req == nil {
		_ = deleteIfValueScript.Run(ctx, s.rdb, []string{slotKey}, id).Err()
		return nil, nil
	}
	if updateRequestTerminal(req.Status) {
		return nil, nil
	}
	return req, nil
}

// mutate reads a request, applies timeouts and fn, and writes the result back
// with updateCASScript, starting over if another writer got there first. It
// returns nil for an unknown request; fn's error is returned with the request
// as it stands (fn should leave req unchanged when it refuses).
func (s *RedisUpdateStore) mutate(ctx context.Context, id string, fn func(*UpdateRequest, time.Time) (updateOfferPlan, error)) (*UpdateRequest, error) {
	for attempt := 0; attempt < updateRedisMaxRetries; attempt++ {
		raw, err := s.rdb.Get(ctx, updateKey(id)).Result()
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("get update request: %w", err)
		}
		req, err := s.unmarshalRequest([]byte(raw))
		if err != nil {
			return nil, err
		}
		now := time.Now()
		applyUpdateTimeout(req, now)
		plan, fnErr := fn(req, now)

		data, err := s.marshalRequest(req)
		if err != nil {
			return nil, err
		}
		offer := "keep"
		switch {
		case plan.remove:
			offer = "remove"
		case !plan.next.IsZero():
			offer = updateOfferScore(plan.next)
		}
		if string(data) == raw && offer == "keep" {
			return req, fnErr
		}
		terminal := "0"
		if updateRequestTerminal(req.Status) {
			terminal = "1"
		}
		ok, err := updateCASScript.Run(ctx, s.rdb,
			[]string{updateKey(id), updateMachineSlotKey(req.MachineKey), updateOffersKey(req.RuntimeID)},
			raw, data, updateTTLMillis(updateRetentionFor(req)), id, terminal, offer,
		).Int64()
		if err != nil {
			return nil, fmt.Errorf("persist update request: %w", err)
		}
		if ok == 1 {
			return req, fnErr
		}
	}
	return nil, fmt.Errorf("persist update request: too much contention on %s", id)
}

// redisUpdateRecord is the stored form: the public fields plus the ones
// responses hide.
type redisUpdateRecord struct {
	Public          *UpdateRequest `json:"r"`
	InitiatorUserID string         `json:"u,omitempty"`
	MachineKey      string         `json:"m"`
	QueueWhenBusy   bool           `json:"q,omitempty"`
}

func (s *RedisUpdateStore) marshalRequest(req *UpdateRequest) ([]byte, error) {
	public := *req
	public.Cancellable = false
	data, err := json.Marshal(redisUpdateRecord{
		Public:          &public,
		InitiatorUserID: req.InitiatorUserID,
		MachineKey:      req.MachineKey,
		QueueWhenBusy:   req.QueueWhenBusy,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal update request: %w", err)
	}
	return data, nil
}

func (s *RedisUpdateStore) unmarshalRequest(raw []byte) (*UpdateRequest, error) {
	var rec redisUpdateRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return nil, fmt.Errorf("decode update request: %w", err)
	}
	if rec.Public == nil {
		return nil, fmt.Errorf("decode update request: missing payload")
	}
	rec.Public.InitiatorUserID = rec.InitiatorUserID
	rec.Public.MachineKey = rec.MachineKey
	rec.Public.QueueWhenBusy = rec.QueueWhenBusy
	return rec.Public, nil
}

func (s *RedisUpdateStore) HasPending(ctx context.Context, runtimeID string) (bool, error) {
	cnt, err := s.rdb.ZCount(ctx, updateOffersKey(runtimeID), "-inf", updateOfferScore(time.Now())).Result()
	if err != nil {
		return false, fmt.Errorf("count due updates: %w", err)
	}
	return cnt > 0, nil
}

func (s *RedisUpdateStore) PopPending(ctx context.Context, runtimeID string, deferrableDaemon bool) (*UpdateRequest, error) {
	offersKey := updateOffersKey(runtimeID)
	for attempt := 0; attempt < updateRedisMaxRetries; attempt++ {
		ids, err := s.rdb.ZRangeArgs(ctx, redis.ZRangeArgs{
			Key:     offersKey,
			Start:   "-inf",
			Stop:    updateOfferScore(time.Now()),
			ByScore: true,
			Count:   1,
		}).Result()
		if err != nil {
			return nil, fmt.Errorf("list due updates: %w", err)
		}
		if len(ids) == 0 {
			return nil, nil
		}
		id := ids[0]

		offered := false
		req, err := s.mutate(ctx, id, func(req *UpdateRequest, now time.Time) (updateOfferPlan, error) {
			var plan updateOfferPlan
			offered, plan = prepareOffer(req, deferrableDaemon, now)
			return plan, nil
		})
		if err != nil {
			return nil, err
		}
		if req == nil {
			s.rdb.ZRem(ctx, offersKey, id)
			continue
		}
		if offered {
			return req, nil
		}
	}
	return nil, nil
}

func (s *RedisUpdateStore) transition(ctx context.Context, id string, fn func(*UpdateRequest, time.Time) error) (*UpdateRequest, error) {
	return s.mutate(ctx, id, func(req *UpdateRequest, now time.Time) (updateOfferPlan, error) {
		return updateOfferPlan{}, fn(req, now)
	})
}

func (s *RedisUpdateStore) MarkRunning(ctx context.Context, id string) (*UpdateRequest, error) {
	return s.transition(ctx, id, transitionToRunning)
}

func (s *RedisUpdateStore) Schedule(ctx context.Context, id string, activeTasks *int) (*UpdateRequest, error) {
	return s.transition(ctx, id, func(req *UpdateRequest, now time.Time) error {
		return transitionToScheduled(req, activeTasks, now)
	})
}

func (s *RedisUpdateStore) Cancel(ctx context.Context, id string) (*UpdateRequest, error) {
	return s.transition(ctx, id, transitionToCancelled)
}

func (s *RedisUpdateStore) Complete(ctx context.Context, id string, output string) error {
	_, err := s.transition(ctx, id, func(req *UpdateRequest, now time.Time) error {
		transitionToResult(req, UpdateCompleted, output, "", now)
		return nil
	})
	return err
}

func (s *RedisUpdateStore) Fail(ctx context.Context, id string, errMsg string) error {
	_, err := s.transition(ctx, id, func(req *UpdateRequest, now time.Time) error {
		transitionToResult(req, UpdateFailed, "", errMsg, now)
		return nil
	})
	return err
}
