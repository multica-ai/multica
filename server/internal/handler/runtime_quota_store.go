package handler

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/redis/go-redis/v9"
)

const runtimeQuotaRetention = 24 * time.Hour

type runtimeQuotaState struct {
	OwnerID     string                           `json:"owner_id"`
	RequestID   string                           `json:"request_id"`
	RequestedAt time.Time                        `json:"requested_at"`
	Report      protocol.SubscriptionQuotaReport `json:"report"`
}

func (s runtimeQuotaState) pending(now time.Time) bool {
	return s.Report.Status == "pending" && now.Before(s.RequestedAt.Add(protocol.SubscriptionQuotaRequestTTL))
}

func (s runtimeQuotaState) reportFor(ownerID string, now time.Time) protocol.SubscriptionQuotaReport {
	if s.OwnerID != ownerID || s.Report.Status == "" {
		return protocol.SubscriptionQuotaReport{Status: "idle"}
	}
	report := s.Report
	if report.Status == "pending" && !s.pending(now) {
		report.Status = "timeout"
	}
	if report.Snapshot != nil && now.Sub(time.Unix(report.Snapshot.ObservedAt, 0)) >= runtimeQuotaRetention {
		report.Snapshot = nil
	}
	return report
}

// Updates commit only if the transition returns changed. The callback can run
// again if a concurrent API replica updates the same runtime.
type runtimeQuotaStore interface {
	read(ctx context.Context, ids []string) (map[string]runtimeQuotaState, error)
	update(ctx context.Context, id string, transition func(runtimeQuotaState) (runtimeQuotaState, bool)) (runtimeQuotaState, error)
}

type memoryRuntimeQuotaStore struct {
	mu      sync.Mutex
	entries map[string]*memoryRuntimeQuotaEntry
}

type memoryRuntimeQuotaEntry struct {
	state     runtimeQuotaState
	expiresAt time.Time
	timer     *time.Timer
}

func (s *memoryRuntimeQuotaStore) state(id string) runtimeQuotaState {
	entry := s.entries[id]
	if entry == nil || !entry.expiresAt.After(time.Now()) {
		return runtimeQuotaState{}
	}
	return entry.state
}

func (s *memoryRuntimeQuotaStore) read(_ context.Context, ids []string) (map[string]runtimeQuotaState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	states := make(map[string]runtimeQuotaState, len(ids))
	for _, id := range ids {
		states[id] = s.state(id)
	}
	return states, nil
}

func (s *memoryRuntimeQuotaStore) update(_ context.Context, id string, transition func(runtimeQuotaState) (runtimeQuotaState, bool)) (runtimeQuotaState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, changed := transition(s.state(id))
	if !changed {
		return state, nil
	}
	if s.entries == nil {
		s.entries = make(map[string]*memoryRuntimeQuotaEntry)
	}
	if old := s.entries[id]; old != nil {
		old.timer.Stop()
	}
	entry := &memoryRuntimeQuotaEntry{state: state, expiresAt: time.Now().Add(runtimeQuotaRetention)}
	entry.timer = time.AfterFunc(runtimeQuotaRetention, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.entries[id] == entry {
			delete(s.entries, id)
		}
	})
	s.entries[id] = entry
	return state, nil
}

type redisRuntimeQuotaStore struct {
	rdb redis.UniversalClient
}

func (h *Handler) UseRedisRuntimeQuotaStore(rdb redis.UniversalClient) {
	h.quotaStore = &redisRuntimeQuotaStore{rdb: rdb}
}

func runtimeQuotaKey(id string) string {
	return "mul:runtime:subscription-quota:" + id
}

func decodeRuntimeQuotaState(raw string, err error) (runtimeQuotaState, error) {
	var state runtimeQuotaState
	if errors.Is(err, redis.Nil) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	err = json.Unmarshal([]byte(raw), &state)
	return state, err
}

func (s *redisRuntimeQuotaStore) read(ctx context.Context, ids []string) (map[string]runtimeQuotaState, error) {
	states := make(map[string]runtimeQuotaState, len(ids))
	if len(ids) == 0 {
		return states, nil
	}
	if len(ids) == 1 {
		state, err := decodeRuntimeQuotaState(s.rdb.Get(ctx, runtimeQuotaKey(ids[0])).Result())
		states[ids[0]] = state
		return states, err
	}

	pipe := s.rdb.Pipeline()
	gets := make([]*redis.StringCmd, len(ids))
	for i, id := range ids {
		gets[i] = pipe.Get(ctx, runtimeQuotaKey(id))
	}
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}
	for i, id := range ids {
		state, err := decodeRuntimeQuotaState(gets[i].Result())
		if err != nil {
			return nil, err
		}
		states[id] = state
	}
	return states, nil
}

// Compare the encoded record inside Redis so replicas cannot overwrite an
// in-flight request or a newer result. Managed Redis may deny MULTI/EXEC.
var updateRuntimeQuota = redis.NewScript(`
local current = redis.call('GET', KEYS[1]) or ''
if current ~= ARGV[1] then return 0 end
redis.call('SET', KEYS[1], ARGV[2], 'EX', ARGV[3])
return 1
`)

func (s *redisRuntimeQuotaStore) update(ctx context.Context, id string, transition func(runtimeQuotaState) (runtimeQuotaState, bool)) (runtimeQuotaState, error) {
	key := runtimeQuotaKey(id)
	for range 4 {
		raw, readErr := s.rdb.Get(ctx, key).Result()
		current, err := decodeRuntimeQuotaState(raw, readErr)
		if err != nil {
			return runtimeQuotaState{}, err
		}
		state, changed := transition(current)
		if !changed {
			return state, nil
		}
		next, err := json.Marshal(state)
		if err != nil {
			return runtimeQuotaState{}, err
		}
		written, err := updateRuntimeQuota.Run(ctx, s.rdb, []string{key}, raw, next, int(runtimeQuotaRetention/time.Second)).Int()
		if err != nil {
			return runtimeQuotaState{}, err
		}
		if written == 1 {
			return state, nil
		}
	}
	return runtimeQuotaState{}, errors.New("runtime quota changed concurrently")
}
