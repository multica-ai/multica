package handler

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	directoryPendingTimeout = 3 * time.Minute
	directoryRunningTimeout = 30 * time.Second
	directoryRetention      = 10 * time.Minute
	directoryStoreLimit     = 1000
)

type RuntimeDirectoryListRequest struct {
	ID        string                     `json:"id"`
	RuntimeID string                     `json:"runtime_id"`
	DaemonID  string                     `json:"daemon_id"`
	Status    string                     `json:"status"`
	Path      string                     `json:"path"`
	Result    *protocol.DirectoryListing `json:"result"`
	Error     string                     `json:"error"`
	CreatedAt time.Time                  `json:"created_at"`
	UpdatedAt time.Time                  `json:"updated_at"`
}

type DirectoryListStore interface {
	Create(context.Context, string, string, string) (*RuntimeDirectoryListRequest, error)
	Get(context.Context, string) (*RuntimeDirectoryListRequest, error)
	HasPending(context.Context, string) (bool, error)
	PopPending(context.Context, string) (*RuntimeDirectoryListRequest, error)
	Finish(context.Context, string, *protocol.DirectoryListing, string) error
}

func directoryRequestTerminal(req *RuntimeDirectoryListRequest) bool {
	return req.Status == "completed" || req.Status == "failed" || req.Status == "timeout"
}
func applyDirectoryTimeout(req *RuntimeDirectoryListRequest, now time.Time) bool {
	if req.Status == "pending" && now.Sub(req.CreatedAt) > directoryPendingTimeout || req.Status == "running" && now.Sub(req.UpdatedAt) > directoryRunningTimeout {
		req.Status = "timeout"
		req.Error = "directory listing timed out"
		req.UpdatedAt = now
		return true
	}
	return false
}
func newDirectoryRequest(runtimeID, daemonID, path string) *RuntimeDirectoryListRequest {
	now := time.Now()
	return &RuntimeDirectoryListRequest{ID: randomID(), RuntimeID: runtimeID, DaemonID: daemonID, Path: path, Status: "pending", CreatedAt: now, UpdatedAt: now}
}
func finishDirectoryRequest(req *RuntimeDirectoryListRequest, result *protocol.DirectoryListing, errMsg string) {
	if directoryRequestTerminal(req) {
		return
	}
	req.UpdatedAt = time.Now()
	req.Result = result
	req.Error = errMsg
	req.Status = "completed"
	if result == nil {
		req.Status = "failed"
	}
}
func cloneDirectoryRequest(req *RuntimeDirectoryListRequest) *RuntimeDirectoryListRequest {
	if req == nil {
		return nil
	}
	data, _ := json.Marshal(req)
	var clone RuntimeDirectoryListRequest
	_ = json.Unmarshal(data, &clone)
	return &clone
}

type InMemoryDirectoryListStore struct {
	mu       sync.Mutex
	requests map[string]*RuntimeDirectoryListRequest
}

func NewInMemoryDirectoryListStore() *InMemoryDirectoryListStore {
	return &InMemoryDirectoryListStore{requests: make(map[string]*RuntimeDirectoryListRequest)}
}
func (s *InMemoryDirectoryListStore) sweep() {
	now := time.Now()
	for id, req := range s.requests {
		if now.Sub(req.CreatedAt) > directoryRetention {
			delete(s.requests, id)
		} else {
			applyDirectoryTimeout(req, now)
		}
	}
}
func (s *InMemoryDirectoryListStore) Create(_ context.Context, runtimeID, daemonID, path string) (*RuntimeDirectoryListRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	if len(s.requests) >= directoryStoreLimit {
		return nil, errors.New("directory request queue is full")
	}
	req := newDirectoryRequest(runtimeID, daemonID, path)
	s.requests[req.ID] = req
	return cloneDirectoryRequest(req), nil
}
func (s *InMemoryDirectoryListStore) Get(_ context.Context, id string) (*RuntimeDirectoryListRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	return cloneDirectoryRequest(s.requests[id]), nil
}
func (s *InMemoryDirectoryListStore) HasPending(_ context.Context, runtimeID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	for _, req := range s.requests {
		if req.RuntimeID == runtimeID && req.Status == "pending" {
			return true, nil
		}
	}
	return false, nil
}
func (s *InMemoryDirectoryListStore) PopPending(_ context.Context, runtimeID string) (*RuntimeDirectoryListRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	var first *RuntimeDirectoryListRequest
	for _, req := range s.requests {
		if req.RuntimeID == runtimeID && req.Status == "pending" && (first == nil || req.CreatedAt.Before(first.CreatedAt)) {
			first = req
		}
	}
	if first != nil {
		first.Status = "running"
		first.UpdatedAt = time.Now()
	}
	return cloneDirectoryRequest(first), nil
}
func (s *InMemoryDirectoryListStore) Finish(_ context.Context, id string, result *protocol.DirectoryListing, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	if req := s.requests[id]; req != nil {
		finishDirectoryRequest(req, result, errMsg)
	}
	return nil
}
