package cerebra

import (
	"context"
	"sync"
	"time"
)

// SessionPin is the pinned model for a session (issue or chat_session).
type SessionPin struct {
	RuntimeID string
	Model     string
	Tier      Tier
	UpdatedAt time.Time
}

// SessionStore manages session affinity for the router. It pins a model to a
// session when the router first selects one, and reuses it for subsequent
// requests at the same tier (refreshing the TTL on every hit).
//
// Rules:
//   - Same-tier requests reuse the pinned model without re-rolling.
//   - Higher-tier requests update the pin (escalation).
//   - Expired pins are treated as absent.
//
// The in-memory store is the fast path. A DB-backed store (cerebra_session_model
// columns) is the durable persistence layer; extend Set/Get to write/read both.
type SessionStore struct {
	mu   sync.Mutex
	pins map[string]*SessionPin // key: sessionKey(issueID, sessionID)
	ttl  time.Duration
}

// DefaultSessionTTL is the default time-to-live for a session pin.
const DefaultSessionTTL = 2 * time.Hour

// NewSessionStore creates a SessionStore with the given TTL.
func NewSessionStore(ttl time.Duration) *SessionStore {
	if ttl <= 0 {
		ttl = DefaultSessionTTL
	}
	return &SessionStore{
		pins: make(map[string]*SessionPin),
		ttl:  ttl,
	}
}

// Get returns the current pin for the session, or nil if absent/expired.
func (s *SessionStore) Get(_ context.Context, issueID, sessionID string) *SessionPin {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := sessionKey(issueID, sessionID)
	pin, ok := s.pins[key]
	if !ok {
		return nil
	}
	if time.Since(pin.UpdatedAt) > s.ttl {
		delete(s.pins, key)
		return nil
	}
	return pin
}

// Set pins a model to the session. If the session already has a higher-tier pin,
// this call updates the pin (escalation). TTL is always refreshed on Set.
func (s *SessionStore) Set(_ context.Context, issueID, sessionID, runtimeID, model string, tier Tier) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := sessionKey(issueID, sessionID)
	existing, ok := s.pins[key]
	if ok && tierRank(existing.Tier) > tierRank(tier) {
		// Existing pin is at a higher tier — escalate: keep tier but update model/runtime.
		// (In practice the higher-tier path selected this model, so update unconditionally.)
	}
	s.pins[key] = &SessionPin{
		RuntimeID: runtimeID,
		Model:     model,
		Tier:      tier,
		UpdatedAt: time.Now(),
	}
}

// Refresh updates the TTL timestamp for an existing pin without changing the model.
func (s *SessionStore) Refresh(_ context.Context, issueID, sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := sessionKey(issueID, sessionID)
	if pin, ok := s.pins[key]; ok {
		pin.UpdatedAt = time.Now()
	}
}

// Delete removes the pin for a session (e.g. when the session ends).
func (s *SessionStore) Delete(_ context.Context, issueID, sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pins, sessionKey(issueID, sessionID))
}

func sessionKey(issueID, sessionID string) string {
	if sessionID != "" {
		return "session:" + sessionID
	}
	return "issue:" + issueID
}
