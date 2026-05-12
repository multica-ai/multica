package handler

import (
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// InteractionStore is a server-side in-memory store for pending interactions.
//
// Limitations (MVP):
//   - Data lives in process memory only. A server restart loses all pending
//     interactions. Daemon-side callers should treat a missing interaction as
//     timed-out and fall back to their default policy.
//   - For production use, this should be backed by the database (a single
//     "interaction" table with status/expires_at columns).
type InteractionStore struct {
	mu      sync.RWMutex
	items   map[string]*protocol.InteractionRequest
	stopCh  chan struct{}
	stopped bool
}

func NewInteractionStore() *InteractionStore {
	s := &InteractionStore{
		items:  make(map[string]*protocol.InteractionRequest),
		stopCh: make(chan struct{}),
	}
	go s.expireLoop()
	return s
}

func (s *InteractionStore) Create(req protocol.InteractionRequest) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if req.ID == "" {
		req.ID = uuid.New().String()
	}
	if req.Status == "" {
		req.Status = protocol.InteractionStatusPending
	}
	if req.CreatedAt.IsZero() {
		req.CreatedAt = time.Now()
	}
	if req.ExpiresAt.IsZero() && req.Type != protocol.InteractionPlanApproval {
		req.ExpiresAt = req.CreatedAt.Add(5 * time.Minute)
	}
	s.items[req.ID] = &req
	return req.ID
}

func (s *InteractionStore) Get(id string) (protocol.InteractionRequest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.items[id]
	if !ok {
		return protocol.InteractionRequest{}, errors.New("interaction not found")
	}
	return *item, nil
}

func (s *InteractionStore) ListByTask(taskID, status string) []protocol.InteractionRequest {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []protocol.InteractionRequest
	for _, item := range s.items {
		if item.TaskID != taskID {
			continue
		}
		if status != "" && item.Status != status {
			continue
		}
		out = append(out, *item)
	}
	return out
}

func (s *InteractionStore) Respond(id, chosenOption, responseMessage string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[id]
	if !ok {
		return errors.New("interaction not found")
	}
	if item.Status != protocol.InteractionStatusPending {
		return errors.New("interaction already resolved")
	}
	now := time.Now()
	item.ChosenOption = chosenOption
	item.ResponseMessage = strings.TrimSpace(responseMessage)
	item.RespondedAt = &now
	if isDeniedInteractionOption(chosenOption) {
		item.Status = protocol.InteractionStatusDenied
	} else {
		item.Status = protocol.InteractionStatusApproved
	}
	return nil
}

func isDeniedInteractionOption(chosenOption string) bool {
	switch strings.ToLower(strings.TrimSpace(chosenOption)) {
	case "deny", "reject", "decline", "cancel", "stop", "revise", "keep_planning":
		return true
	default:
		return false
	}
}

func (s *InteractionStore) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.stopped {
		s.stopped = true
		close(s.stopCh)
	}
}

func (s *InteractionStore) expireLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case now := <-ticker.C:
			s.expireAt(now)
		}
	}
}

func (s *InteractionStore) expireAt(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.items {
		if item.Status == protocol.InteractionStatusPending && !item.ExpiresAt.IsZero() && now.After(item.ExpiresAt) {
			item.Status = protocol.InteractionStatusTimedOut
			t := now
			item.RespondedAt = &t
		}
	}
}
