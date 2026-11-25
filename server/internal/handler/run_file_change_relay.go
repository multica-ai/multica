package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Run file change body relay ().
//
// Run file change bodies no longer live in the database: the daemon keeps the
// worktree snapshots as pinned git trees on the machine that ran the task, and
// "open a file" becomes a server-mediated round trip — the server queues a
// relay request, hints the runtime's daemon over the WebSocket, the daemon
// claims the request over HTTP, reads the blobs locally, and posts the file
// back. Content passes through memory / Redis only and is never persisted.
//
// The store is the cross-node state for that flow, and the same multi-instance
// constraint as ModelListStore applies: the user's GET, the daemon's claim and
// the daemon's POST can each land on a different API node. Single-node
// deployments without Redis use the in-memory implementation; multi-node
// deployments must use the Redis one or the daemon's POST lands on a node that
// cannot see the waiting request.

const (
	// fileChangeRelayTTL bounds a relay request end to end: the user's GET
	// waits at most fileChangeRelayWait, and anything the daemon delivers
	// after that still has a short window before expiry.
	fileChangeRelayTTL = 30 * time.Second
	// fileChangeRelayWait bounds the server-side wait after the wakeup hint.
	// Beyond it the user sees "runtime offline" and can retry (which issues a
	// fresh request).
	fileChangeRelayWait = 10 * time.Second
	// fileChangeRelayPoll is the Redis waiter poll interval. A response is at
	// most a few MiB and the wait is human-interactive, so latency, not
	// throughput, is the axis that matters.
	fileChangeRelayPoll = 100 * time.Millisecond
	// fileChangeRelayMaxInFlight caps concurrent waiting relays per API node.
	// Worst-case in-flight memory is ~128 MiB (16 × 2 × 2 MiB sides, JSON
	// escaping doubled); typical is tens of KiB.
	fileChangeRelayMaxInFlight = 16
)

// FileChangeRelayRequest is one pending "fetch this file body from the
// runtime" order. UserID and WorkspaceID ride along for authorization and
// audit; they are not sent to the daemon. The struct is stored in Redis, so
// Claimed must stay exported for the claim script to see it.
type FileChangeRelayRequest struct {
	ID          string    `json:"request_id"`
	TaskID      string    `json:"task_id"`
	FileID      string    `json:"file_id"`
	RuntimeID   string    `json:"runtime_id"`
	UserID      string    `json:"user_id"`
	WorkspaceID string    `json:"workspace_id"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
	Claimed     bool      `json:"claimed"`
}

// FileChangeRelayResponse is the daemon's answer: the fetched File, or an
// explicit purged marker when the local record is gone. File is the raw
// rundiff.File JSON the daemon marshalled — it passes through unmodified.
type FileChangeRelayResponse struct {
	State string          `json:"state"` // "ok" or "purged"
	File  json.RawMessage `json:"file,omitempty"`
}

// Expired reports whether the request aged out of the relay window.
func (r *FileChangeRelayRequest) Expired(now time.Time) bool {
	return now.After(r.ExpiresAt)
}

// PendingView is what the daemon sees when it claims requests.
type PendingView struct {
	RequestID string    `json:"request_id"`
	TaskID    string    `json:"task_id"`
	FileID    string    `json:"file_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

// FileChangeRelayStore is the contract every backend (in-memory single-node,
// Redis multi-node) must satisfy.
type FileChangeRelayStore interface {
	// Create enqueues a pending request.
	Create(ctx context.Context, req *FileChangeRelayRequest) error
	// ClaimPending atomically hands every pending request for the runtime to
	// the caller (marks them claimed so a second daemon pull cannot double
	// serve) and returns their views.
	ClaimPending(ctx context.Context, runtimeID string) ([]PendingView, error)
	// Get loads a request by ID, or nil when unknown / expired.
	Get(ctx context.Context, id string) (*FileChangeRelayRequest, error)
	// PutResponse stores the daemon's response and wakes a local waiter.
	PutResponse(ctx context.Context, requestID string, resp *FileChangeRelayResponse) error
	// WaitResponse blocks until a response lands, the deadline passes, or ctx
	// is cancelled. found=false means no response arrived in time.
	WaitResponse(ctx context.Context, requestID string, deadline time.Time) (resp *FileChangeRelayResponse, found bool)
}

// ---------------------------------------------------------------------------
// In-memory implementation (single node)
// ---------------------------------------------------------------------------

type memFileChangeRelayStore struct {
	mu        sync.Mutex
	requests  map[string]*FileChangeRelayRequest
	responses map[string]*FileChangeRelayResponse
	waiters   map[string]chan struct{}
}

func NewInMemoryFileChangeRelayStore() FileChangeRelayStore {
	return &memFileChangeRelayStore{
		requests:  map[string]*FileChangeRelayRequest{},
		responses: map[string]*FileChangeRelayResponse{},
		waiters:   map[string]chan struct{}{},
	}
}

func (s *memFileChangeRelayStore) Create(_ context.Context, req *FileChangeRelayRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.requests[req.ID] = req
	// Garbage-collect expired traffic so the maps cannot grow unbounded.
	for id, old := range s.requests {
		if old.Expired(now) {
			delete(s.requests, id)
			delete(s.responses, id)
		}
	}
	return nil
}

func (s *memFileChangeRelayStore) ClaimPending(_ context.Context, runtimeID string) ([]PendingView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	var out []PendingView
	for _, req := range s.requests {
		if req.RuntimeID != runtimeID || req.Claimed || req.Expired(now) {
			continue
		}
		req.Claimed = true
		out = append(out, PendingView{RequestID: req.ID, TaskID: req.TaskID, FileID: req.FileID, ExpiresAt: req.ExpiresAt})
	}
	return out, nil
}

func (s *memFileChangeRelayStore) Get(_ context.Context, id string) (*FileChangeRelayRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	req := s.requests[id]
	if req == nil || req.Expired(time.Now()) {
		return nil, nil
	}
	return req, nil
}

func (s *memFileChangeRelayStore) PutResponse(_ context.Context, requestID string, resp *FileChangeRelayResponse) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.responses[requestID] = resp
	if ch := s.waiters[requestID]; ch != nil {
		close(ch)
		delete(s.waiters, requestID)
	}
	return nil
}

