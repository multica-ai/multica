package daemon

import (
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

var (
	ErrInteractionNotFound = errors.New("interaction not found")
	ErrInteractionResolved = errors.New("interaction already resolved")
)

// InteractionRegistry manages pending interaction requests for a daemon.
// It is safe for concurrent use.
type InteractionRegistry struct {
	mu      sync.RWMutex
	items   map[string]*protocol.InteractionRequest // keyed by request ID
	stopCh  chan struct{}
	stopped bool
}

// NewInteractionRegistry creates a registry and starts a background goroutine
// that expires timed-out interactions every second.
func NewInteractionRegistry() *InteractionRegistry {
	r := &InteractionRegistry{
		items:  make(map[string]*protocol.InteractionRequest),
		stopCh: make(chan struct{}),
	}
	go r.expireLoop()
	return r
}

// Create registers a new pending interaction and returns its assigned ID.
func (r *InteractionRegistry) Create(req protocol.InteractionRequest) string {
	r.mu.Lock()
	defer r.mu.Unlock()

	if req.ID == "" {
		req.ID = uuid.New().String()
	}
	if req.Status == "" {
		req.Status = protocol.InteractionStatusPending
	}
	if req.CreatedAt.IsZero() {
		req.CreatedAt = time.Now()
	}
	if req.ExpiresAt.IsZero() {
		req.ExpiresAt = req.CreatedAt.Add(5 * time.Minute)
	}

	r.items[req.ID] = &req
	return req.ID
}

// Get returns a single interaction by ID.
func (r *InteractionRegistry) Get(id string) (protocol.InteractionRequest, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	item, ok := r.items[id]
	if !ok {
		return protocol.InteractionRequest{}, ErrInteractionNotFound
	}
	return *item, nil
}

// List returns all interactions, optionally filtered by status.
func (r *InteractionRegistry) List(status string) []protocol.InteractionRequest {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]protocol.InteractionRequest, 0, len(r.items))
	for _, item := range r.items {
		if status == "" || item.Status == status {
			out = append(out, *item)
		}
	}
	return out
}

// Respond resolves a pending interaction with the chosen option.
func (r *InteractionRegistry) Respond(id, chosenOption, responseMessage string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	item, ok := r.items[id]
	if !ok {
		return ErrInteractionNotFound
	}
	if item.Status != protocol.InteractionStatusPending {
		return ErrInteractionResolved
	}

	now := time.Now()
	item.Status = protocol.InteractionStatusApproved
	if isDeniedRegistryOption(chosenOption) {
		item.Status = protocol.InteractionStatusDenied
	}
	item.ChosenOption = chosenOption
	item.ResponseMessage = strings.TrimSpace(responseMessage)
	item.RespondedAt = &now
	return nil
}

func isDeniedRegistryOption(chosenOption string) bool {
	switch strings.ToLower(strings.TrimSpace(chosenOption)) {
	case "deny", "reject", "decline", "cancel", "stop", "revise", "keep_planning":
		return true
	default:
		return false
	}
}

// Cancel marks a pending interaction as cancelled.
func (r *InteractionRegistry) Cancel(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	item, ok := r.items[id]
	if !ok {
		return ErrInteractionNotFound
	}
	if item.Status != protocol.InteractionStatusPending {
		return ErrInteractionResolved
	}

	now := time.Now()
	item.Status = protocol.InteractionStatusCancelled
	item.RespondedAt = &now
	return nil
}

// Stop terminates the background expiry goroutine.
func (r *InteractionRegistry) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.stopped {
		r.stopped = true
		close(r.stopCh)
	}
}

// expireLoop checks for timed-out interactions every second.
func (r *InteractionRegistry) expireLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.stopCh:
			return
		case now := <-ticker.C:
			r.expireAt(now)
		}
	}
}

func (r *InteractionRegistry) expireAt(now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, item := range r.items {
		if item.Status == protocol.InteractionStatusPending && now.After(item.ExpiresAt) {
			item.Status = protocol.InteractionStatusTimedOut
			t := now
			item.RespondedAt = &t
		}
	}
}