func (s *memFileChangeRelayStore) WaitResponse(ctx context.Context, requestID string, deadline time.Time) (*FileChangeRelayResponse, bool) {
	s.mu.Lock()
	if resp := s.responses[requestID]; resp != nil {
		s.mu.Unlock()
		return resp, true
	}
	ch := s.waiters[requestID]
	if ch == nil {
		ch = make(chan struct{}, 1)
		s.waiters[requestID] = ch
	}
	s.mu.Unlock()

	wait := time.Until(deadline)
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ch:
		s.mu.Lock()
		resp := s.responses[requestID]
		s.mu.Unlock()
		return resp, true
	case <-timer.C:
		s.mu.Lock()
		delete(s.waiters, requestID)
		s.mu.Unlock()
		return nil, false
	case <-ctx.Done():
		s.mu.Lock()
		delete(s.waiters, requestID)
		s.mu.Unlock()
		return nil, false
	}
}

// ---------------------------------------------------------------------------
// Redis implementation (multi node)
// ---------------------------------------------------------------------------

// Key layout (namespaced under the shared {runtime_pending} hash tag so the
// pending set lives beside the model-list / local-skill stores):
//
//	mul:{runtime_pending}:file_change:req:<id>     → JSON request, TTL = relay TTL
//	mul:{runtime_pending}:file_change:pending:<rt> → ZSET { member = request id, score = created UnixNano }
//	mul:{runtime_pending}:file_change:resp:<id>    → JSON response, TTL = relay TTL
//
// Waiting polls the response key: a relay response is at most a few MiB and
// the interaction is human-scale, so a 100 ms poll beats holding a pub/sub
// subscription per open file.
const (
	fileChangeReqKeyPrefix     = "mul:" + runtimePendingRedisHashTag + ":file_change:req:"
	fileChangePendingKeyPrefix = "mul:" + runtimePendingRedisHashTag + ":file_change:pending:"
	fileChangeRespKeyPrefix    = "mul:" + runtimePendingRedisHashTag + ":file_change:resp:"
)

func fileChangeReqKey(id string) string     { return fileChangeReqKeyPrefix + id }
func fileChangeRespKey(id string) string    { return fileChangeRespKeyPrefix + id }
func fileChangePendingKey(rt string) string { return fileChangePendingKeyPrefix + rt }

type RedisFileChangeRelayStore struct {
	rdb redis.UniversalClient
}

func NewRedisFileChangeRelayStore(rdb redis.UniversalClient) *RedisFileChangeRelayStore {
	return &RedisFileChangeRelayStore{rdb: rdb}
}

func marshalRelayRequest(req *FileChangeRelayRequest) ([]byte, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal relay request: %w", err)
	}
	return data, nil
}

func unmarshalRelayRequest(raw []byte) (*FileChangeRelayRequest, error) {
	var req FileChangeRelayRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("decode relay request: %w", err)
	}
	return &req, nil
}

func (s *RedisFileChangeRelayStore) Create(ctx context.Context, req *FileChangeRelayRequest) error {
	data, err := marshalRelayRequest(req)
	if err != nil {
		return err
	}
	ttl := time.Until(req.ExpiresAt)
	if ttl <= 0 {
		return errors.New("relay request already expired")
	}
	pipe := s.rdb.Pipeline()
	pipe.Set(ctx, fileChangeReqKey(req.ID), data, ttl)
	pipe.ZAdd(ctx, fileChangePendingKey(req.RuntimeID), redis.Z{
		Score:  float64(req.CreatedAt.UnixNano()),
		Member: req.ID,
	})
	// Keep the pending zset alive past the per-record TTL so stale members can
	// be lazily swept on ClaimPending.
	pipe.Expire(ctx, fileChangePendingKey(req.RuntimeID), fileChangeRelayTTL*2)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("persist relay request: %w", err)
	}
	return nil
}

// claimFileChangeScript atomically claims one pending request: the pending
// entry is removed and the stored record flips to claimed, so a concurrent
// pull on another node cannot serve the same request twice.
var claimFileChangeScript = redis.NewScript(`
local removed = redis.call('ZREM', KEYS[1], ARGV[1])
if removed == 0 then
    return 0
end
local raw = redis.call('GET', KEYS[2])
if not raw then
    return 0
end
local ok = pcall(function()
    local req = cjson.decode(raw)
    req.claimed = true
    redis.call('SET', KEYS[2], cjson.encode(req), 'EX', tonumber(ARGV[2]))
end)
if not ok then
    redis.call('SET', KEYS[2], raw, 'KEEPTTL')
end
return 1
`)

func (s *RedisFileChangeRelayStore) ClaimPending(ctx context.Context, runtimeID string) ([]PendingView, error) {
	pendingKey := fileChangePendingKey(runtimeID)
	ids, err := s.rdb.ZRange(ctx, pendingKey, 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("zrange relay pending: %w", err)
	}
	var out []PendingView
	for _, id := range ids {
		req, err := s.loadRequest(ctx, id)
		if err != nil {
			return out, err
		}
		if req == nil {
			// Record expired but the zset still references it — drop.
			s.rdb.ZRem(ctx, pendingKey, id)
			continue
		}
		claimCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		result, err := claimFileChangeScript.Run(claimCtx, s.rdb,
			[]string{pendingKey, fileChangeReqKey(id)},
			id, int(fileChangeRelayTTL.Seconds()),
		).Int64()
		cancel()
		if err != nil {
			return out, fmt.Errorf("claim relay request: %w", err)
		}
		if result == 0 {
			continue // another node won the claim, or the record vanished
		}
		out = append(out, PendingView{RequestID: req.ID, TaskID: req.TaskID, FileID: req.FileID, ExpiresAt: req.ExpiresAt})
	}
	return out, nil
}

func (s *RedisFileChangeRelayStore) loadRequest(ctx context.Context, id string) (*FileChangeRelayRequest, error) {
	raw, err := s.rdb.Get(ctx, fileChangeReqKey(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get relay request: %w", err)
	}
	req, err := unmarshalRelayRequest(raw)
	if err != nil {
		return nil, err
	}
	if req.Expired(time.Now()) {
		return nil, nil
	}
	return req, nil
}

func (s *RedisFileChangeRelayStore) Get(ctx context.Context, id string) (*FileChangeRelayRequest, error) {
	return s.loadRequest(ctx, id)
}

func (s *RedisFileChangeRelayStore) PutResponse(ctx context.Context, requestID string, resp *FileChangeRelayResponse) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("marshal relay response: %w", err)
	}
	if err := s.rdb.Set(ctx, fileChangeRespKey(requestID), data, fileChangeRelayTTL).Err(); err != nil {
		return fmt.Errorf("persist relay response: %w", err)
	}
	return nil
}

func (s *RedisFileChangeRelayStore) WaitResponse(ctx context.Context, requestID string, deadline time.Time) (*FileChangeRelayResponse, bool) {
	for {
		resp, found := s.tryGetResponse(ctx, requestID)
		if found {
			return resp, true
		}
		wait := time.Until(deadline)
		if wait > fileChangeRelayPoll {
			wait = fileChangeRelayPoll
		}
		if wait <= 0 {
			return nil, false
		}
		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return nil, false
		}
	}
}

func (s *RedisFileChangeRelayStore) tryGetResponse(ctx context.Context, requestID string) (*FileChangeRelayResponse, bool) {
	raw, err := s.rdb.Get(ctx, fileChangeRespKey(requestID)).Bytes()
	if err != nil {
		// redis.Nil (nothing yet) and read errors both mean "keep waiting" is
		// best-effort; a broken Redis fails the wait and the user retries.
		return nil, false
	}
	var resp FileChangeRelayResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, false
	}
	return &resp, true
}
